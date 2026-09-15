//go:build !windows

package tray

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

type headlessTray struct {
	stopChan chan struct{}
}

// NewTray always returns the signal-driven implementation on non-Windows
// platforms: there is no desktop session to attach a tray icon to, so
// `headless` is accepted only for signature parity with the Windows build.
func NewTray(headless bool) Tray {
	return &headlessTray{stopChan: make(chan struct{})}
}

func (t *headlessTray) Run(callbacks Callbacks) error {
	slog.Info("modo headless activo (SIGINT/SIGTERM para salir, SIGHUP para recargar configuracion)", "src", "TRAY")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	for {
		select {
		case sig := <-sigChan:
			if sig == syscall.SIGHUP {
				if callbacks.OnReload != nil {
					callbacks.OnReload()
				}
				continue
			}
			if callbacks.OnExit != nil {
				callbacks.OnExit()
			}
			return nil
		case <-t.stopChan:
			if callbacks.OnExit != nil {
				callbacks.OnExit()
			}
			return nil
		}
	}
}

// NotifyPrintError is a no-op headless: the caller already logs the full
// error, and there is no tray/UI to surface a notification on.
func (t *headlessTray) NotifyPrintError(title, summary, detail string) {}

func (t *headlessTray) Stop() {
	close(t.stopChan)
}
