// Package winconsole controls the visibility of the process's own console
// window on Windows. The client is built as a normal console app (not
// -H=windowsgui) so the first-run config wizard can read/write stdin/stdout;
// once config.json exists there is nothing left to type, so the console is
// hidden by default and can be toggled back on from the tray for debugging
// (watching live log lines) without needing a second console-only binary.
//
// On non-Windows platforms every function is a no-op: there is no console
// window concept to hide (the process just runs in whatever
// terminal/service supervisor launched it).
package winconsole
