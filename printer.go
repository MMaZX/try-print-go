package main

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"usqay-print-server/printer"
)

func ValidatePrintDependencies() error {
	switch runtime.GOOS {
	case "linux":
		return validateLinuxDeps()
	case "darwin":
		return validateMacDeps()
	case "windows":
		return nil // Windows no requiere dependencias externas
	default:
		return fmt.Errorf("sistema operativo no soportado: %s", runtime.GOOS)
	}
}

func validateLinuxDeps() error {
	// lp (cups-client)
	if _, err := exec.LookPath("lp"); err != nil {
		return errors.New("❌ Falta 'lp'. Instala CUPS: sudo apt install cups cups-client -y")
	}

	// wkhtmltopdf
	if _, err := exec.LookPath("wkhtmltopdf"); err != nil {
		return errors.New("❌ Falta 'wkhtmltopdf'. Instálalo: sudo apt install wkhtmltopdf -y")
	}

	return nil
}

func validateMacDeps() error {
	if _, err := exec.LookPath("lp"); err != nil {
		return errors.New("❌ 'lp' no está disponible. Asegúrate de tener CUPS habilitado")
	}
	if _, err := exec.LookPath("wkhtmltopdf"); err != nil {
		return errors.New("❌ Falta 'wkhtmltopdf'. Instala con: brew install wkhtmltopdf")
	}
	return nil
}

func Print(printerName string, html []byte, ratio float64, jobID string) error {
	return printer.PrintPlatform(printerName, html, ratio, jobID)
}

// func PrintHTMLDocument(printerName string, htmlContent []byte, jobID string) error {
// 	// Validar dependencias según el sistema
// 	if err := ValidatePrintDependencies(); err != nil {
// 		return err
// 	}

// 	return printer.Print(printerName, htmlContent, jobID)
// }
