// Package ui provides simple native dialogs for surfacing critical errors to
// the user, independent of the structured slog output that goes to the log
// files. ShowError/ShowInfo/ShowWarning are implemented per-platform: a
// native MessageBox on Windows, a log line everywhere else (there is no
// interactive desktop session to show a dialog on).
package ui
