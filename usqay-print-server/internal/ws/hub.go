package ws

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"usqay-print-server/internal/config"
)

// ServerJob is a print job tracked by the server (in-memory for the POC).
type ServerJob struct {
	ID              string          `json:"id"`
	TerminalID      string          `json:"terminal_id"`
	BusinessID      string          `json:"business_id"`
	ImpresoraNameID string          `json:"impresora_name_id"`
	Tipo            string          `json:"tipo"`
	DocumentoSlug   string          `json:"documento_slug"`
	Payload         json.RawMessage `json:"payload"`
	ExpiraEn        int             `json:"expira_en"`
	Reimpresion     bool            `json:"reimpresion"`
	Estado          string          `json:"estado"` // pendiente|enviado|impreso|error
	CreatedAt       time.Time       `json:"created_at"`
}

// AgentInfo represents details of a connected print agent.
type AgentInfo struct {
	TerminalID  string `json:"terminal_id"`
	BusinessID  string `json:"business_id"`
	ConnectedAt string `json:"connected_at"`
	LastPing    string `json:"last_ping"`
}

// AgentStatus represents the status of a specific print agent.
type AgentStatus struct {
	Online      bool   `json:"online"`
	LastPing    string `json:"last_ping,omitempty"`
	PendingJobs int    `json:"pending_jobs"`
}

// Hub maintains the active terminal registry and the in-memory job queue.
// All methods are safe for concurrent use.
type Hub struct {
	mu                  sync.RWMutex
	cfg                 *config.Config
	clients             map[string]*Client      // terminal_id → active client
	jobs                map[string]*ServerJob   // job_id → job
	pending             map[string][]*ServerJob // terminal_id → undelivered jobs
	monitors            map[*MonitorClient]bool // active browser monitors
	printerListRequests map[string]chan []OsPrinterInfo // request_id → response channel
}

// NewHub creates an empty Hub.
func NewHub(cfg *config.Config) *Hub {
	return &Hub{
		cfg:                 cfg,
		clients:             make(map[string]*Client),
		jobs:                make(map[string]*ServerJob),
		pending:             make(map[string][]*ServerJob),
		monitors:            make(map[*MonitorClient]bool),
		printerListRequests: make(map[string]chan []OsPrinterInfo),
	}
}

// Register attempts to make c the active connection for terminalID.
// If another connection is active, it verifies if it is alive before kicking it.
// Returns true if registration succeeded, false if rejected because the existing client is alive.
func (h *Hub) Register(terminalID string, c *Client) (bool, error) {
	h.mu.Lock()
	old, exists := h.clients[terminalID]
	h.mu.Unlock()

	if exists && old != c {
		slog.Info("candidato duplicado detectado, verificando salud del cliente actual", "terminal_id", terminalID)
		if old.VerifyAlive(1 * time.Second) {
			slog.Warn("registro rechazado: el cliente actual responde y está activo", "terminal_id", terminalID)
			return false, nil
		}
		// If not alive, kick the old zombie and replace it
		slog.Warn("kick: cliente anterior inactivo/zombie detectado, desplazándolo", "terminal_id", terminalID)
		old.Kick("nueva conexión registrada (anterior inactiva)")
	}

	h.mu.Lock()
	// Re-check in case another client registered while we were waiting
	if current, exists := h.clients[terminalID]; exists && current != c {
		current.Kick("nueva conexión registrada")
	}
	h.clients[terminalID] = c
	h.mu.Unlock()

	slog.Info("terminal registrado exitosamente", slog.String("kind", "ok"), "terminal_id", terminalID)
	return true, nil
}

// Unregister removes c from the registry only if it is still the active connection.
// This prevents a kicked client from removing the new one.
func (h *Hub) Unregister(terminalID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.clients[terminalID] == c {
		delete(h.clients, terminalID)
		slog.Info("terminal desconectado", "terminal_id", terminalID)
	}
}

// Enqueue creates a new job for the terminal and delivers it if connected,
// or queues it for delivery on the next connection otherwise.
func (h *Hub) Enqueue(terminalID, tipoDocumento string, payload json.RawMessage) (*ServerJob, error) {
	return h.EnqueueLaravel(newJobID(), terminalID, "", "", tipoDocumento, payload, 300, false)
}

