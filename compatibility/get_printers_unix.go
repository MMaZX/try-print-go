//go:build linux || darwin
package compatibility

import (
    "os/exec"
    "strings"
)

func GetInstalledPrinters() ([]string, error) {
    out, err := exec.Command("lpstat", "-p").Output()
    if err != nil {
        return nil, err
    }

    lines := strings.Split(string(out), "\n")
    printers := []string{}

    for _, line := range lines {
        if strings.HasPrefix(line, "printer ") {
            parts := strings.Fields(line)
            if len(parts) >= 2 {
                printers = append(printers, parts[1])
            }
        }
    }

    return printers, nil
}
