// Package ws defines the WebSocket protocol between server and print agents.
package ws

import "encoding/json"

// Protocol message type identifiers.
const (
	TypeRegister = "register"
	TypeConfig   = "config"
	TypePrint    = "print"
	TypeReceived = "received"
	TypePrinted  = "printed"
	TypeError    = "error"
	TypeSync     = "sync"
	TypePing     = "ping"
	TypePong     = "pong"
	TypeKick     = "kick"
)

// Envelope lets us inspect the type field before full decoding.
type Envelope struct {
	Type string `json:"type"`
}

// --- Client → Server ---

// RegisterMsg is the first message sent by a print agent on connect.
type RegisterMsg struct {
	Type       string `json:"type"`
	TerminalID string `json:"terminal_id"`
	BusinessID string `json:"business_id,omitempty"`
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

// ConfigMsg carries the printer and routing configuration for the terminal.
type ConfigMsg struct {
	Type       string `json:"type"`
	TerminalID string `json:"terminal_id"`
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
