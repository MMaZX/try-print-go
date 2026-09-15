//go:build !windows

package ui

import "log/slog"

func ShowError(title, message string) {
	slog.Error(message, "src", "UI", "title", title)
}

func ShowInfo(title, message string) {
	slog.Info(message, "src", "UI", "title", title)
}

func ShowWarning(title, message string) {
	slog.Warn(message, "src", "UI", "title", title)
}
