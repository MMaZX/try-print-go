//go:build windows
// +build windows

package printer

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	winspool             = syscall.NewLazyDLL("winspool.drv")
	procOpenPrinter      = winspool.NewProc("OpenPrinterW")
	procClosePrinter     = winspool.NewProc("ClosePrinter")
	procStartDocPrinter  = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter    = winspool.NewProc("EndDocPrinter")
	procStartPagePrinter = winspool.NewProc("StartPagePrinter")
	procEndPagePrinter   = winspool.NewProc("EndPagePrinter")
	procWritePrinter     = winspool.NewProc("WritePrinter")
)

type DOC_INFO_1 struct {
	DocName    *uint16
	OutputFile *uint16
	Datatype   *uint16
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func openWindowsPrinter(name string) (syscall.Handle, error) {
	var handle syscall.Handle

	r1, _, err := procOpenPrinter.Call(
		uintptr(unsafe.Pointer(utf16Ptr(name))),
		uintptr(unsafe.Pointer(&handle)),
		0,
	)

	if r1 == 0 {
		return 0, fmt.Errorf("OpenPrinter failed: %v", err)
	}

	return handle, nil
}

func PrintPlatform(printerName string, htmlContent []byte, ratio float64, jobID string) error {
	hPrinter, err := openWindowsPrinter(printerName)
	if err != nil {
		return err
	}
	defer procClosePrinter.Call(uintptr(hPrinter))

	docInfo := DOC_INFO_1{
		DocName:  utf16Ptr("Usqay Job " + jobID),
		Datatype: utf16Ptr("RAW"),
	}

	r1, _, err := procStartDocPrinter.Call(
		uintptr(hPrinter),
		1,
		uintptr(unsafe.Pointer(&docInfo)),
	)

	if r1 == 0 {
		return fmt.Errorf("StartDocPrinter failed: %v", err)
	}
	defer procEndDocPrinter.Call(uintptr(hPrinter))

	r1, _, err = procStartPagePrinter.Call(uintptr(hPrinter))
	if r1 == 0 {
		return fmt.Errorf("StartPagePrinter failed: %v", err)
	}

	var written uint32
	r1, _, err = procWritePrinter.Call(
		uintptr(hPrinter),
		uintptr(unsafe.Pointer(&htmlContent[0])),
		uintptr(len(htmlContent)),
		uintptr(unsafe.Pointer(&written)),
	)

	if r1 == 0 {
		return fmt.Errorf("WritePrinter failed: %v", err)
	}

	r1, _, err = procEndPagePrinter.Call(uintptr(hPrinter))
	if r1 == 0 {
		return fmt.Errorf("EndPagePrinter failed: %v", err)
	}

	return nil
}