// EnqueueLaravel creates a print job with full Laravel metadata and sends it to the agent.
// reimpresion=true instructs the agent to replace an existing job and re-print it.
func (h *Hub) EnqueueLaravel(jobID, terminalID, impresoraNameID, tipo, documentoSlug string, payload json.RawMessage, expiraEn int, reimpresion bool) (*ServerJob, error) {
	job := &ServerJob{
		ID:              jobID,
		TerminalID:      terminalID,
		ImpresoraNameID: impresoraNameID,
		Tipo:            tipo,
		DocumentoSlug:   documentoSlug,
		Payload:         payload,
		ExpiraEn:        expiraEn,
		Reimpresion:     reimpresion,
		Estado:          "pendiente",
		CreatedAt:       time.Now().UTC(),
	}

	h.mu.Lock()
	h.jobs[job.ID] = job
	client, connected := h.clients[terminalID]
	if connected {
		job.BusinessID = client.businessID
	}
	if !connected {
		h.pending[terminalID] = append(h.pending[terminalID], job)
	}
	h.mu.Unlock()

	if connected {
		job.Estado = "enviado"
		client.Send(PrintJobMsg{
			Type:            TypePrint,
			JobID:           job.ID,
			TipoDocumento:   strings.ToLower(job.DocumentoSlug), // For compatibility with client's render
			TerminalID:      job.TerminalID,
			ImpresoraNameID: job.ImpresoraNameID,
			Tipo:            job.Tipo,
			DocumentoSlug:   job.DocumentoSlug,
			Payload:         job.Payload,
			ExpiraEn:        job.ExpiraEn,
			Reimpresion:     job.Reimpresion,
		})
		slog.Info("trabajo enviado por WebSocket", "job_id", job.ID, "terminal_id", terminalID)
	} else {
		slog.Warn("terminal desconectado — trabajo en cola", "job_id", job.ID, "terminal_id", terminalID)
	}

	// Notify monitors
	h.broadcastToMonitors(map[string]any{
		"type":        "job_update",
		"job_id":      job.ID,
		"terminal_id": terminalID,
		"estado":      "PENDIENTE",
	})
	h.broadcastToMonitors(map[string]any{
		"type":         "agent_update",
		"terminal_id":  terminalID,
		"pending_jobs": h.PendingJobsCount(terminalID),
	})

	return job, nil
}

// FlushPending delivers all queued jobs to the newly connected terminal.
func (h *Hub) FlushPending(terminalID string, c *Client) {
	h.mu.Lock()
	jobs := h.pending[terminalID]
	delete(h.pending, terminalID)
	h.mu.Unlock()

	for _, job := range jobs {
		job.Estado = "enviado"
		c.Send(PrintJobMsg{
			Type:            TypePrint,
			JobID:           job.ID,
			TipoDocumento:   strings.ToLower(job.DocumentoSlug), // For compatibility with client's render
			TerminalID:      job.TerminalID,
			ImpresoraNameID: job.ImpresoraNameID,
			Tipo:            job.Tipo,
			DocumentoSlug:   job.DocumentoSlug,
			Payload:         job.Payload,
			ExpiraEn:        job.ExpiraEn,
			Reimpresion:     job.Reimpresion,
		})
		slog.Info("trabajo pendiente entregado tras reconexión", slog.String("kind", "ok"), "job_id", job.ID, "terminal_id", terminalID)
	}
}

// HandleSync marks the given job IDs as printed and resends any jobs the
// client missed while disconnected.
func (h *Hub) HandleSync(terminalID string, printedIDs []string, c *Client) {
	h.mu.Lock()
	updatedIDs := make([]string, 0)
	for _, id := range printedIDs {
		if job, ok := h.jobs[id]; ok && job.TerminalID == terminalID && job.Estado != "impreso" {
			job.Estado = "impreso"
			updatedIDs = append(updatedIDs, id)
		}
	}
	h.mu.Unlock()

	for _, id := range updatedIDs {
		h.broadcastToMonitors(map[string]any{
			"type":        "job_update",
			"job_id":      id,
			"terminal_id": terminalID,
			"estado":      "IMPRESO",
		})
		h.updateLaravelJobStatus(id, "IMPRESO", "")
	}

	h.broadcastToMonitors(map[string]any{
		"type":         "agent_update",
		"terminal_id":  terminalID,
		"pending_jobs": h.PendingJobsCount(terminalID),
	})

	slog.Info("sync procesado", slog.String("kind", "ok"), "terminal_id", terminalID, "confirmados", len(printedIDs))
}

// MarkReceived sets the job state to "enviado" (ACK from client).
func (h *Hub) MarkReceived(jobID string) {
	h.mu.Lock()
	var terminalID string
	var found bool
	if job, ok := h.jobs[jobID]; ok {
		job.Estado = "enviado"
		terminalID = job.TerminalID
		found = true
	}
	h.mu.Unlock()

	if found {
		h.broadcastToMonitors(map[string]any{
			"type":        "job_update",
			"job_id":      jobID,
			"terminal_id": terminalID,
			"estado":      "ENVIADO",
		})
		h.broadcastToMonitors(map[string]any{
			"type":         "agent_update",
			"terminal_id":  terminalID,
			"pending_jobs": h.PendingJobsCount(terminalID),
		})
	}
}

