// Package printer provides a unified interface for sending bytes to physical
// printers, supporting both thermal ESC/POS and regular inkjet/laser devices.
package printer

import (
	"regexp"
	"strings"
)

// Printer sends a byte stream to a physical printer device.
type Printer interface {
	Print(data []byte) error
	// Mode returns "escpos" for thermal receipt printers or "text" for inkjet/laser.
	Mode() string
}

// Type identifies the printer connection mechanism.
type Type string

const (
	// TypeNetwork sends raw ESC/POS bytes over a direct TCP socket (IP:9100).
	TypeNetwork Type = "network"
	// TypeSystem routes through the OS spooler (Windows winspool / Linux CUPS).
	TypeSystem Type = "system"
)

// PrinterInfo holds the OS name and auto-detected mode hint for a local printer.
type PrinterInfo struct {
	Name     string `json:"name"`
	ModeHint string `json:"mode_hint"` // "escpos" | "text"
}

// escposKeywords is matched case-insensitively against a printer name or model string.
// A match means the printer is likely a thermal ESC/POS receipt device.
var escposKeywords = []string{
	"tm-", "tm_", // Epson TM series (TM-T20, TM-T88…)
	"tsp",          // Star TSP series
	"star ",        // Star Micronics
	"thermal",      // generic thermal
	"receipt",      // generic receipt
	"pos-", "pos_", // generic POS
	"zj-", "zj_", // Zjiang
	"xp-", "xp_", // Xprinter
	"bixolon",    // Bixolon
	"citizen",    // Citizen
	"hprt",       // HPRT
	"snbc",       // SNBC
	"sewoo",      // Sewoo
	"sp-",        // Star SP kitchen printers
	"ct-s",       // Citizen CT-S series
	"rp-", "rp_", // Epson RP series
	"gp-", "gp_", // Gprinter
	"gprinter",             // Gprinter (nombre completo)
	"rongta",               // Rongta
	"goojprt",              // Goojprt
	"munbyn",               // Munbyn
	"escpos",               // driver genérico "ESCPOS"
	"esc/pos",              // driver genérico "ESC/POS"
	"58mm", "76mm", "80mm", // anchos de rollo térmico en el nombre ("80mm Series Printer")
}

// posModelPattern matches generic clone names like "POS80", "POS-58" or
// "pos 80" — a bare "pos" prefix followed by the roll width. These are the
// most common Windows driver names for cheap thermal printers and carry no
// brand keyword at all.
var posModelPattern = regexp.MustCompile(`pos[ _-]?\d`)

// detectModeFromName returns "escpos" if s matches known thermal printer
// keywords or generic POS-model naming patterns, "text" otherwise.
// Matching is case-insensitive.
func detectModeFromName(s string) string {
	lower := strings.ToLower(s)
	for _, kw := range escposKeywords {
		if strings.Contains(lower, kw) {
			return "escpos"
		}
	}
	if posModelPattern.MatchString(lower) {
		return "escpos"
	}
	return "text"
}
