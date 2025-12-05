package printer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func PrintPlatform(printerName string, htmlContent []byte, ratio float64, jobID string) error {
	tmpHTML := filepath.Join(os.TempDir(), fmt.Sprintf("usqay_%s.html", jobID))
	tmpPDF := tmpHTML + ".pdf"

	// Guardar HTML temporal
	if err := os.WriteFile(tmpHTML, htmlContent, 0644); err != nil {
		return fmt.Errorf("error creando HTML temporal: %v", err)
	}

	// Convertir HTML → PDF
	// Lo mismo aplica aquí para asegurar que wkhtmltopdf funcione correctamente.
	cmdPDF := exec.Command("wkhtmltopdf", "--enable-local-file-access", tmpHTML, tmpPDF)
	if out, err := cmdPDF.CombinedOutput(); err != nil {
		return fmt.Errorf("wkhtmltopdf error: %v\n%s", err, string(out))
	}

	// Enviar a CUPS
	cmdPrint := exec.Command("lp", "-d", printerName, tmpPDF)
	if out, err := cmdPrint.CombinedOutput(); err != nil {
		return fmt.Errorf("error imprimiendo con lp: %v\n%s", err, string(out))
	}

	return nil
}
