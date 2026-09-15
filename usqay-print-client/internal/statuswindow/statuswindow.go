// Package statuswindow shows a small native window reporting live connection
// status ("conectando...", "reconectando...") that the user cannot dismiss —
// no close button, no Alt+F4 — because it reflects state the agent is
// actively waiting on, not a message the user can act on. It closes itself
// (via Close) once that state resolves, or disappears with the rest of the
// process if the binary is closed.
//
// Implemented as a native window only on Windows, where the tray/GUI runs;
// on other platforms it just logs, matching the internal/ui package.
package statuswindow
