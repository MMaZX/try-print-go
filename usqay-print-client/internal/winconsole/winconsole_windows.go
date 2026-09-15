//go:build windows

package winconsole

import "golang.org/x/sys/windows"

var (
	modKernel32          = windows.NewLazySystemDLL("kernel32.dll")
	modUser32            = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWindow = modKernel32.NewProc("GetConsoleWindow")
	procShowWindow       = modUser32.NewProc("ShowWindow")
)

const swHide = 0

func consoleHwnd() uintptr {
	hwnd, _, _ := procGetConsoleWindow.Call()
	return hwnd
}

// Hide hides the process's own console window, if it has one.
func Hide() {
	if hwnd := consoleHwnd(); hwnd != 0 {
		procShowWindow.Call(hwnd, swHide)
	}
}
