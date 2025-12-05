package main

import (
	"errors"
	"fmt"
	"os"
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
	if err := ValidatePrintDependencies(); err != nil {
		return err
	}

	pdfPath, err := htmlToPDFTemp(html)
	if err != nil {
		return err
	}
	defer os.Remove(pdfPath)

	return printer.PrintPlatform(printerName, []byte(pdfPath), ratio, jobID)
}

func htmlToPDFTemp(html []byte) (string, error) {
	tmpHTML, err := os.CreateTemp("", "doc-*.html")
	if err != nil {
		return "", err
	}
	defer tmpHTML.Close()

	if _, err := tmpHTML.Write(html); err != nil {
		return "", err
	}

	outPDF := tmpHTML.Name() + ".pdf"

	// Añade la opción para permitir acceso a archivos locales (lo que necesita para About:blank y otros recursos si los hubiera)
	cmd := exec.Command("wkhtmltopdf", "--enable-local-file-access", tmpHTML.Name(), outPDF)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("wkhtmltopdf error: %v\n%s", err, output)
	}

	return outPDF, nil
}
