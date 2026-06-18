//go:build windows

package printer

import (
	"fmt"

	winprinter "github.com/alexbrainman/printer"
)

// SystemPrinter sends bytes through the Windows Print Spooler (winspool.drv).
// escpos=true uses datatype "RAW" (ESC/POS bytes bypass driver processing).
// escpos=false uses datatype "TEXT" so the driver renders plain text correctly.
type SystemPrinter struct {
	name   string
	escpos bool
}

// NewSystemPrinter creates a Windows spooler printer. escpos=true for thermal, false for inkjet/laser.
func NewSystemPrinter(name string, escpos bool) *SystemPrinter {
	return &SystemPrinter{name: name, escpos: escpos}
}

func (p *SystemPrinter) Mode() string {
	if p.escpos {
		return "escpos"
	}
	return "text"
}

func (p *SystemPrinter) Print(data []byte) error {
	pr, err := winprinter.Open(p.name)
	if err != nil {
		return fmt.Errorf("abrir impresora %q: %w", p.name, err)
	}
	defer pr.Close()

	datatype := "RAW"
	if !p.escpos {
		datatype = "TEXT"
	}

	if err := pr.StartDocument("usqay-job", datatype); err != nil {
		return fmt.Errorf("iniciar documento en %q: %w", p.name, err)
	}
	defer pr.EndDocument()

	if err := pr.StartPage(); err != nil {
		return fmt.Errorf("iniciar página en %q: %w", p.name, err)
	}
	if _, err := pr.Write(data); err != nil {
		pr.EndPage()
		return fmt.Errorf("escribir en impresora %q: %w", p.name, err)
	}
	return pr.EndPage()
}

// ListPrinters returns all Windows printers with an auto-detected mode hint
// derived from the printer name (driver introspection via WinAPI is not needed
// because ESC/POS thermal printer names reliably contain model keywords).
func ListPrinters() ([]PrinterInfo, error) {
	names, err := winprinter.ReadNames()
	if err != nil {
		return nil, fmt.Errorf("listar impresoras Windows: %w", err)
	}
	infos := make([]PrinterInfo, 0, len(names))
	for _, name := range names {
		infos = append(infos, PrinterInfo{
			Name:     name,
			ModeHint: detectModeFromName(name),
		})
	}
	return infos, nil
}
