// Package localapi exposes a local HTTP endpoint (POST /print) so a LAN caller
// (Electron running on another station, per Propuesta 1) can enqueue a print job
// without going through the cloud WebSocket. It reuses the same SQLite queue and
// worker that already process jobs delivered by usqay-print-server.
package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"usqay-print-client/internal/config"
	"usqay-print-client/internal/queue"
)

// Server serves the local print HTTP API.
type Server struct {
	repo  *queue.Repository
	port  int
	token string
	srv   *http.Server
}

// NewServer builds a Server bound to the port/token configured in cfg.
func NewServer(cfg *config.Config, repo *queue.Repository) *Server {
	return &Server{
		repo:  repo,
		port:  cfg.LocalAPIPort,
		token: cfg.LocalAPIToken,
	}
}

// printRequest mirrors the fields of ws.PrintJobMsg relevant to a HTTP-originated job.
type printRequest struct {
	JobID           string          `json:"job_id"`
	ImpresoraNameID string          `json:"impresora_name_id"`
	DocumentoSlug   string          `json:"documento_slug"`
	Payload         json.RawMessage `json:"payload"`
	Reimpresion     bool            `json:"reimpresion,omitempty"`
}

type printResponse struct {
	JobID  string `json:"job_id"`
	Estado string `json:"estado"`
}

// Handler builds the http.Handler serving the local print API, independent of the
// network listener — used by Run and directly by tests via httptest.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /print", s.handlePrint)
	return mux
}

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts it down gracefully.
func (s *Server) Run(ctx context.Context) {
	s.srv = &http.Server{Addr: fmt.Sprintf("0.0.0.0:%d", s.port), Handler: s.Handler()}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("error apagando servidor HTTP local", "src", "LOCALAPI", "error", err)
		}
	}()

	slog.Info("servidor HTTP local iniciado", "src", "LOCALAPI", "puerto", s.port)
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("servidor HTTP local terminó con error", "src", "LOCALAPI", "error", err)
	}
}

func (s *Server) handlePrint(w http.ResponseWriter, r *http.Request) {
	if s.token != "" && r.Header.Get("X-Local-Api-Token") != s.token {
		slog.Warn("solicitud rechazada: token local inválido o ausente", "src", "LOCALAPI", "remote_addr", r.RemoteAddr)
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}

	var req printRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "JSON inválido: "+err.Error(), http.StatusBadRequest)
		return
	}

	if missing := firstMissingField(req); missing != "" {
		http.Error(w, fmt.Sprintf("campo requerido faltante: %s", missing), http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	job := queue.PrintJob{
		ID:            req.JobID,
		TipoDocumento: strings.ToLower(req.DocumentoSlug),
		ImpresoraID:   req.ImpresoraNameID,
		Payload:       string(req.Payload),
		Estado:        queue.EstadoPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.repo.Enqueue(job, req.Reimpresion); err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			slog.Info("trabajo duplicado recibido por HTTP local, respondiendo idempotente", "src", "LOCALAPI", "job_id", req.JobID)
		} else {
			slog.Error("error guardando trabajo recibido por HTTP local", "src", "LOCALAPI", "job_id", req.JobID, "error", err)
			http.Error(w, "error interno al encolar el trabajo", http.StatusInternalServerError)
			return
		}
	} else {
		slog.Info("trabajo de impresión recibido por HTTP local", "src", "LOCALAPI", "job_id", req.JobID, "tipo", job.TipoDocumento)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(printResponse{JobID: req.JobID, Estado: "recibido"})
}

// firstMissingField returns the name of the first required field that is empty, or "" if all are present.
func firstMissingField(req printRequest) string {
	switch {
	case req.JobID == "":
		return "job_id"
	case req.ImpresoraNameID == "":
		return "impresora_name_id"
	case req.DocumentoSlug == "":
		return "documento_slug"
	case len(req.Payload) == 0:
		return "payload"
	default:
		return ""
	}
}
