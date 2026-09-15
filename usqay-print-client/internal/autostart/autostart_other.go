//go:build !windows

package autostart

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func Install() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, _ = filepath.Abs(exePath)

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	autostartDir := filepath.Join(home, ".config", "autostart")
	if err := os.MkdirAll(autostartDir, 0o755); err != nil {
		return fmt.Errorf("crear directorio de autostart: %w", err)
	}

	desktopFile := filepath.Join(autostartDir, "usqay-print-client.desktop")
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Exec=%s
Hidden=false
NoDisplay=false
X-GNOME-Autostart-enabled=true
Name=Usqay Print Client
Comment=Agente de impresion Usqay
`, exePath)

	if err := os.WriteFile(desktopFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("crear archivo autostart .desktop: %w", err)
	}

	slog.Info("inicio automatico configurado", "src", "AUTOSTART", "archivo", desktopFile)
	return nil
}

func Uninstall() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	desktopFile := filepath.Join(home, ".config", "autostart", "usqay-print-client.desktop")
	if err := os.Remove(desktopFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("eliminar archivo autostart: %w", err)
	}
	slog.Info("inicio automatico desinstalado", "src", "AUTOSTART")
	return nil
}

func IsEnabled() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	desktopFile := filepath.Join(home, ".config", "autostart", "usqay-print-client.desktop")
	_, err = os.Stat(desktopFile)
	return err == nil, nil
}
