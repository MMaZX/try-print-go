package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"usqay-print-server/internal/config"
	"usqay-print-server/internal/logging"
	"usqay-print-server/internal/ws"
)

// LaravelJobRequest represents the JSON request from Laravel when dispatching a job.
type LaravelJobRequest struct {
	JobID           any             `json:"job_id"`
	TerminalID      any             `json:"terminal_id"`
	ImpresoraNameID string          `json:"impresora_name_id"`
	Tipo            string          `json:"tipo"`
	DocumentoSlug   string          `json:"documento_slug"`
	Payload         json.RawMessage `json:"payload"`
	ExpiraEn        int             `json:"expira_en"`
	Reimpresion     bool            `json:"reimpresion"`
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	setupLogger(cfg.LogLevel, nil)

	hub := ws.NewHub(cfg)
	mux := buildRouter(hub, cfg)

	addr := fmt.Sprintf(":%d", cfg.Port)
	slog.Info("usqay-print-server iniciado", "addr", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // WebSocket connections are long-lived
		IdleTimeout:  60 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("servidor detenido", "error", err)
		os.Exit(1)
	}
}

// authMiddleware enforces the presence of X-Internal-Token when configured.
func authMiddleware(cfg *config.Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Internal-Token")
		if cfg.InternalToken != "" && token != cfg.InternalToken {
			slog.Warn("intento de acceso no autorizado", "path", r.URL.Path, "token_recibido", token)
			http.Error(w, "No autorizado", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// parseStringID normalizes string or float/int IDs into a string.
func parseStringID(val any) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func buildRouter(hub *ws.Hub, cfg *config.Config) *http.ServeMux {
	mux := http.NewServeMux()

	// WebSocket endpoint — print clients connect here
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeHTTP(hub, cfg.Tokens, w, r)
	})
	mux.HandleFunc("/ws/agent", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeHTTP(hub, cfg.Tokens, w, r)
	})

	// WebSocket endpoint — monitors (browsers) connect here
	mux.HandleFunc("/ws/monitor", func(w http.ResponseWriter, r *http.Request) {
		ws.ServeMonitorHTTP(hub, w, r)
	})

	// --- Group 1: Laravel → Print Server (Internal calls) ---

	// POST /api/v1/jobs — Laravel dispatches a new job
	mux.HandleFunc("POST /api/v1/jobs", authMiddleware(cfg, func(w http.ResponseWriter, r *http.Request) {
		var req LaravelJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "JSON inválido: "+err.Error(), http.StatusBadRequest)
			return
		}

		jobID := parseStringID(req.JobID)
		terminalID := parseStringID(req.TerminalID)

		if jobID == "" {
			http.Error(w, "job_id es requerido", http.StatusBadRequest)
			return
		}
		if terminalID == "" {
			http.Error(w, "terminal_id es requerido", http.StatusBadRequest)
			return
		}

		job, err := hub.EnqueueLaravel(
			jobID,
			terminalID,
			req.ImpresoraNameID,
			req.Tipo,
			req.DocumentoSlug,
			req.Payload,
			req.ExpiraEn,
			req.Reimpresion,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("trabajo Laravel encolado", "job_id", job.ID, "terminal_id", terminalID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{
			"job_id": job.ID,
			"estado": job.Estado,
		})
	}))

	// PUT /api/v1/jobs/{job_id}/status — Update job status
	mux.HandleFunc("PUT /api/v1/jobs/{job_id}/status", authMiddleware(cfg, func(w http.ResponseWriter, r *http.Request) {
		jobID := r.PathValue("job_id")
		if jobID == "" {
			http.Error(w, "job_id es requerido", http.StatusBadRequest)
			return
		}

		var req struct {
			Estado   string `json:"estado"`
			ErrorMsg string `json:"error_msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "JSON inválido: "+err.Error(), http.StatusBadRequest)
			return
		}

		switch strings.ToUpper(req.Estado) {
		case "IMPRESO":
			hub.MarkPrinted(jobID)
		case "ERROR":
			hub.MarkError(jobID, req.ErrorMsg)
		case "ENVIADO", "RECIBIDO":
			hub.MarkReceived(jobID)
		default:
			http.Error(w, "estado desconocido: "+req.Estado, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"job_id": jobID,
			"estado": strings.ToUpper(req.Estado),
		})
	}))

	// POST /api/v1/agents/{terminal_id}/config-refresh — Notify agent to reconnect and reload config
	mux.HandleFunc("POST /api/v1/agents/{terminal_id}/config-refresh", authMiddleware(cfg, func(w http.ResponseWriter, r *http.Request) {
		terminalID := r.PathValue("terminal_id")
		if terminalID == "" {
			http.Error(w, "terminal_id es requerido", http.StatusBadRequest)
			return
		}

		refreshed := hub.RefreshAgentConfig(terminalID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"terminal_id": terminalID,
			"refreshed":   refreshed,
		})
	}))

	// --- Group 2: Laravel BFF → Print Server (Frontend status info) ---

	// GET /api/v1/agents — List all currently connected agents
	mux.HandleFunc("GET /api/v1/agents", authMiddleware(cfg, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hub.ConnectedAgentsInfo())
	}))

	// GET /api/v1/agents/{terminal_id}/status — Specific status of an agent
	mux.HandleFunc("GET /api/v1/agents/{terminal_id}/status", authMiddleware(cfg, func(w http.ResponseWriter, r *http.Request) {
		terminalID := r.PathValue("terminal_id")
		if terminalID == "" {
			http.Error(w, "terminal_id es requerido", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hub.GetAgentStatus(terminalID))
	}))

	// GET /api/v1/agents/{terminal_id}/printers — Request OS printer list from agent
	mux.HandleFunc("GET /api/v1/agents/{terminal_id}/printers", authMiddleware(cfg, func(w http.ResponseWriter, r *http.Request) {
		terminalID := r.PathValue("terminal_id")
		if terminalID == "" {
			http.Error(w, "terminal_id es requerido", http.StatusBadRequest)
			return
		}

		printers, err := hub.RequestPrinterList(terminalID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"terminal_id": terminalID,
			"printers":    printers,
		})
	}))

	// POST /api/v1/agents/{terminal_id}/kick — Force agent disconnection
	mux.HandleFunc("POST /api/v1/agents/{terminal_id}/kick", authMiddleware(cfg, func(w http.ResponseWriter, r *http.Request) {
		terminalID := r.PathValue("terminal_id")
		if terminalID == "" {
			http.Error(w, "terminal_id es requerido", http.StatusBadRequest)
			return
		}

		var req struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Reason == "" {
			req.Reason = "desconexión forzada vía API"
		}

		kicked := hub.KickAgent(terminalID, req.Reason)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"terminal_id": terminalID,
			"kicked":      kicked,
		})
	}))

	// --- Legacy/Dev testing endpoints ---

	// Trigger a test print job via HTTP (for E2E testing)
	mux.HandleFunc("POST /api/print", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			TerminalID    string          `json:"terminal_id"`
			TipoDocumento string          `json:"tipo_documento"`
			Payload       json.RawMessage `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "JSON inválido: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.TerminalID == "" {
			http.Error(w, "terminal_id es requerido", http.StatusBadRequest)
			return
		}
		if req.TipoDocumento == "" {
			req.TipoDocumento = "comanda"
		}
		if len(req.Payload) == 0 {
			req.Payload = json.RawMessage(`{
				"mesa": 3,
				"items": [
					{"nombre": "Lomo Saltado",    "cantidad": 2, "precio": 21.00},
					{"nombre": "Inca Kola 500ml", "cantidad": 2, "precio": 4.00},
					{"nombre": "Arroz con Leche", "cantidad": 1, "precio": 7.00}
				]
			}`)
		}

		job, err := hub.Enqueue(req.TerminalID, req.TipoDocumento, req.Payload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("trabajo creado via API", "job_id", job.ID, "terminal_id", req.TerminalID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"job_id": job.ID,
			"estado": job.Estado,
		})
	})

	// Status endpoint — show connected terminals and job summary
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"terminales_conectados": hub.ConnectedTerminals(),
			"trabajos":              hub.JobSummary(),
		})
	})

	return mux
}

func setupLogger(level string, _ io.Writer) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	console := logging.NewConsoleHandler(os.Stdout, l)

	daily, err := logging.NewDailyWriter("logs")
	if err != nil {
		slog.New(console).Warn("no se pudo abrir el log diario, solo consola", "error", err)
		slog.SetDefault(slog.New(console))
		return
	}

	if result, err := logging.ArchivePreviousMonth("logs"); err != nil {
		slog.New(console).Warn("error archivando logs del mes anterior", "error", err)
	} else if result != nil {
		slog.New(console).Info("logs del mes anterior archivados", "zip", result.ZipPath, "archivos", result.Archived)
	}

	fileHandler := slog.NewTextHandler(daily, &slog.HandlerOptions{Level: l})
	slog.SetDefault(slog.New(logging.NewMultiHandler(console, fileHandler)))
}
