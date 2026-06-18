// Package ws defines the WebSocket protocol between server and print agents.
package ws

import "encoding/json"

// Protocol message type identifiers.
const (
	TypeRegister     = "register"
	TypeConfig       = "config"
	TypePrint        = "print"
	TypeReceived     = "received"
	TypePrinted      = "printed"
	TypeError        = "error"
	TypeSync         = "sync"
	TypePing         = "ping"
	TypePong         = "pong"
	TypeKick         = "kick"
	TypeListPrinters = "list_printers"
	TypePrinterList  = "printer_list"
)

// Envelope lets us inspect the type field before full decoding.
type Envelope struct {
	Type string `json:"type"`
}

// --- Client → Server ---

// RegisterMsg is the first message sent by a print agent on connect.
// TerminalID is optional: it is used only for local-dev token fallback.
// In production with LaravelBaseURL configured, the server derives the
// terminal identity from the token via the agents/validate endpoint.
type RegisterMsg struct {
	Type       string `json:"type"`
	TerminalID string `json:"terminal_id,omitempty"`
	Token      string `json:"token"`
	Version    string `json:"version,omitempty"`
}

// ReceivedMsg acknowledges that a job was stored locally in SQLite.
type ReceivedMsg struct {
	Type  string `json:"type"`
	JobID string `json:"job_id"`
}

// PrintedMsg confirms that the physical printer processed the job.
type PrintedMsg struct {
	Type  string `json:"type"`
	JobID string `json:"job_id"`
}

// ErrorMsg reports a failed print attempt.
type ErrorMsg struct {
	Type  string `json:"type"`
	JobID string `json:"job_id"`
	Msg   string `json:"msg"`
}

// SyncMsg lists job IDs the client already printed (used on reconnect).
type SyncMsg struct {
	Type        string   `json:"type"`
	PrintedJobs []string `json:"printed_jobs"`
}

// --- Server → Client ---

// PrinterSpec describes a physical printer pushed to the agent on connect.
type PrinterSpec struct {
	ID   string `json:"id"`             // UUID from impresoras table
	Tipo string `json:"tipo"`           // "RED" | "USB" | "SERIE"
	Addr string `json:"addr"`           // IP:port for RED; OS printer name for USB/SERIE
	Mode string `json:"mode,omitempty"` // "escpos" | "text"; empty defaults to "escpos"
}

// OsPrinterInfo carries an OS-level printer name and its auto-detected mode hint,
// reported by the agent in response to a list_printers request.
type OsPrinterInfo struct {
	Name     string `json:"name"`
	ModeHint string `json:"mode_hint"` // "escpos" | "text"
}

// ConfigMsg carries the terminal identity and printer list after registration.
// The client stores this in memory and uses Printers to resolve each print job.
type ConfigMsg struct {
	Type       string        `json:"type"`
	TerminalID string        `json:"terminal_id"`
	Printers   []PrinterSpec `json:"printers"`
}

// PrintJobMsg delivers a print job to the agent.
type PrintJobMsg struct {
	Type            string          `json:"type"`
	JobID           string          `json:"job_id"`
	TipoDocumento   string          `json:"tipo_documento"`
	TerminalID      string          `json:"terminal_id,omitempty"`
	ImpresoraNameID string          `json:"impresora_name_id,omitempty"`
	Tipo            string          `json:"tipo,omitempty"`
	DocumentoSlug   string          `json:"documento_slug,omitempty"`
	Payload         json.RawMessage `json:"payload"`
	ExpiraEn        int             `json:"expira_en,omitempty"`
	Reimpresion     bool            `json:"reimpresion,omitempty"`
}

// KickMsg is sent to the previous connection when a new one registers.
type KickMsg struct {
	Type   string `json:"type"`
	Reason string `json:"reason,omitempty"`
}

// PongMsg responds to a client ping.
type PongMsg struct {
	Type string `json:"type"`
}

// ListPrintersMsg is sent to an agent to request its available OS printer names.
type ListPrintersMsg struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

// PrinterListMsg is the agent's response carrying OS printers with mode hints.
type PrinterListMsg struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Printers  []OsPrinterInfo `json:"printers"`
}
