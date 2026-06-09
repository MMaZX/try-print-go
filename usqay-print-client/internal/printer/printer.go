// Package printer provides a unified interface for sending raw ESC/POS bytes
// to physical thermal printers, regardless of connection type or OS.
package printer

// Printer sends a raw byte stream to a physical printer device.
type Printer interface {
	Print(data []byte) error
}

// Type identifies the printer connection mechanism.
type Type string

const (
	// TypeNetwork sends raw ESC/POS bytes over a direct TCP socket (IP:9100).
	TypeNetwork Type = "network"
	// TypeSystem routes through the OS spooler (Windows winspool / Linux CUPS).
	TypeSystem Type = "system"
)
