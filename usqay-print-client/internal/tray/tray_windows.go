//go:build windows

package tray

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"usqay-print-client/internal/autostart"
	"usqay-print-client/internal/ui"
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modShell32  = windows.NewLazySystemDLL("shell32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow    = modUser32.NewProc("DestroyWindow")
	procCreatePopupMenu  = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW      = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenu   = modUser32.NewProc("TrackPopupMenu")
	procDestroyMenu      = modUser32.NewProc("DestroyMenu")
	procGetCursorPos     = modUser32.NewProc("GetCursorPos")
	procSetForegroundWnd = modUser32.NewProc("SetForegroundWindow")
	procPostQuitMessage  = modUser32.NewProc("PostQuitMessage")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procTranslateMessage = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
	procShellNotifyIconW = modShell32.NewProc("Shell_NotifyIconW")
	procLoadIconW        = modUser32.NewProc("LoadIconW")
	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
)

const (
	wmUser          = 0x0400
	wmTrayIcon      = wmUser + 1
	wmCommand       = 0x0111
	wmRButtonUp     = 0x0205
	wmLButtonDblClk = 0x0203
	wmDestroy       = 0x0002

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	niifInfo  = 0x00000001
	niifError = 0x00000003

	// ninBalloonUserClick is sent via uCallbackMessage when the user clicks
	// the body of the balloon (not its close "x").
	ninBalloonUserClick = wmUser + 5

	mfString    = 0x00000000
	mfSeparator = 0x00000800
	mfChecked   = 0x00000008

	tpmBottomAlign = 0x0020
	tpmRightAlign  = 0x0008

	idmStatus    = 1000
	idmReload    = 1001
	idmAutostart = 1002
	idmExit      = 1003

	// idiApplication (32512) is the resource ID under which goversioninfo
	// embeds icon.ico when building (see versioninfo.json). Passing our own
	// module handle (not NULL) to LoadIconW makes it resolve this embedded
	// icon instead of a Windows system default.
	idiApplication = 32512
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type notifyIconDataW struct {
	cbSize            uint32
	hWnd              windows.HWND
	uID               uint32
	uFlags            uint32
	uCallbackMessage  uint32
	hIcon             windows.Handle
	szTip             [128]uint16
	dwState           uint32
	dwStateMask       uint32
	szInfo            [256]uint16
	uTimeoutOrVersion uint32
	szInfoTitle       [64]uint16
	dwInfoFlags       uint32
	guidItem          windows.GUID
	hBalloonIcon      windows.Handle
}

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type windowsTray struct {
	hwnd      windows.HWND
	nid       notifyIconDataW
	callbacks Callbacks

	// notifyMu protects lastNotifyTitle/lastNotifyDetail: written from the
	// print worker goroutine, read from the window thread (wndProc) when
	// handling the user's click on the balloon.
	notifyMu         sync.Mutex
	lastNotifyTitle  string
	lastNotifyDetail string
	hasPendingDetail bool
}

var currentTray *windowsTray

// NewTray returns a native tray on Windows, unless headless is requested (no
// window/UI at all — Ctrl+C/SIGTERM to exit, matching the non-Windows
// fallback minus SIGHUP reload, which Windows has no equivalent signal for).
func NewTray(headless bool) Tray {
	if headless {
		return &signalTray{stopChan: make(chan struct{})}
	}
	return &windowsTray{}
}

func (t *windowsTray) Run(callbacks Callbacks) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	t.callbacks = callbacks
	currentTray = t

	classNamePtr, _ := syscall.UTF16PtrFromString("UsqayPrintClientTrayClass")
	windowNamePtr, _ := syscall.UTF16PtrFromString("UsqayPrintClientTray")

	// Module handle of the executable itself, not NULL: with NULL, LoadIconW
	// can only return Windows' predefined icons, ignoring any resource
	// embedded in the .exe.
	hInstRaw, _, _ := procGetModuleHandleW.Call(0)
	hInst := windows.Handle(hInstRaw)
	hIcon, _, _ := procLoadIconW.Call(uintptr(hInst), uintptr(idiApplication))

	var wc wndClassExW
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.lpfnWndProc = syscall.NewCallback(wndProc)
	wc.hInstance = hInst
	wc.hIcon = windows.Handle(hIcon)
	wc.hIconSm = windows.Handle(hIcon)
	wc.lpszClassName = classNamePtr

	ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if ret == 0 {
		return fmt.Errorf("RegisterClassExW fallo: %w", err)
	}

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(windowNamePtr)),
		0,
		0, 0, 0, 0,
		0, 0, uintptr(hInst), 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW fallo: %w", err)
	}
	t.hwnd = windows.HWND(hwnd)

	t.nid.cbSize = uint32(unsafe.Sizeof(t.nid))
	t.nid.hWnd = t.hwnd
	t.nid.uID = 1
	t.nid.uFlags = nifMessage | nifIcon | nifTip
	t.nid.uCallbackMessage = wmTrayIcon
	t.nid.hIcon = windows.Handle(hIcon)

	tip, _ := syscall.UTF16FromString("Usqay Print Client")
	copy(t.nid.szTip[:], tip)

	procShellNotifyIconW.Call(uintptr(nimAdd), uintptr(unsafe.Pointer(&t.nid)))
	slog.Info("bandeja del sistema iniciada", "src", "TRAY")

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	t.Stop()
	return nil
}

