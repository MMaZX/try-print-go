package logging

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DailyWriter is an io.Writer that writes to logs/YYYY-MM-DD.log and rolls
// over automatically at midnight.
type DailyWriter struct {
	logDir string
	file   *os.File
	date   string
}

// NewDailyWriter creates logDir if needed and opens today's log file.
func NewDailyWriter(logDir string) (*DailyWriter, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("crear directorio de logs %s: %w", logDir, err)
	}
	w := &DailyWriter{logDir: logDir}
	if err := w.openToday(); err != nil {
		return nil, err
	}
	return w, nil
}

// Write implements io.Writer. Rotates the file automatically on date change.
func (w *DailyWriter) Write(p []byte) (int, error) {
	today := time.Now().Format("2006-01-02")
	if today != w.date {
		_ = w.file.Close()
		if err := w.openToday(); err != nil {
			return 0, err
		}
	}
	return w.file.Write(p)
}

// Close closes the underlying log file.
func (w *DailyWriter) Close() error {
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

func (w *DailyWriter) openToday() error {
	today := time.Now().Format("2006-01-02")
	path := filepath.Join(w.logDir, today+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("abrir archivo de log %s: %w", path, err)
	}
	w.file = f
	w.date = today
	return nil
}

// ArchiveResult carries the outcome of a monthly archive operation.
type ArchiveResult struct {
	ZipPath  string
	Archived int
}

// ArchivePreviousMonth zips all .log files from the previous calendar month into
// logs/archive/YYYY/mes/YYYY-mes.zip and removes the originals.
// Safe to call every startup — skips if no files need archiving.
func ArchivePreviousMonth(logDir string) (*ArchiveResult, error) {
	prev := time.Now().AddDate(0, -1, 0)
	year := prev.Format("2006")
	monthName := spanishMonth(prev.Month())
	prefix := prev.Format("2006-01")

	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil, fmt.Errorf("leer directorio de logs: %w", err)
	}

	var toArchive []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".log") {
			toArchive = append(toArchive, filepath.Join(logDir, e.Name()))
		}
	}
	if len(toArchive) == 0 {
		return nil, nil
	}

	zipDir := filepath.Join(logDir, "archive", year, monthName)
	if err := os.MkdirAll(zipDir, 0o755); err != nil {
		return nil, fmt.Errorf("crear directorio de archivo: %w", err)
	}

	zipPath := filepath.Join(zipDir, fmt.Sprintf("%s-%s.zip", year, monthName))
	if err := createZip(zipPath, toArchive); err != nil {
		return nil, fmt.Errorf("crear zip: %w", err)
	}

	for _, f := range toArchive {
		_ = os.Remove(f)
	}

	return &ArchiveResult{ZipPath: zipPath, Archived: len(toArchive)}, nil
}

func createZip(dst string, files []string) error {
	zf, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("crear archivo zip: %w", err)
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	defer zw.Close()

	for _, src := range files {
		if err := addFileToZip(zw, src); err != nil {
			return err
		}
	}
	return nil
}

func addFileToZip(zw *zip.Writer, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("abrir %s: %w", src, err)
	}
	defer f.Close()

	w, err := zw.Create(filepath.Base(src))
	if err != nil {
		return fmt.Errorf("entrada zip para %s: %w", src, err)
	}
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("copiar %s al zip: %w", src, err)
	}
	return nil
}

var spanishMonthNames = [...]string{
	"", "enero", "febrero", "marzo", "abril", "mayo", "junio",
	"julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre",
}

func spanishMonth(m time.Month) string { return spanishMonthNames[m] }