// MarkPrinted sets the job state to "impreso" and calls Laravel webhook.
func (h *Hub) MarkPrinted(jobID string) {
	h.mu.Lock()
	var terminalID string
	var found bool
	if job, ok := h.jobs[jobID]; ok {
		job.Estado = "impreso"
		terminalID = job.TerminalID
		found = true
	}
	h.mu.Unlock()

	if found {
		slog.Info("trabajo confirmado como impreso", slog.String("kind", "ok"), "job_id", jobID)
		h.broadcastToMonitors(map[string]any{
			"type":        "job_update",
			"job_id":      jobID,
			"terminal_id": terminalID,
			"estado":      "IMPRESO",
		})
		h.broadcastToMonitors(map[string]any{
			"type":         "agent_update",
			"terminal_id":  terminalID,
			"pending_jobs": h.PendingJobsCount(terminalID),
		})
		h.updateLaravelJobStatus(jobID, "IMPRESO", "")
	}
}

// MarkError sets the job state to "error" and calls Laravel webhook.
func (h *Hub) MarkError(jobID, msg string) {
	h.mu.Lock()
	var terminalID string
	var found bool
	if job, ok := h.jobs[jobID]; ok {
		job.Estado = "error"
		terminalID = job.TerminalID
		found = true
	}
	h.mu.Unlock()

	if found {
		slog.Error("trabajo con error reportado por cliente", "job_id", jobID, "msg", msg)
		h.broadcastToMonitors(map[string]any{
			"type":        "job_update",
			"job_id":      jobID,
			"terminal_id": terminalID,
			"estado":      "ERROR",
		})
		h.broadcastToMonitors(map[string]any{
			"type":         "agent_update",
			"terminal_id":  terminalID,
			"pending_jobs": h.PendingJobsCount(terminalID),
		})
		h.updateLaravelJobStatus(jobID, "ERROR", msg)
	}
}

// ConnectedTerminals returns a snapshot of connected terminal IDs.
func (h *Hub) ConnectedTerminals() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// JobSummary returns a snapshot of all tracked jobs for the status endpoint.
func (h *Hub) JobSummary() []*ServerJob {
	h.mu.RLock()
	defer h.mu.RUnlock()
	jobs := make([]*ServerJob, 0, len(h.jobs))
	for _, j := range h.jobs {
		cp := *j // copy to avoid races after unlock
		jobs = append(jobs, &cp)
	}
	return jobs
}

// PendingJobsCount returns the number of active jobs for a terminal.
func (h *Hub) PendingJobsCount(terminalID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.countPendingJobsLocked(terminalID)
}

// countPendingJobsLocked counts active jobs without taking the lock.
func (h *Hub) countPendingJobsLocked(terminalID string) int {
	count := 0
	for _, job := range h.jobs {
		if job.TerminalID == terminalID && job.Estado != "impreso" && job.Estado != "error" {
			count++
		}
	}
	return count
}

// ConnectedAgentsInfo returns lists of connected agent metadata.
func (h *Hub) ConnectedAgentsInfo() []AgentInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	infos := make([]AgentInfo, 0, len(h.clients))
	for id, client := range h.clients {
		infos = append(infos, AgentInfo{
			TerminalID:  id,
			BusinessID:  client.businessID,
			ConnectedAt: client.connectedAt.Format(time.RFC3339),
			LastPing:    client.lastPing.Format(time.RFC3339),
		})
	}
	return infos
}

// GetAgentStatus returns the status of a specific terminal ID.
func (h *Hub) GetAgentStatus(terminalID string) AgentStatus {
	h.mu.RLock()
	client, online := h.clients[terminalID]
	h.mu.RUnlock()

	var lastPing string
	if online {
		lastPing = client.lastPing.Format(time.RFC3339)
	}

	return AgentStatus{
		Online:      online,
		LastPing:    lastPing,
		PendingJobs: h.PendingJobsCount(terminalID),
	}
}

// KickAgent disconnects an agent forcefully.
func (h *Hub) KickAgent(terminalID string, reason string) bool {
	h.mu.Lock()
	client, found := h.clients[terminalID]
	h.mu.Unlock()

	if found {
		client.Kick(reason)
		return true
	}
	return false
}

// RefreshAgentConfig sends config_refresh message to the agent.
func (h *Hub) RefreshAgentConfig(terminalID string) bool {
	h.mu.RLock()
	client, found := h.clients[terminalID]
	h.mu.RUnlock()

	if found {
		client.Send(map[string]string{
			"type": "config_refresh",
		})
		return true
	}
	return false
}

