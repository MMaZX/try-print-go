package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"usqay-print-client/internal/config"
	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/queue"
)

const (
	pingInterval = 30 * time.Second
	maxBackoff   = 60 * time.Second
)

// outMsg is an outgoing message queued by the worker or the connection itself.
type outMsg struct {
	data any
}

// Connection manages the WebSocket connection to the print server, including
// automatic reconnection with exponential backoff.
type Connection struct {
	cfg        *config.Config
	repo       *queue.Repository
	registry   *printer.Registry
	terminalID string   // set after receiving TypeConfig from the server
	outgoing   chan outMsg // buffered; written by Notify, drained by writeLoop
}

// NewConnection creates a Connection. Call Run in a goroutine to activate it.
func NewConnection(cfg *config.Config, repo *queue.Repository, registry *printer.Registry) *Connection {
	return &Connection{
		cfg:      cfg,
		repo:     repo,
		registry: registry,
		outgoing: make(chan outMsg, 64),
	}
}

// Notify queues a job-completion notification to be sent to the server.
// Called by the Worker after each print attempt. Non-blocking and goroutine-safe.
// If the outgoing channel is full (connection down for a long time), the
// notification is dropped — it will be recovered by the sync on next reconnect.
func (c *Connection) Notify(jobID string, estado queue.Estado, errMsg string) {
	var msgType string
	switch estado {
	case queue.EstadoPrinted:
		msgType = TypePrinted
	case queue.EstadoError:
		msgType = TypeError
	default:
		return
	}
	select {
	case c.outgoing <- outMsg{data: AckMsg{Type: msgType, JobID: jobID, Msg: errMsg}}:
	default:
		slog.Warn("notificación descartada (canal lleno), se sincronizará al reconectar", "job_id", jobID)
	}
}

// Run connects and reconnects with exponential backoff until ctx is cancelled.
func (c *Connection) Run(ctx context.Context) {
	delay := time.Second
	for ctx.Err() == nil {
		if err := c.connectAndServe(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("WebSocket desconectado, reintentando",
				"error", err, "espera", delay)
			select {
			case <-time.After(delay):
				if delay < maxBackoff {
					delay *= 2
				}
			case <-ctx.Done():
				return
			}
		} else {
			delay = time.Second // reset backoff after a successful connection
		}
	}
	slog.Info("conexión WebSocket detenida")
}

func (c *Connection) connectAndServe(ctx context.Context) error {
	slog.Info("conectando al servidor", "url", c.cfg.ServerURL)

	wsConn, _, err := websocket.Dial(ctx, c.cfg.ServerURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	// connCtx is cancelled when either read or write loop exits.
	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	defer wsConn.CloseNow()

	// Handshake: register + sync
	if err := c.handshake(connCtx, wsConn); err != nil {
		return err
	}
	slog.Info("registrado con el servidor")

	// Run both loops concurrently; either failing triggers connCancel.
	errCh := make(chan error, 2)
	go func() {
		err := c.writeLoop(connCtx, wsConn)
		connCancel()
		errCh <- err
	}()
	go func() {
		err := c.readLoop(connCtx, wsConn)
		connCancel()
		errCh <- err
	}()

	e1 := <-errCh
	e2 := <-errCh

	if ctx.Err() != nil {
		return nil // parent context done = clean shutdown
	}
	if e1 != nil {
		return e1
	}
	return e2
}

// handshake sends the register and sync messages before entering the main loops.
func (c *Connection) handshake(ctx context.Context, wsConn *websocket.Conn) error {
	reg := RegisterMsg{
		Type:    TypeRegister,
		Token:   c.cfg.Token,
		Version: "1.0.3",
	}
	if err := wsjson.Write(ctx, wsConn, reg); err != nil {
		return fmt.Errorf("enviar register: %w", err)
	}

	syncMsg, err := c.buildSync()
	if err != nil {
		return fmt.Errorf("construir sync: %w", err)
	}
	if err := wsjson.Write(ctx, wsConn, syncMsg); err != nil {
		return fmt.Errorf("enviar sync: %w", err)
	}
	slog.Info("sync enviado", "trabajos_impresos_locales", len(syncMsg.PrintedJobs))
	return nil
}

// readLoop reads server messages and dispatches them until the connection closes.
func (c *Connection) readLoop(ctx context.Context, wsConn *websocket.Conn) error {
	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, wsConn, &raw); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("leer: %w", err)
		}
		c.dispatch(raw, wsConn)
	}
}

// writeLoop sends outgoing messages and periodic pings until ctx is cancelled.
func (c *Connection) writeLoop(ctx context.Context, wsConn *websocket.Conn) error {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = wsConn.Close(websocket.StatusNormalClosure, "cerrando")
			return nil
		case <-ping.C:
			if err := wsjson.Write(ctx, wsConn, map[string]string{"type": TypePing}); err != nil {
				return fmt.Errorf("ping: %w", err)
			}
		case out := <-c.outgoing:
			if err := wsjson.Write(ctx, wsConn, out.data); err != nil {
				return fmt.Errorf("enviar mensaje: %w", err)
			}
		}
	}
}

