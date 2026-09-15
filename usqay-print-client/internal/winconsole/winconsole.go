// Package winconsole hides the process's own console window on Windows. The
// client is built as a normal console app (not -H=windowsgui) so the
// first-run config wizard can read/write stdin/stdout; once config.json
// exists there is nothing left to type, so the console is hidden for the
// rest of the process's life. There is no way to show it again from the
// tray — closing that window via its "x" delivers a console-close event Go
// turns into an unhandled termination signal, killing the whole process
// instead of just that window (see main.go's -terminal flag for the
// supported way to watch logs live: run the binary directly in a terminal).
//
// On non-Windows platforms Hide is a no-op: there is no console window
// concept to hide (the process just runs in whatever terminal/service
// supervisor launched it).
package winconsole
