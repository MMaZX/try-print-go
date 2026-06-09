//go:build linux

package printer

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// SystemPrinter sends raw ESC/POS bytes to a CUPS printer via the lp command.
type SystemPrinter struct {
	name string
}

// NewSystemPrinter creates a printer that uses CUPS by printer name.
// name must match the printer name returned by lpstat -p.
func NewSystemPrinter(name string) *SystemPrinter {
	return &SystemPrinter{name: name}
}

func (p *SystemPrinter) Print(data []byte) error {
	cmd := exec.Command("lp", "-d", p.name, "-")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("lp -d %q: %w: %s", p.name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ListPrinters returns the names of all CUPS printers available on this system.
func ListPrinters() ([]string, error) {
	out, err := exec.Command("lpstat", "-p").Output()
	if err != nil {
		// lpstat exits non-zero when no printers are configured; treat as empty list.
		return nil, nil
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		// lpstat -p output format: "printer <name> is idle."
		if strings.HasPrefix(line, "printer ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				names = append(names, parts[1])
			}
		}
	}
	return names, nil
}
