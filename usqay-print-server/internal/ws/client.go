package ws

import (
	"bytes"
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
	token       string        // agent token, kept for re-validating with Laravel on config-refresh
	printers    []PrinterSpec // populated from Laravel validate response
	connectedAt time.Time
	lastPing    time.Time
	send        chan any
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

// ServeHTTP upgrades the HTTP connection to WebSocket and serves the client
// for the full duration of the connection. Blocking — called from http.Handler.
func ServeHTTP(hub *Hub, w http.ResponseWriter, r *http.Request) {
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
		ctx:      ctx,
		cancel:   cancel,
	}
	defer cancel()
	defer conn.CloseNow()
	defer func() {
		if c.terminalID != "" {
			hub.Unregister(c.businessID, c.terminalID, c)
			hub.broadcastToMonitors(map[string]any{
				"type":        "agent_disconnected",
				"terminal_id": c.terminalID,
				"business_id": c.businessID,
			})
		}
	}()

	// Step 1: Register handshake (10s timeout).
	if err := c.handleRegister(); err != nil {
		slog.Warn("registro fallido", "remote", r.RemoteAddr, "error", err)
		// Close frame explícito en vez de depender solo del defer conn.CloseNow():
		// sin esto el cliente ve un EOF crudo y no tiene forma de saber que el
		// registro fue rechazado en vez de una caída de red.
		_ = c.conn.Close(websocket.StatusPolicyViolation, "registro rechazado: "+err.Error())
		return
	}

	// Step 2: Start write pump before queuing any outgoing messages.
	writeErr := make(chan error, 1)
	go func() { writeErr <- c.writePump() }()

	// Step 3: Send initial config (terminal identity + printers) and deliver any queued jobs.
	c.Send(ConfigMsg{Type: TypeConfig, TerminalID: c.terminalID, Printers: c.printers})
	hub.FlushPending(c.businessID, c.terminalID, c)

	// Step 4: Read pump blocks until the connection dies.
	c.readPump()

	// Stop write pump.
	cancel()
	<-writeErr
}

// handleRegister reads and validates the first message on the connection.
// The terminal identity (business_id, terminal_id) is derived entirely from
// Laravel's validation of the token — the client does not supply it. There is
// no local/offline fallback: if Laravel is unreachable or unconfigured, the
// registration fails outright rather than degrading to an unauthenticated or
// shared-bucket mode (ver CLAUDE.md: no defaults silenciosos para campos críticos).
func (c *Client) handleRegister() error {
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
	if reg.Token == "" {
		return fmt.Errorf("token vacío en mensaje de registro")
	}

	if c.hub.cfg.LaravelBaseURL == "" {
		return fmt.Errorf("laravel_base_url no configurado — el servidor no puede validar agentes")
	}

	valid, terminalID, businessID, printers, err := c.validateWithLaravel(reg.Token)
	if err != nil {
		return fmt.Errorf("validar con Laravel: %w", err)
	}

	if !valid {
		return fmt.Errorf("token inválido")
	}
	if terminalID == "" {
		return fmt.Errorf("terminal_id no determinado: token válido pero Laravel no devolvió terminal_id")
	}

	c.terminalID = terminalID
	c.businessID = businessID
	c.token = reg.Token
	c.printers = printers
	c.connectedAt = time.Now().UTC()
	c.lastPing = time.Now().UTC()

	hub := c.hub
	hub.Register(businessID, terminalID, c)

	hub.broadcastToMonitors(map[string]any{
		"type":        "agent_connected",
		"terminal_id": terminalID,
		"business_id": businessID,
		"version":     reg.Version,
	})

	slog.Info("terminal autenticado", slog.String("kind", "ok"), "terminal_id", terminalID, "business_id", businessID, "impresoras", len(printers))
	return nil
}

// validateWithLaravel calls POST /api/tenant/print-configuration/agents/validate
// with the agent token and returns the resolved terminal identity and printer list.
// The endpoint is protected by the print.server middleware (X-Internal-Token).
func (c *Client) validateWithLaravel(token string) (valid bool, terminalID, businessID string, printers []PrinterSpec, err error) {
	cfg := c.hub.cfg
	endpoint := cfg.LaravelBaseURL + "/api/tenant/print-configuration/agents/validate"

	body, _ := json.Marshal(map[string]string{"token": token})
	req, reqErr := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if reqErr != nil {
		return false, "", "", nil, fmt.Errorf("crear request: %w", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.InternalToken != "" {
		req.Header.Set("X-Internal-Token", cfg.InternalToken)
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, reqErr := httpClient.Do(req)
	if reqErr != nil {
		return false, "", "", nil, fmt.Errorf("ejecutar request: %w", reqErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, "", "", nil, fmt.Errorf("laravel devolvió status %d", resp.StatusCode)
	}

	var res struct {
		Valid      bool          `json:"valid"`
		TerminalID string        `json:"terminal_id"`
		BusinessID string        `json:"business_id"`
		Printers   []PrinterSpec `json:"printers"`
	}
	if reqErr := json.NewDecoder(resp.Body).Decode(&res); reqErr != nil {
		return false, "", "", nil, fmt.Errorf("decodificar respuesta: %w", reqErr)
	}

	return res.Valid, res.TerminalID, res.BusinessID, res.Printers, nil
}

// RefreshConfig re-validates this agent's token with Laravel and pushes the
// resulting printer list over the already-open connection. Unlike a Kick, the
// WebSocket is never touched — the client applies the new config in place
// (see applyConfig client-side), so in-flight print jobs and the connection
// itself are unaffected. Returns an error if the token is no longer valid or
// Laravel is unreachable; the caller decides what to do with a failed refresh.
func (c *Client) RefreshConfig() error {
	valid, terminalID, businessID, printers, err := c.validateWithLaravel(c.token)
	if err != nil {
		return fmt.Errorf("validar con Laravel: %w", err)
	}
	if !valid {
		return fmt.Errorf("token ya no es válido")
	}
	if terminalID != c.terminalID || businessID != c.businessID {
		return fmt.Errorf("identidad de terminal cambió durante el refresh (terminal_id=%s business_id=%s)", terminalID, businessID)
	}

	c.printers = printers
	c.Send(ConfigMsg{Type: TypeConfig, TerminalID: c.terminalID, Printers: c.printers})
	return nil
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
			c.hub.HandleSync(c.businessID, c.terminalID, m.PrintedJobs, c)
			slog.Info("sync recibido", slog.String("kind", "ok"), "terminal_id", c.terminalID, "trabajos_impresos", len(m.PrintedJobs))
		}
	case TypePing:
		c.Send(PongMsg{Type: TypePong})
	case TypePrinterList:
		var m PrinterListMsg
		if err := json.Unmarshal(raw, &m); err == nil {
			c.hub.ResolvePrinterList(m.RequestID, m.Printers)
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