// dispatch routes an incoming server message to the appropriate handler.
func (c *Connection) dispatch(raw json.RawMessage, wsConn *websocket.Conn) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		slog.Warn("mensaje del servidor malformado", "error", err)
		return
	}

	switch env.Type {
	case TypeConfig:
		var msg ConfigMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			slog.Error("error parseando config", "error", err)
			return
		}
		c.applyConfig(msg)
	case TypePrint:
		var msg PrintJobMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			slog.Error("error parseando trabajo de impresión", "error", err)
			return
		}
		c.handlePrintJob(msg)
	case TypePong:
		// heartbeat acknowledged — no action needed
	case TypeListPrinters:
		var m ListPrintersMsg
		if err := json.Unmarshal(raw, &m); err != nil {
			slog.Error("error parseando list_printers", "error", err)
			return
		}
		go c.handleListPrinters(m)
	case TypeKick:
		slog.Warn("esta terminal fue desplazada por una nueva conexión — reconectando")
	case "config_refresh":
		slog.Info("petición de config_refresh recibida del servidor, reconectando...")
		_ = wsConn.Close(websocket.StatusNormalClosure, "config_refresh")
	case "ping":
		select {
		case c.outgoing <- outMsg{data: map[string]string{"type": "pong"}}:
		default:
		}
	default:
		slog.Warn("tipo de mensaje desconocido del servidor", "type", env.Type)
	}
}

// handlePrintJob persists the job in SQLite and sends an immediate ACK.
func (c *Connection) handlePrintJob(msg PrintJobMsg) {
	now := time.Now().UTC()
	job := queue.PrintJob{
		ID:            msg.JobID,
		TipoDocumento: msg.TipoDocumento,
		ImpresoraID:   msg.ImpresoraNameID,
		Payload:       string(msg.Payload),
		Estado:        queue.EstadoPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := c.repo.Insert(job); err != nil {
		slog.Error("error guardando trabajo en SQLite", "job_id", msg.JobID, "error", err)
		return
	}
	slog.Info("trabajo recibido y guardado en SQLite",
		"job_id", msg.JobID, "tipo", msg.TipoDocumento)

	// ACK immediately — the worker will handle the actual printing asynchronously.
	select {
	case c.outgoing <- outMsg{data: AckMsg{Type: TypeReceived, JobID: msg.JobID}}:
	default:
		slog.Warn("ACK descartado (canal lleno)", "job_id", msg.JobID)
	}
}

// buildSync queries SQLite for all PRINTED jobs to report on reconnect.
func (c *Connection) buildSync() (SyncMsg, error) {
	jobs, err := c.repo.ListByStatus(queue.EstadoPrinted)
	if err != nil {
		return SyncMsg{}, fmt.Errorf("listar trabajos PRINTED: %w", err)
	}
	ids := make([]string, 0, len(jobs))
	for _, j := range jobs {
		ids = append(ids, j.ID)
	}
	return SyncMsg{Type: TypeSync, PrintedJobs: ids}, nil
}

// applyConfig populates the printer registry from the ConfigMsg sent by the server.
// Called each time a TypeConfig message is received (connection and config_refresh).
func (c *Connection) applyConfig(msg ConfigMsg) {
	c.terminalID = msg.TerminalID
	c.registry.Clear()
	count := 0
	for _, spec := range msg.Printers {
		p := buildPrinterFromSpec(spec)
		if p == nil {
			slog.Warn("tipo de impresora desconocido, ignorado", "id", spec.ID, "tipo", spec.Tipo)
			continue
		}
		c.registry.Set(spec.ID, p)
		count++
	}
	slog.Info("configuración aplicada", "terminal_id", msg.TerminalID, "impresoras", count)
}

// handleListPrinters responds to a list_printers request from the server with the OS printer names.
func (c *Connection) handleListPrinters(m ListPrintersMsg) {
	names, err := printer.ListPrinters()
	if err != nil {
		slog.Error("error listando impresoras del OS", "error", err)
	}
	if names == nil {
		names = []string{}
	}
	slog.Info("respondiendo lista de impresoras", "request_id", m.RequestID, "count", len(names))
	select {
	case c.outgoing <- outMsg{data: PrinterListMsg{
		Type:      TypePrinterList,
		RequestID: m.RequestID,
		Printers:  names,
	}}:
	default:
		slog.Warn("printer_list descartado (canal lleno)", "request_id", m.RequestID)
	}
}

// buildPrinterFromSpec maps a PrinterSpec received from the server to a live Printer.
func buildPrinterFromSpec(spec PrinterSpec) printer.Printer {
	switch spec.Tipo {
	case "RED":
		if spec.Addr == "" {
			return nil
		}
		return printer.NewNetworkPrinter(spec.Addr)
	case "USB", "SERIE":
		if spec.Addr == "" {
			return nil
		}
		return printer.NewSystemPrinter(spec.Addr)
	default:
		return nil
	}
}
