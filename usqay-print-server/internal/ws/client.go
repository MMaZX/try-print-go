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

// Client represents one active WebSocket connection from a print agent.
type Client struct {
	hub         *Hub
	conn        *websocket.Conn
	terminalID  string
	businessID  string
	connectedAt time.Time
	lastPing    time.Time
	send        chan any
	pingChan    chan bool // used for active pong verification
	ctx         context.Context
	cancel      context.CancelFunc
}

// Send queues a message for delivery. Non-blocking; drops if the channel is full.
func (c *Client) Send(msg any) {
	select {
	case c.send <- msg:
	default:
		slog.Warn("canal de envío lleno, mensaje descartado", "terminal_id", c.terminalID)
	}
}

// Kick sends a kick notice and cancels the client context.
func (c *Client) Kick(reason string) {
	c.Send(KickMsg{Type: TypeKick, Reason: reason})
	// Give the write pump a moment to flush the kick message.
	time.AfterFunc(200*time.Millisecond, c.cancel)
}

// VerifyAlive sends a ping and waits for a pong or timeout.
func (c *Client) VerifyAlive(timeout time.Duration) bool {
	// Drain any leftover messages in pingChan
	for {
		select {
		case <-c.pingChan:
		default:
			goto drained
		}
	}
drained:

	// Send ping
	c.Send(map[string]string{"type": TypePing})

	// Wait for pong or timeout
	select {
	case <-c.pingChan:
		return true
	case <-time.After(timeout):
		return false
	case <-c.ctx.Done():
		return false
	}
}

// ServeHTTP upgrades the HTTP connection to WebSocket and serves the client
// for the full duration of the connection. Blocking — called from http.Handler.
func ServeHTTP(hub *Hub, tokens map[string]string, w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // allow any Origin for the POC
	})
	if err != nil {
		slog.Error("error en WebSocket upgrade", "error", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan any, 32),
		pingChan: make(chan bool, 1),
		ctx:      ctx,
		cancel:   cancel,
	}
	defer cancel()
	defer conn.CloseNow()
	defer func() {
		if c.terminalID != "" {
			hub.Unregister(c.terminalID, c)
			hub.broadcastToMonitors(map[string]any{
				"type":        "agent_disconnected",
				"terminal_id": c.terminalID,
			})
		}
	}()

	// Step 1: Register handshake (10s timeout).
	if err := c.handleRegister(tokens); err != nil {
		slog.Warn("registro fallido", "remote", r.RemoteAddr, "error", err)
		return
	}

	// Step 2: Start write pump before queuing any outgoing messages.
	writeErr := make(chan error, 1)
	go func() { writeErr <- c.writePump() }()

	// Step 3: Send initial config and deliver any queued jobs.
	c.Send(ConfigMsg{Type: TypeConfig, TerminalID: c.terminalID})
	hub.FlushPending(c.terminalID, c)

	// Step 4: Read pump blocks until the connection dies.
	c.readPump()

	// Stop write pump.
	cancel()
	<-writeErr
}

// handleRegister reads and validates the first message on the connection.
func (c *Client) handleRegister(tokens map[string]string) error {
	regCtx, regCancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer regCancel()

	var raw json.RawMessage
	if err := wsjson.Read(regCtx, c.conn, &raw); err != nil {
		return fmt.Errorf("leer mensaje de registro: %w", err)
	}

	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("parsear envelope: %w", err)
	}
	if env.Type != TypeRegister {
		return fmt.Errorf("primer mensaje debe ser %q, recibido %q", TypeRegister, env.Type)
	}

	var reg RegisterMsg
	if err := json.Unmarshal(raw, &reg); err != nil {
		return fmt.Errorf("parsear register: %w", err)
	}
	if reg.TerminalID == "" {
		return fmt.Errorf("terminal_id vacío")
	}

	valid := false
	businessID := ""
	var valErr error

	if c.hub.cfg.LaravelBaseURL != "" {
		valid, businessID, valErr = c.validateWithLaravel(reg.TerminalID, reg.Token)
		if valErr != nil {
			slog.Warn("error validando con Laravel, intentando fallback local", "terminal_id", reg.TerminalID, "error", valErr)
			expected, ok := tokens[reg.TerminalID]
			if ok && expected == reg.Token {
				valid = true
				businessID = "local-fallback"
			}
		}
	} else {
		expected, ok := tokens[reg.TerminalID]
		if ok && expected == reg.Token {
			valid = true
			businessID = "local-development"
		}
	}

	if !valid {
		return fmt.Errorf("token inválido para terminal %q", reg.TerminalID)
	}

	c.terminalID = reg.TerminalID
	c.businessID = businessID
	c.connectedAt = time.Now().UTC()
	c.lastPing = time.Now().UTC()

	hub := c.hub
	success, err := hub.Register(reg.TerminalID, c)
	if err != nil {
		return err
	}
	if !success {
		return fmt.Errorf("terminal %q ya tiene una conexión activa y saludable", reg.TerminalID)
	}

	// Broadcast to monitors
	hub.broadcastToMonitors(map[string]any{
		"type":        "agent_connected",
		"terminal_id": reg.TerminalID,
		"version":     reg.Version,
	})

	slog.Info("terminal autenticado", "terminal_id", reg.TerminalID, "business_id", c.businessID)
	return nil
}

