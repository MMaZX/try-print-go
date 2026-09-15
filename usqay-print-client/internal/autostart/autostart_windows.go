//go:build windows

package autostart

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TaskName identifies the scheduled task used for autostart.
const TaskName = "UsqayPrintClient"

// taskXMLTemplate starts the executable at the current user's logon, with
// automatic retry if the process exits unexpectedly.
const taskXMLTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Usqay Print Client - inicio automatico con bandeja del sistema</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
    </Exec>
  </Actions>
</Task>
`

func Install() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolver ruta del ejecutable: %w", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return fmt.Errorf("resolver ruta absoluta del ejecutable: %w", err)
	}

	xmlPath, err := writeTaskXML(exePath)
	if err != nil {
		return err
	}
	defer os.Remove(xmlPath)

	cmd := exec.Command("schtasks", "/Create", "/TN", TaskName, "/XML", xmlPath, "/F")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /create fallo: %w (salida: %s)", err, strings.TrimSpace(string(out)))
	}

	slog.Info("tarea de inicio automatico registrada", "src", "AUTOSTART", "tarea", TaskName, "exe", exePath)
	return nil
}

func Uninstall() error {
	cmd := exec.Command("schtasks", "/Delete", "/TN", TaskName, "/F")
	out, err := cmd.CombinedOutput()
	if err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "ERROR") && strings.Contains(outStr, "cannot find") {
			slog.Info("la tarea de inicio automatico ya no existia", "src", "AUTOSTART", "tarea", TaskName)
			return nil
		}
		return fmt.Errorf("schtasks /delete fallo: %w (salida: %s)", err, strings.TrimSpace(outStr))
	}

	slog.Info("tarea de inicio automatico eliminada", "src", "AUTOSTART", "tarea", TaskName)
	return nil
}

func IsEnabled() (bool, error) {
	cmd := exec.Command("schtasks", "/Query", "/TN", TaskName)
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, fmt.Errorf("schtasks /query fallo: %w", err)
	}
	return true, nil
}

// writeTaskXML writes the task definition to a temp file, with the
// executable path properly XML-escaped (quotes, spaces).
func writeTaskXML(exePath string) (string, error) {
	// Literal quotes around the path (for paths with spaces); no %q, which
	// would add Go-style backslash escapes invalid in a Windows path.
	quotedPath := `"` + exePath + `"`

	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(quotedPath)); err != nil {
		return "", fmt.Errorf("escapar ruta del ejecutable para XML: %w", err)
	}

	content := fmt.Sprintf(taskXMLTemplate, escaped.String())

	tmpFile, err := os.CreateTemp("", "usqay-autostart-*.xml")
	if err != nil {
		return "", fmt.Errorf("crear archivo temporal para la tarea: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		return "", fmt.Errorf("escribir definicion de tarea: %w", err)
	}

	return tmpFile.Name(), nil
}
