//go:build linux

package printer

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SystemPrinter sends bytes to a CUPS printer via the lp command.
// escpos=true adds -o raw to bypass CUPS filter chain (thermal printers).
// escpos=false lets CUPS apply the printer's own driver filters (inkjet/laser).
type SystemPrinter struct {
	name   string
	escpos bool
}

// NewSystemPrinter creates a CUPS printer. escpos=true for thermal, false for inkjet/laser.
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
	if p.escpos {
		return p.printRaw(data)
	}
	return p.printText(data)
}

// printRaw sends ESC/POS bytes via stdin with -o raw, bypassing all CUPS filters.
// lp exits immediately after handing bytes to the printer device — no blocking.
func (p *SystemPrinter) printRaw(data []byte) error {
	cmd := exec.Command("lp", "-d", p.name, "-o", "raw", "-")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("lp raw -d %q: %w: %s", p.name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// printText submits plain-text data to CUPS for inkjet/laser printers.
//
// It writes content to a temp file instead of using stdin, and redirects
// stdout/stderr to /dev/null. This is necessary because CombinedOutput() creates
// internal pipes; CUPS filter children (Ghostscript, rastertolsb, etc.) inherit
// those pipe write-fds, causing Go's Wait() to block until the entire filter
// chain finishes — often 10-90 seconds on inkjet printers. With /dev/null fds,
// lp returns as soon as the job is accepted by the spooler.
func (p *SystemPrinter) printText(data []byte) error {
	f, err := os.CreateTemp("", "usqay-print-*.txt")
	if err != nil {
		return fmt.Errorf("crear archivo temporal: %w", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("escribir archivo temporal: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cerrar archivo temporal: %w", err)
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("abrir devnull: %w", err)
	}
	defer devNull.Close()

	// Sin -o media: el tamaño de papel lo decide la cola CUPS (lpadmin/PPD),
	// que es quien conoce el papel realmente instalado en cada impresora.
	cmd := exec.Command("lp", "-d", p.name, f.Name())
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("lp text -d %q: %w", p.name, err)
	}
	return nil
}

// ListPrinters returns all CUPS printers with an auto-detected mode hint.
// It runs lpstat -p -l to obtain make/model descriptions and applies keyword
// matching to distinguish thermal ESC/POS from inkjet/laser devices.
func ListPrinters() ([]PrinterInfo, error) {
	out, err := exec.Command("lpstat", "-p", "-l").Output()
	if err != nil {
		// lpstat exits non-zero when no printers are configured; treat as empty.
		return []PrinterInfo{}, nil
	}

	var infos []PrinterInfo
	var currentName, currentModel string

	flush := func() {
		if currentName == "" {
			return
		}
		src := currentName
		if currentModel != "" {
			src = currentModel
		}
		infos = append(infos, PrinterInfo{Name: currentName, ModeHint: detectModeFromName(src)})
		currentName, currentModel = "", ""
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "printer ") {
			flush()
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				currentName = parts[1]
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Description:") {
			currentModel = strings.TrimSpace(strings.TrimPrefix(trimmed, "Description:"))
		}
	}
	flush()

	if infos == nil {
		infos = []PrinterInfo{}
	}
	return infos, nil
}