// validateWithLaravel queries Laravel's agent validation endpoint.
func (c *Client) validateWithLaravel(terminalID, token string) (bool, string, error) {
	cfg := c.hub.cfg
	url := fmt.Sprintf("%s/api/tenant/print-configuration/agents/validate?terminal_id=%s&token=%s",
		cfg.LaravelBaseURL, terminalID, token)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, "", fmt.Errorf("crear request: %w", err)
	}

	if cfg.InternalToken != "" {
		req.Header.Set("X-Internal-Token", cfg.InternalToken)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("ejecutar request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("laravel devolvió status %d", resp.StatusCode)
	}

	var res struct {
		Valid      bool   `json:"valid"`
		BusinessID string `json:"business_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return false, "", fmt.Errorf("decodificar respuesta: %w", err)
	}

	return res.Valid, res.BusinessID, nil
}

// readPump reads messages from the WebSocket and dispatches them.
func (c *Client) readPump() {
	for {
		var raw json.RawMessage
		if err := wsjson.Read(c.ctx, c.conn, &raw); err != nil {
			if c.ctx.Err() == nil {
				slog.Warn("conexión cerrada", "terminal_id", c.terminalID, "error", err)
			}
			return
		}

		// Update lastPing on any received message
		c.lastPing = time.Now().UTC()

		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			slog.Warn("mensaje malformado recibido", "terminal_id", c.terminalID)
			continue
		}
		c.dispatch(env.Type, raw)
	}
}

// dispatch routes an incoming message to the appropriate handler.
func (c *Client) dispatch(msgType string, raw json.RawMessage) {
	switch msgType {
	case TypeReceived:
		var m ReceivedMsg
		if err := json.Unmarshal(raw, &m); err == nil {
			c.hub.MarkReceived(m.JobID)
			slog.Info("ACK recibido", "job_id", m.JobID, "terminal_id", c.terminalID)
		}
	case TypePrinted:
		var m PrintedMsg
		if err := json.Unmarshal(raw, &m); err == nil {
			c.hub.MarkPrinted(m.JobID)
		}
	case TypeError:
		var m ErrorMsg
		if err := json.Unmarshal(raw, &m); err == nil {
			c.hub.MarkError(m.JobID, m.Msg)
		}
	case TypeSync:
		var m SyncMsg
		if err := json.Unmarshal(raw, &m); err == nil {
			c.hub.HandleSync(c.terminalID, m.PrintedJobs, c)
			slog.Info("sync recibido", "terminal_id", c.terminalID, "trabajos_impresos", len(m.PrintedJobs))
		}
	case TypePing:
		c.Send(PongMsg{Type: TypePong})
	case TypePong:
		select {
		case c.pingChan <- true:
		default:
		}
	default:
		slog.Warn("tipo de mensaje desconocido", "type", msgType, "terminal_id", c.terminalID)
	}
}

// writePump drains the send channel and writes messages to the WebSocket.
// Exactly one goroutine runs this at a time.
func (c *Client) writePump() error {
	for {
		select {
		case <-c.ctx.Done():
			_ = c.conn.Close(websocket.StatusNormalClosure, "cerrando")
			return nil
		case msg := <-c.send:
			if err := wsjson.Write(c.ctx, c.conn, msg); err != nil {
				return fmt.Errorf("escribir mensaje a %s: %w", c.terminalID, err)
			}
		}
	}
}
