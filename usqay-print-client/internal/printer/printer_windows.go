//go:build windows

package printer

import (
	"fmt"

	winprinter "github.com/alexbrainman/printer"
)

// SystemPrinter sends raw ESC/POS bytes through the Windows Print Spooler (winspool.drv).
type SystemPrinter struct {
	name string
}

// NewSystemPrinter creates a printer that uses the Windows spooler by printer name.
// name must match exactly the printer name shown in Windows Settings → Printers.
func NewSystemPrinter(name string) *SystemPrinter {
	return &SystemPrinter{name: name}
}

func (p *SystemPrinter) Print(data []byte) error {
	pr, err := winprinter.Open(p.name)
	if err != nil {
		return fmt.Errorf("abrir impresora %q: %w", p.name, err)
	}
	defer pr.Close()

	if err := pr.StartDocument("usqay-job", "RAW"); err != nil {
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

// ListPrinters returns the names of all printers installed on this Windows system.
func ListPrinters() ([]string, error) {
	names, err := winprinter.ReadNames()
	if err != nil {
		return nil, fmt.Errorf("listar impresoras Windows: %w", err)
	}
	return names, nil
}