// NotifyPrintError shows a red-icon balloon for a job that failed to print.
// summary is shown in the balloon body (Windows truncates around 255
// characters); clicking the balloon opens a native dialog (ui.ShowError)
// with the full error.
func (t *windowsTray) NotifyPrintError(title, summary, detail string) {
	t.notifyMu.Lock()
	t.lastNotifyTitle = title
	t.lastNotifyDetail = detail
	t.hasPendingDetail = detail != ""
	t.notifyMu.Unlock()

	t.showBalloon(title, summary, niifError)
}

func (t *windowsTray) showBalloon(title, message string, iconFlag uint32) {
	if t.hwnd == 0 {
		return
	}
	t.nid.uFlags |= nifInfo
	t.nid.dwInfoFlags = iconFlag

	// Clear the fixed-size buffers before copying: they are reused across
	// calls, and a shorter message than the previous one would leave
	// residual characters without a reliable null terminator.
	for i := range t.nid.szInfoTitle {
		t.nid.szInfoTitle[i] = 0
	}
	for i := range t.nid.szInfo {
		t.nid.szInfo[i] = 0
	}

	titleUtf16, _ := syscall.UTF16FromString(title)
	copy(t.nid.szInfoTitle[:len(t.nid.szInfoTitle)-1], titleUtf16)

	msgUtf16, _ := syscall.UTF16FromString(message)
	copy(t.nid.szInfo[:len(t.nid.szInfo)-1], msgUtf16)

	procShellNotifyIconW.Call(uintptr(nimModify), uintptr(unsafe.Pointer(&t.nid)))
}

// showPendingDetail opens the full-error dialog for the last notification,
// if any is pending. Invoked when the user clicks the balloon.
func (t *windowsTray) showPendingDetail() {
	t.notifyMu.Lock()
	title, detail, ok := t.lastNotifyTitle, t.lastNotifyDetail, t.hasPendingDetail
	t.notifyMu.Unlock()

	if !ok {
		return
	}
	ui.ShowError(title, detail)
}

func (t *windowsTray) Stop() {
	if t.hwnd != 0 {
		procShellNotifyIconW.Call(uintptr(nimDelete), uintptr(unsafe.Pointer(&t.nid)))
		procDestroyWindow.Call(uintptr(t.hwnd))
		t.hwnd = 0
	}
}

func wndProc(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) uintptr {
	switch message {
	case wmTrayIcon:
		switch lParam {
		case wmRButtonUp:
			showContextMenu(windows.HWND(hwnd))
		case wmLButtonDblClk:
			if currentTray != nil && currentTray.callbacks.OnShowStatus != nil {
				currentTray.callbacks.OnShowStatus()
			}
		case ninBalloonUserClick:
			if currentTray != nil {
				currentTray.showPendingDetail()
			}
		}
		return 0

	case wmCommand:
		cmdID := int(wParam & 0xFFFF)
		if currentTray != nil {
			switch cmdID {
			case idmStatus:
				if currentTray.callbacks.OnShowStatus != nil {
					currentTray.callbacks.OnShowStatus()
				}
			case idmReload:
				if currentTray.callbacks.OnReload != nil {
					currentTray.callbacks.OnReload()
				}
			case idmAutostart:
				if currentTray.callbacks.OnToggleAutostart != nil {
					currentTray.callbacks.OnToggleAutostart()
				}
			case idmExit:
				if currentTray.callbacks.OnExit != nil {
					currentTray.callbacks.OnExit()
				}
				procPostQuitMessage.Call(0)
			}
		}
		return 0

	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return ret
}

func showContextMenu(hwnd windows.HWND) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	statusStr, _ := syscall.UTF16PtrFromString("Ver informacion de conexion")
	procAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(idmStatus), uintptr(unsafe.Pointer(statusStr)))

	reloadStr, _ := syscall.UTF16PtrFromString("Recargar configuracion")
	procAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(idmReload), uintptr(unsafe.Pointer(reloadStr)))

	enabled, _ := autostart.IsEnabled()
	autostartFlags := uintptr(mfString)
	if enabled {
		autostartFlags |= uintptr(mfChecked)
	}
	autostartStr, _ := syscall.UTF16PtrFromString("Iniciar con el sistema")
	procAppendMenuW.Call(hMenu, autostartFlags, uintptr(idmAutostart), uintptr(unsafe.Pointer(autostartStr)))

	procAppendMenuW.Call(hMenu, uintptr(mfSeparator), 0, 0)

	exitStr, _ := syscall.UTF16PtrFromString("Salir")
	procAppendMenuW.Call(hMenu, uintptr(mfString), uintptr(idmExit), uintptr(unsafe.Pointer(exitStr)))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	procSetForegroundWnd.Call(uintptr(hwnd))
	procTrackPopupMenu.Call(
		hMenu,
		uintptr(tpmRightAlign|tpmBottomAlign),
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		uintptr(hwnd),
		0,
	)
}

// signalTray is used on Windows only when --headless is passed explicitly:
// no window, no tray icon, just wait for Ctrl+C/SIGTERM. Windows has no
// SIGHUP equivalent, so unlike the non-Windows headless fallback there is no
// signal-driven reload here.
type signalTray struct {
	stopChan chan struct{}
}

func (t *signalTray) Run(callbacks Callbacks) error {
	slog.Info("modo headless activo (Ctrl+C/SIGTERM para salir)", "src", "TRAY")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
	case <-t.stopChan:
	}
	if callbacks.OnExit != nil {
		callbacks.OnExit()
	}
	return nil
}

func (t *signalTray) NotifyPrintError(title, summary, detail string) {}

func (t *signalTray) Stop() {
	close(t.stopChan)
}
