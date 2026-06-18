// Package ws handles the WebSocket connection to the print server.
package ws

import "encoding/json"

// Protocol message type identifiers (must match the server constants).
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

// Envelope is used to peek at the type field before full decode.
type Envelope struct {
	Type string `json:"type"`
}

// --- Client → Server ---

// RegisterMsg is the first message sent on every (re)connection.
// The server derives the terminal identity from the token via Laravel;
// the client does not send terminal_id.
type RegisterMsg struct {
	Type    string `json:"type"`
	Token   string `json:"token"`
	Version string `json:"version,omitempty"`
}

// AckMsg acknowledges receipt or reports completion of a job.
// Use TypeReceived, TypePrinted, or TypeError in the Type field.
type AckMsg struct {
	Type  string `json:"type"`
	JobID string `json:"job_id"`
	Msg   string `json:"msg,omitempty"` // error description when Type == TypeError
}

// SyncMsg reports locally-printed job IDs on reconnect.
type SyncMsg struct {
	Type        string   `json:"type"`
	PrintedJobs []string `json:"printed_jobs"`
}

// --- Server → Client ---

// PrinterSpec describes a physical printer pushed by the server in ConfigMsg.
type PrinterSpec struct {
	ID   string `json:"id"`             // UUID from impresoras table
	Tipo string `json:"tipo"`           // "RED" | "USB" | "SERIE"
	Addr string `json:"addr"`           // IP:port for RED; OS printer name for USB/SERIE
	Mode string `json:"mode,omitempty"` // "escpos" | "text"; empty defaults to "escpos"
}

// OsPrinterInfo carries an OS-level printer name and its auto-detected mode hint.
type OsPrinterInfo struct {
	Name     string `json:"name"`
	ModeHint string `json:"mode_hint"` // "escpos" | "text"
}

// ConfigMsg is sent by the server after successful token validation.
type ConfigMsg struct {
	Type       string        `json:"type"`
	TerminalID string        `json:"terminal_id"`
	Printers   []PrinterSpec `json:"printers"`
}

// PrintJobMsg delivers a new print job from the server.
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

// ListPrintersMsg is sent by the server to request available OS printer names from this agent.
type ListPrintersMsg struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

// PrinterListMsg is the agent's response carrying OS printer info with mode hints.
type PrinterListMsg struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Printers  []OsPrinterInfo `json:"printers"`
}
