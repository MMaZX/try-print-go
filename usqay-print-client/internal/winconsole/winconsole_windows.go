//go:build windows

package winconsole

import "golang.org/x/sys/windows"

var (
	modKernel32          = windows.NewLazySystemDLL("kernel32.dll")
	modUser32            = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWindow = modKernel32.NewProc("GetConsoleWindow")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procIsWindowVisible  = modUser32.NewProc("IsWindowVisible")
)

const (
	swHide = 0
	swShow = 5
)

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

// Show makes the process's own console window visible again.
func Show() {
	if hwnd := consoleHwnd(); hwnd != 0 {
		procShowWindow.Call(hwnd, swShow)
	}
}

// IsVisible reports whether the console window is currently shown.
func IsVisible() bool {
	hwnd := consoleHwnd()
	if hwnd == 0 {
		return false
	}
	ret, _, _ := procIsWindowVisible.Call(hwnd)
	return ret != 0
}

// Toggle hides the console if it is visible, or shows it otherwise.
func Toggle() {
	if IsVisible() {
		Hide()
		return
	}
	Show()
}
