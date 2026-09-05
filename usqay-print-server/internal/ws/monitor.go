package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// MonitorClient represents one active WebSocket connection from an admin monitor (browser).
type MonitorClient struct {
	hub           *Hub
	conn          *websocket.Conn
	authenticated bool
	send          chan any
	ctx           context.Context
	cancel        context.CancelFunc
}

// Send queues a message for delivery. Non-blocking; drops if the channel is full.
func (m *MonitorClient) Send(msg any) {
	select {
	case m.send <- msg:
	default:
		slog.Warn("canal de envío de monitor lleno, mensaje descartado")
	}
}

// ServeMonitorHTTP upgrades the HTTP connection to WebSocket and serves the monitor.
func ServeMonitorHTTP(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow any origin for development
	})
	if err != nil {
		slog.Error("error en WebSocket monitor upgrade", "error", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := &MonitorClient{
		hub:    hub,
		conn:   conn,
		send:   make(chan any, 64),
		ctx:    ctx,
		cancel: cancel,
	}
	defer cancel()
	defer conn.CloseNow()

	// Register monitor
	hub.RegisterMonitor(m)
	defer hub.UnregisterMonitor(m)

	// Step 1: Wait for authentication message (10s timeout)
	if err := m.handleAuth(); err != nil {
		slog.Warn("autenticación de monitor fallida", "remote", r.RemoteAddr, "error", err)
		return
	}

	// Step 2: Start write pump
	writeErr := make(chan error, 1)
	go func() { writeErr <- m.writePump() }()

	// Step 3: Send snapshot immediately after authentication
	m.sendSnapshot()

	// Step 4: Keep connection alive by reading (discard messages or handle client close)
	m.readPump()

	cancel()
	<-writeErr
}

// handleAuth handles the monitor authentication handshake.
func (m *MonitorClient) handleAuth() error {
	authCtx, authCancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer authCancel()

	var raw json.RawMessage
	if err := wsjson.Read(authCtx, m.conn, &raw); err != nil {
		return fmt.Errorf("leer mensaje de autenticación: %w", err)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("parsear envelope: %w", err)
	}
	if env.Type != "monitor_auth" {
		return fmt.Errorf("primer mensaje debe ser %q, recibido %q", "monitor_auth", env.Type)
	}

	var auth struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &auth); err != nil {
		return fmt.Errorf("parsear monitor_auth: %w", err)
	}

	if m.hub.cfg.LaravelBaseURL == "" {
		return fmt.Errorf("laravel_base_url no configurado — el servidor no puede validar monitores")
	}

	valid, err := m.hub.validateMonitorTokenWithLaravel(auth.Token)
	if err != nil {
		return fmt.Errorf("validar token de monitor con Laravel: %w", err)
	}
	if !valid {
		return fmt.Errorf("token de monitor inválido")
	}

	m.authenticated = true
	slog.Info("monitor autenticado exitosamente", slog.String("kind", "ok"))
	return nil
}

// MonitorAgentInfo holds fields returned in the snapshot
type MonitorAgentInfo struct {
	TerminalID  string `json:"terminal_id"`
	BusinessID  string `json:"business_id"`
	Online      bool   `json:"online"`
	LastPing    string `json:"last_ping"`
	PendingJobs int    `json:"pending_jobs"`
}

// sendSnapshot sends the initial list of connected agents to the monitor.
func (m *MonitorClient) sendSnapshot() {
	m.hub.mu.RLock()
	agents := make([]MonitorAgentInfo, 0, len(m.hub.clients))
	for key, client := range m.hub.clients {
		agents = append(agents, MonitorAgentInfo{
			TerminalID:  key.terminalID,
			BusinessID:  key.businessID,
			Online:      true,
			LastPing:    client.lastPing.Format(time.RFC3339),
			PendingJobs: m.hub.countPendingJobsLocked(key.businessID, key.terminalID),
		})
	}
	m.hub.mu.RUnlock()

	m.Send(map[string]any{
		"type":   "snapshot",
		"agents": agents,
	})
}

// writePump drains the send channel and writes messages to the WebSocket.
func (m *MonitorClient) writePump() error {
	for {
		select {
		case <-m.ctx.Done():
			_ = m.conn.Close(websocket.StatusNormalClosure, "cerrando")
			return nil
		case msg := <-m.send:
			if err := wsjson.Write(m.ctx, m.conn, msg); err != nil {
				return fmt.Errorf("escribir mensaje a monitor: %w", err)
			}
		}
	}
}

// readPump reads and discards messages from the monitor.
func (m *MonitorClient) readPump() {
	for {
		var raw json.RawMessage
		if err := wsjson.Read(m.ctx, m.conn, &raw); err != nil {
			return
		}
	}
}
