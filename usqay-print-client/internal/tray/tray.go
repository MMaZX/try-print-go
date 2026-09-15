// Package tray provides a native Windows system tray icon for
// usqay-print-client, with a headless fallback (signal-driven) for
// Linux/servers where there is no desktop session.
package tray

type Callbacks struct {
	OnShowStatus      func()
	OnReload          func()
	OnToggleAutostart func()
	OnExit            func()
}

type Tray interface {
	// Run blocks until the tray/process is told to exit (menu "Salir",
	// Ctrl+C, SIGTERM). callbacks.OnExit is invoked exactly once before Run
	// returns.
	Run(callbacks Callbacks) error
	// NotifyPrintError shows a notification for a job that failed to print.
	// summary is the short text shown in the notification body; detail is
	// the full error, shown if the user clicks the notification (Windows
	// only — elsewhere this is a no-op, the caller already logged detail).
	NotifyPrintError(title, summary, detail string)
	Stop()
}
