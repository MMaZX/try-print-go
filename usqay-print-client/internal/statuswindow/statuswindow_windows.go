//go:build windows

package statuswindow

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow    = modUser32.NewProc("DestroyWindow")
	procGetSystemMenu    = modUser32.NewProc("GetSystemMenu")
	procRemoveMenu       = modUser32.NewProc("RemoveMenu")
	procSetWindowTextW   = modUser32.NewProc("SetWindowTextW")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procTranslateMessage = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage  = modUser32.NewProc("PostQuitMessage")
	procPostMessageW     = modUser32.NewProc("PostMessageW")
	procSendMessageW     = modUser32.NewProc("SendMessageW")
	procShowWindow       = modUser32.NewProc("ShowWindow")
	procSetForegroundWnd = modUser32.NewProc("SetForegroundWindow")
	procGetClientRect    = modUser32.NewProc("GetClientRect")
	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
)

const (
	wmUserBase  = 0x0400
	wmSetText   = wmUserBase + 10
	wmCloseSelf = wmUserBase + 11
	wmClose     = 0x0010

	wsCaption = 0x00C00000
	wsSysMenu = 0x00080000
	wsVisible = 0x10000000
	wsChild   = 0x40000000
	ssLeft    = 0x00000000

	swShow      = 5
	scClose     = 0xF060
	mfByCommand = 0x00000000

	windowWidth  = 460
	windowHeight = 150
)

// cwUseDefault is CW_USEDEFAULT (0x80000000). A var, not a const: converting
// a negative constant straight to an unsigned type is a compile error in Go
// even when explicitly typed, since constant conversions must be
// representable — the uint32(cwUseDefault) conversion at the call site needs
// a runtime (variable) value to reproduce the bit pattern CreateWindowExW
// expects via two's-complement reinterpretation.
var cwUseDefault int32 = -2147483648

type rect struct{ Left, Top, Right, Bottom int32 }

type point struct{ X, Y int32 }

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

type msg struct {
	HWnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

var (
	registerOnce sync.Once
	classNamePtr *uint16
	registryMu   sync.Mutex
	windowByHWND = map[windows.HWND]*Window{}
)

// Window is a small always-on-top native window with no close button, used
// to report connection status the user cannot dismiss (see package doc).
type Window struct {
	hwnd  windows.HWND
	label windows.HWND
	ready chan struct{}
}

// Show creates and displays the window on a dedicated OS thread — native
// windows are thread-affine, so this thread just pumps this one window's
// message queue for its whole lifetime — and returns once it is visible.
func Show(title, text string) *Window {
	w := &Window{ready: make(chan struct{})}
	go w.run(title, text)
	<-w.ready
	return w
}

func (w *Window) run(title, text string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	registerOnce.Do(func() {
		classNamePtr, _ = syscall.UTF16PtrFromString("UsqayStatusWindowClass")
		hInstRaw, _, _ := procGetModuleHandleW.Call(0)
		var wc wndClassExW
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		wc.lpfnWndProc = syscall.NewCallback(wndProc)
		wc.hInstance = windows.Handle(hInstRaw)
		wc.lpszClassName = classNamePtr
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	})

	hInstRaw, _, _ := procGetModuleHandleW.Call(0)
	titlePtr, _ := syscall.UTF16PtrFromString(title)

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(wsCaption|wsSysMenu),
		uintptr(uint32(cwUseDefault)), uintptr(uint32(cwUseDefault)),
		uintptr(windowWidth), uintptr(windowHeight),
		0, 0, hInstRaw, 0,
	)
	if hwnd == 0 {
		close(w.ready)
		return
	}
	w.hwnd = windows.HWND(hwnd)

	registryMu.Lock()
	windowByHWND[w.hwnd] = w
	registryMu.Unlock()

	// Quita el boton "cerrar" (y Alt+F4) de esta ventana: sin SC_CLOSE en el
	// menu de sistema, Windows ni siquiera dibuja la X. Ver comentario del
	// paquete: esta ventana refleja un estado en curso, no algo descartable
	// por el usuario.
	hMenu, _, _ := procGetSystemMenu.Call(uintptr(hwnd), 0)
	if hMenu != 0 {
		procRemoveMenu.Call(hMenu, uintptr(scClose), uintptr(mfByCommand))
	}

	labelPtr, _ := syscall.UTF16PtrFromString(text)
	staticClass, _ := syscall.UTF16PtrFromString("STATIC")
	var clientRect rect
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&clientRect)))
	label, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(staticClass)),
		uintptr(unsafe.Pointer(labelPtr)),
		uintptr(wsChild|wsVisible|ssLeft),
		uintptr(16), uintptr(16),
		uintptr(clientRect.Right-clientRect.Left-32), uintptr(clientRect.Bottom-clientRect.Top-32),
		uintptr(hwnd), 0, hInstRaw, 0,
	)
	w.label = windows.HWND(label)

	procShowWindow.Call(uintptr(hwnd), swShow)
	procSetForegroundWnd.Call(uintptr(hwnd))

	close(w.ready)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	registryMu.Lock()
	delete(windowByHWND, w.hwnd)
	registryMu.Unlock()
}

// SetText updates the label. SendMessage blocks (even across threads) until
// the target thread's WndProc has processed it, so this is safe to call from
// any goroutine without racing the text being read.
func (w *Window) SetText(text string) {
	if w == nil || w.hwnd == 0 {
		return
	}
	ptr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procSendMessageW.Call(uintptr(w.hwnd), uintptr(wmSetText), 0, uintptr(unsafe.Pointer(ptr)))
}

// Close asks the window to destroy itself and stop pumping messages. Safe to
// call from any goroutine, and safe to call more than once (or on a nil
// Window).
func (w *Window) Close() {
	if w == nil || w.hwnd == 0 {
		return
	}
	procPostMessageW.Call(uintptr(w.hwnd), uintptr(wmCloseSelf), 0, 0)
	w.hwnd = 0
}

func wndProc(hwnd uintptr, message uint32, wParam uintptr, lParam uintptr) uintptr {
	switch message {
	case wmSetText:
		registryMu.Lock()
		win := windowByHWND[windows.HWND(hwnd)]
		registryMu.Unlock()
		if win != nil && win.label != 0 {
			procSetWindowTextW.Call(uintptr(win.label), lParam)
		}
		return 0
	case wmCloseSelf:
		procDestroyWindow.Call(hwnd)
		procPostQuitMessage.Call(0)
		return 0
	case wmClose:
		// Nunca deberia dispararse sin SC_CLOSE en el menu, pero por las
		// dudas: ignorar cualquier intento externo de cerrar la ventana.
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return ret
}
