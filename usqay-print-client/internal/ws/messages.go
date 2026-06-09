// Package ws handles the WebSocket connection to the print server.
package ws

import "encoding/json"

// Protocol message type identifiers (must match the server constants).
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

// Envelope is used to peek at the type field before full decode.
type Envelope struct {
	Type string `json:"type"`
}

// --- Client → Server ---

// RegisterMsg is the first message sent on every (re)connection.
type RegisterMsg struct {
	Type       string `json:"type"`
	TerminalID string `json:"terminal_id"`
	BusinessID string `json:"business_id,omitempty"`
	Token      string `json:"token"`
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

// PrintJobMsg delivers a new print job from the server.
type PrintJobMsg struct {
	Type          string          `json:"type"`
	JobID         string          `json:"job_id"`
	TipoDocumento string          `json:"tipo_documento"`
	Payload       json.RawMessage `json:"payload"`
}
