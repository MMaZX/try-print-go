package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
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
	terminalID string     // set after receiving TypeConfig from the server
	outgoing   chan outMsg // buffered; written by Notify, drained by writeLoop
	// pendingRefresh is set to true when the server sends config_refresh so that
	// the next TypeConfig message is logged with a distinctive banner instead of
	// the normal startup log.
	pendingRefresh atomic.Bool
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
			slog.Error("error parseando config", "src", "SERVER", "error", err)
			return
		}
		c.applyConfig(msg)
	case TypePrint:
		var msg PrintJobMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			slog.Error("error parseando trabajo de impresión", "src", "SERVER", "error", err)
			return
		}
		c.handlePrintJob(msg)
	case TypePong:
		// heartbeat acknowledged — no action needed
	case TypeListPrinters:
		var m ListPrintersMsg
		if err := json.Unmarshal(raw, &m); err != nil {
			slog.Error("error parseando list_printers", "src", "SERVER", "error", err)
			return
		}
		go c.handleListPrinters(m)
	case TypePrinterConfigUpdated:
		var msg PrinterConfigUpdatedMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			slog.Error("error parseando printer_config_updated", "src", "SERVER", "error", err)
			return
		}
		c.applyPrinterConfigUpdate(msg)
	case TypeKick:
		slog.Warn("esta terminal fue desplazada por una nueva conexión — reconectando", "src", "SERVER")
	case "config_refresh":
		slog.Info("servidor solicitó recargar configuración — reconectando", "src", "SERVER")
		c.pendingRefresh.Store(true)
		_ = wsConn.Close(websocket.StatusNormalClosure, "config_refresh")
	case "ping":
		select {
		case c.outgoing <- outMsg{data: map[string]string{"type": "pong"}}:
		default:
		}
	default:
		slog.Warn("tipo de mensaje desconocido del servidor", "src", "SERVER", "type", env.Type)
	}
}

// handlePrintJob persists the job in SQLite and sends an immediate ACK.
// If msg.Reimpresion is true, replaces any existing record and re-queues for printing.
// If the job already exists without the reimpresion flag, logs a warning and ACKs silently.
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

	if err := c.repo.Enqueue(job, msg.Reimpresion); err != nil {
		if errors.Is(err, queue.ErrDuplicate) {
			slog.Warn("trabajo duplicado ignorado — ya existe en cola local", "src", "SERVER", "job_id", msg.JobID)
		} else {
			slog.Error("error guardando trabajo en SQLite", "src", "SERVER", "job_id", msg.JobID, "error", err)
			return
		}
	} else if msg.Reimpresion {
		slog.Info("reimpresión encolada", "src", "SERVER", "job_id", msg.JobID, "tipo", msg.TipoDocumento)
	} else {
		slog.Info("trabajo de impresión recibido", "src", "SERVER", "job_id", msg.JobID, "tipo", msg.TipoDocumento)
	}

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

// LoadCachedConfig loads the last persisted printer configuration from SQLite
// and populates the registry for offline operation.
func (c *Connection) LoadCachedConfig() error {
	terminalID, printersJSON, err := c.repo.LoadConfig()
	if err != nil {
		if errors.Is(err, queue.ErrNoConfig) {
			return err
		}
		return fmt.Errorf("cargar configuración cacheada: %w", err)
	}

	var printers []PrinterSpec
	if err := json.Unmarshal(printersJSON, &printers); err != nil {
		return fmt.Errorf("deserializar impresoras cacheadas: %w", err)
	}

	// La caché local puede pertenecer a una terminal distinta (config.json cambió de
	// terminal, o se reusó el agent.db). Descartarla evita operar con identidad equivocada.
	// terminal_id es opcional en config.json: si no está seteado no hay con qué comparar,
	// así que se acepta la caché tal cual venga (igual que con TypeConfig, ver más abajo).
	if c.cfg.TerminalID != "" && terminalID != c.cfg.TerminalID {
		return fmt.Errorf("%w: caché=%q config=%q", queue.ErrCachedConfigForeignTerminal, terminalID, c.cfg.TerminalID)
	}

	count := c.hydrateRegistry(printers, terminalID)
	if count == 0 {
		slog.Warn("configuración offline cargada desde caché local pero sin impresoras — el agente no podrá resolver trabajos hasta reconectar con el servidor",
			"terminal_id", terminalID, "impresoras", count)
	} else {
		slog.Info("configuración offline cargada desde caché local", "terminal_id", terminalID, "impresoras", count)
	}
	return nil
}

// hydrateRegistry configures terminal ID, resets the registry, and populates it with live Printer instances.
func (c *Connection) hydrateRegistry(printers []PrinterSpec, terminalID string) int {
	c.terminalID = terminalID
	c.registry.Clear()

	count := 0
	for _, spec := range printers {
		p := buildPrinterFromSpec(spec)
		if p == nil {
			slog.Warn("tipo de impresora desconocido, ignorado", "src", "SERVER", "id", spec.ID, "tipo", spec.Tipo)
			continue
		}
		c.registry.Set(spec.ID, p)
		slog.Info("impresora registrada",
			"src", "SERVER",
			"id", spec.ID,
			"tipo", spec.Tipo,
			"addr", spec.Addr,
			"mode", p.Mode(),
		)
		count++
	}
	return count
}