// RegisterMonitor registers a new monitor client.
func (h *Hub) RegisterMonitor(m *MonitorClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.monitors[m] = true
	slog.Info("monitor conectado", slog.String("kind", "ok"))
}

// UnregisterMonitor removes a monitor client.
func (h *Hub) UnregisterMonitor(m *MonitorClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.monitors, m)
	slog.Info("monitor desconectado")
}

// broadcastToMonitors sends a message to all authenticated monitors.
func (h *Hub) broadcastToMonitors(msg any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for m := range h.monitors {
		if m.authenticated {
			m.Send(msg)
		}
	}
}

// updateLaravelJobStatus notifies Laravel of a job status change.
func (h *Hub) updateLaravelJobStatus(jobID string, estado string, errMsg string) {
	if h.cfg.LaravelBaseURL == "" {
		slog.Debug("laravel_base_url no configurado, omitiendo actualización de Laravel")
		return
	}

	h.mu.RLock()
	job, found := h.jobs[jobID]
	var terminalID, businessID string
	if found {
		terminalID = job.TerminalID
		businessID = job.BusinessID
	}
	h.mu.RUnlock()

	if !found {
		slog.Warn("updateLaravelJobStatus: job no encontrado", "job_id", jobID)
		return
	}

	url := fmt.Sprintf("%s/api/tenant/print-configuration/queue/%s/status", h.cfg.LaravelBaseURL, jobID)

	payload := map[string]any{
		"estado":      estado,
		"terminal_id": terminalID,
		"business_id": businessID,
	}
	if errMsg != "" {
		payload["error"] = errMsg
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		slog.Error("error marshaling laravel status payload", "error", err)
		return
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		slog.Error("error creating laravel status request", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if h.cfg.InternalToken != "" {
		req.Header.Set("X-Internal-Token", h.cfg.InternalToken)
	}

	client := &http.Client{Timeout: 5 * time.Second}

	go func() {
		resp, err := client.Do(req)
		if err != nil {
			slog.Error("error enviando estado a Laravel", "url", url, "error", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			bodyBytes, _ := io.ReadAll(resp.Body)
			slog.Error("Laravel devolvió error al actualizar estado", "status", resp.StatusCode, "body", string(bodyBytes))
		} else {
			slog.Info("estado actualizado en Laravel", slog.String("kind", "ok"), "job_id", jobID, "estado", estado)
		}
	}()
}

// validateMonitorTokenWithLaravel calls Laravel to validate a monitor token.
func (h *Hub) validateMonitorTokenWithLaravel(token string) (bool, error) {
	if h.cfg.LaravelBaseURL == "" {
		return false, fmt.Errorf("laravel_base_url no configurado")
	}

	url := fmt.Sprintf("%s/api/tenant/print-configuration/monitor-token/validate",
		h.cfg.LaravelBaseURL)
	body := strings.NewReader(`{"token":"` + token + `"}`)
	req, err := http.NewRequest("POST", url, body)
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		return false, fmt.Errorf("crear request: %w", err)
	}

	if h.cfg.InternalToken != "" {
		req.Header.Set("X-Internal-Token", h.cfg.InternalToken)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("ejecutar request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("laravel devolvió status %d", resp.StatusCode)
	}

	var res struct {
		Valid bool `json:"valid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return false, fmt.Errorf("decodificar respuesta: %w", err)
	}

	return res.Valid, nil
}

// RequestPrinterList sends a list_printers request to the connected agent and waits for the response.
// Returns an error if the terminal is not connected or if the request times out.
func (h *Hub) RequestPrinterList(terminalID string) ([]OsPrinterInfo, error) {
	h.mu.RLock()
	client, online := h.clients[terminalID]
	h.mu.RUnlock()

	if !online {
		return nil, fmt.Errorf("terminal %s no está conectado", terminalID)
	}

	reqID := newJobID()
	ch := make(chan []OsPrinterInfo, 1)

	h.mu.Lock()
	h.printerListRequests[reqID] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.printerListRequests, reqID)
		h.mu.Unlock()
	}()

	client.Send(ListPrintersMsg{Type: TypeListPrinters, RequestID: reqID})

	select {
	case printers := <-ch:
		return printers, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("timeout esperando lista de impresoras del terminal %s", terminalID)
	}
}

// ResolvePrinterList delivers a printer_list response to its waiting request.
func (h *Hub) ResolvePrinterList(requestID string, printers []OsPrinterInfo) {
	h.mu.Lock()
	ch, ok := h.printerListRequests[requestID]
	h.mu.Unlock()

	if ok {
		select {
		case ch <- printers:
		default:
		}
	}
}

func newJobID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
