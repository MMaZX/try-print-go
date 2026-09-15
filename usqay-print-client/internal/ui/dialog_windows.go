//go:build windows

package ui

import (
	"log/slog"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32       = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW = modUser32.NewProc("MessageBoxW")
)

const (
	mbOK            = 0x00000000
	mbIconError     = 0x00000010
	mbIconWarning   = 0x00000030
	mbIconInfo      = 0x00000040
	mbTopmost       = 0x00040000
	mbSetForeground = 0x00010000
)

func ShowError(title, message string) {
	slog.Error(message, "src", "UI", "title", title)
	messageBox(title, message, mbIconError)
}

func ShowInfo(title, message string) {
	slog.Info(message, "src", "UI", "title", title)
	messageBox(title, message, mbIconInfo)
}

func ShowWarning(title, message string) {
	slog.Warn(message, "src", "UI", "title", title)
	messageBox(title, message, mbIconWarning)
}

func messageBox(title, message string, iconFlag uintptr) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)
	_, _, _ = procMessageBoxW.Call(0, uintptr(unsafe.Pointer(msgPtr)), uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbOK)|iconFlag|uintptr(mbTopmost)|uintptr(mbSetForeground))
}