// applyConfig populates the printer registry from the ConfigMsg sent by the server.
// Called each time a TypeConfig message is received (connection and config_refresh).
// When the cycle was triggered by a config_refresh message the change is logged with
// a distinctive [CONFIG] banner so it is unmistakable in the log output.
func (c *Connection) applyConfig(msg ConfigMsg) {
	if c.cfg.TerminalID != "" && msg.TerminalID != c.cfg.TerminalID {
		slog.Warn("configuración recibida para otra terminal, ignorada",
			"src", "CONFIG", "terminal_id_recibido", msg.TerminalID, "terminal_id_local", c.cfg.TerminalID)
		return
	}

	isRefresh := c.pendingRefresh.Swap(false)

	if isRefresh {
		slog.Warn("▶ CONFIGURACIÓN ACTUALIZADA POR EL SERVIDOR ◀",
			"src", "CONFIG", "terminal_id", msg.TerminalID)
	}

	count := c.hydrateRegistry(msg.Printers, msg.TerminalID)

	for _, spec := range msg.Printers {
		if spec.Profile == nil {
			continue
		}
		// No persistir el perfil de una impresora cuyo tipo es desconocido o no se registró:
		if _, ok := c.registry.Resolve(spec.ID); !ok {
			slog.Warn("perfil ignorado para impresora de tipo desconocido", "src", "CONFIG", "id", spec.ID, "tipo", spec.Tipo)
			continue
		}
		if err := c.repo.SaveProfile(spec.ID, *spec.Profile); err != nil {
			slog.Error("error guardando perfil en SQLite", "src", "CONFIG", "id", spec.ID, "error", err)
		}
	}

	if count == 0 {
		slog.Warn("configuración recibida sin impresoras — trabajos en cola serán retenidos hasta recibir config válida",
			"src", "SERVER", "terminal_id", msg.TerminalID)
		return
	}

	printersJSON, err := json.Marshal(msg.Printers)
	if err != nil {
		slog.Error("error serializando impresoras para caché", "src", "CONFIG", "error", err)
	} else {
		if err := c.repo.SaveConfig(msg.TerminalID, printersJSON); err != nil {
			slog.Error("error guardando configuración cacheada en SQLite", "src", "CONFIG", "error", err)
		}
	}

	if isRefresh {
		slog.Warn("▶ REFRESH COMPLETADO ◀",
			"src", "CONFIG", "terminal_id", msg.TerminalID, "impresoras_activas", count)
	} else {
		slog.Info("configuración aplicada", "src", "SERVER", "terminal_id", msg.TerminalID, "impresoras", count)
	}
}

// applyPrinterConfigUpdate saves a single updated profile to SQLite in hot-reload.
func (c *Connection) applyPrinterConfigUpdate(msg PrinterConfigUpdatedMsg) {
	if msg.Profile == nil {
		slog.Warn("mensaje printer_config_updated sin perfil", "src", "CONFIG", "impresora_id", msg.ImpresoraID)
		return
	}
	slog.Info("actualizando perfil de impresora en caliente", "src", "CONFIG", "impresora_id", msg.ImpresoraID, "width_dots", msg.Profile.WidthDots)
	if err := c.repo.SaveProfile(msg.ImpresoraID, *msg.Profile); err != nil {
		slog.Error("error guardando perfil en caliente en SQLite", "src", "CONFIG", "impresora_id", msg.ImpresoraID, "error", err)
	}
}

// handleListPrinters responds to a list_printers request with OS printers and their mode hints.
func (c *Connection) handleListPrinters(m ListPrintersMsg) {
	infos, err := printer.ListPrinters()
	if err != nil {
		slog.Error("error listando impresoras del OS", "error", err)
	}
	printers := make([]OsPrinterInfo, 0, len(infos))
	for _, p := range infos {
		printers = append(printers, OsPrinterInfo{Name: p.Name, ModeHint: p.ModeHint})
	}
	slog.Info("respondiendo lista de impresoras", "src", "SERVER", "request_id", m.RequestID, "count", len(printers))
	select {
	case c.outgoing <- outMsg{data: PrinterListMsg{
		Type:      TypePrinterList,
		RequestID: m.RequestID,
		Printers:  printers,
	}}:
	default:
		slog.Warn("printer_list descartado (canal lleno)", "request_id", m.RequestID)
	}
}

// buildPrinterFromSpec maps a PrinterSpec received from the server to a live Printer.
// Network printers are always ESC/POS (raw TCP port 9100).
// System printers use the mode from the server config; empty defaults to "escpos"
// for backward compatibility with configs that predate the mode field.
func buildPrinterFromSpec(spec PrinterSpec) printer.Printer {
	escpos := spec.Mode != "text"
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
		if strings.HasPrefix(spec.Addr, "/dev/") {
			return printer.NewDirectDevicePrinter(spec.Addr, escpos)
		}
		return printer.NewSystemPrinter(spec.Addr, escpos)
	default:
		return nil
	}
}
