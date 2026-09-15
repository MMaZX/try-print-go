//go:build !windows

package statuswindow

import "log/slog"

// Window is a no-op outside Windows: there is no desktop session to show a
// native window on, so state changes are just logged.
type Window struct{}

// Show logs the initial status and returns a no-op Window.
func Show(title, text string) *Window {
	slog.Info(text, "src", "UI", "title", title)
	return &Window{}
}

func (w *Window) SetText(text string) {
	slog.Info(text, "src", "UI")
}

func (w *Window) Close() {}
