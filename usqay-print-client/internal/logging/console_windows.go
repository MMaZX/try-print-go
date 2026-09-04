//go:build windows

package logging

import "golang.org/x/sys/windows"

// enableVirtualTerminal turns on ANSI escape processing on the given console
// handle. Windows' legacy console host (conhost — what OpenSSH+pwsh attaches
// to by default, unlike Windows Terminal) does not render ANSI codes unless
// this mode is explicitly enabled; without it, escape sequences print as
// literal garbage like "[36mINFO[0m" instead of colored text.
func enableVirtualTerminal(fd uintptr) bool {
	handle := windows.Handle(fd)
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	return windows.SetConsoleMode(handle, mode) == nil
}
