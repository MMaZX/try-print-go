//go:build !windows

package logging

// enableVirtualTerminal is a no-op outside Windows: Linux/macOS terminals
// that report as a tty already understand ANSI escape codes natively.
func enableVirtualTerminal(_ uintptr) bool { return true }
