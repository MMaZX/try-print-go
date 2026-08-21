package queue

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"usqay-print-client/internal/printer"
)

const pollInterval = 500 * time.Millisecond

// NotifyFunc is called by the Worker when a job reaches a terminal state
// (PRINTED or ERROR). Must be non-blocking and safe for concurrent use.
type NotifyFunc func(jobID string, estado Estado, errMsg string)

// Worker polls the local SQLite queue and sends each pending job to the printer.
// It runs until ctx is cancelled. No error ever causes a panic — all failures
// are logged and persisted as ERROR state so the queue remains operational.
type Worker struct {
	repo          *Repository
	registry      *printer.Registry
	notify        NotifyFunc // may be nil
	capturePRN    bool
	capturePRNDir string
}

// NewWorker creates a Worker backed by repo and the printer registry.
// notify is called after each job reaches PRINTED or ERROR; pass nil to skip.
func NewWorker(repo *Repository, registry *printer.Registry, notify NotifyFunc, capturePRN bool, capturePRNDir string) *Worker {
	return &Worker{
		repo:          repo,
		registry:      registry,
		notify:        notify,
		capturePRN:    capturePRN,
		capturePRNDir: capturePRNDir,
	}
}

// Run blocks, polling for PENDING jobs every 500ms, until ctx is done.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	slog.Info("worker iniciado", "src", "WORKER", "intervalo", pollInterval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("worker detenido", "src", "WORKER")
			return
		case <-ticker.C:
			w.processNext()
		}
	}
}

func (w *Worker) processNext() {
	job, err := w.repo.NextPending()
	if err != nil {
		slog.Error("error consultando trabajo pendiente", "src", "WORKER", "error", err)
		return
	}
	if job == nil {
		return
	}

	// Wait for the server to deliver printer configuration before processing.
	if w.registry.Len() == 0 {
		slog.Debug("registro de impresoras vacío, esperando config del servidor", "src", "WORKER", "job_id", job.ID)
		return
	}

	p, ok := w.registry.Resolve(job.ImpresoraID)
	if !ok {
		slog.Error("la impresora_id del trabajo no está en la configuración enviada por el servidor para este terminal",
			"src", "WORKER",
			"job_id", job.ID,
			"impresora_id", job.ImpresoraID,
			"impresoras_configuradas", w.registry.IDs(),
		)
		w.finalize(job.ID, EstadoError, "impresora_id no registrada: "+job.ImpresoraID)
		return
	}

	slog.Info("procesando trabajo",
		"src", "WORKER", "job_id", job.ID, "tipo", job.TipoDocumento, "impresora_id", job.ImpresoraID)

	if err := w.repo.UpdateStatus(job.ID, EstadoProcessing, ""); err != nil {
		slog.Error("error actualizando a PROCESSING", "src", "WORKER", "job_id", job.ID, "error", err)
		return
	}

	// Fetch profile from local cache (SQLite)
	var prof *printer.DeviceProfile
	if job.ImpresoraID != "" {
		var err error
		prof, err = w.repo.GetProfile(job.ImpresoraID)
		if err != nil {
			slog.Warn("error obteniendo perfil de SQLite, se usará cascada", "src", "WORKER", "impresora_id", job.ImpresoraID, "error", err)
		}
	}

	// Choose Image/ESC/POS or plain-text renderer based on the printer's mode.
	var data []byte
	var renderErr error
	renderStart := time.Now()
	if p.Mode() == "text" {
		data, renderErr = renderText(prof, job.Payload)
	} else {
		data, renderErr = RenderImage(prof, job.Payload)
	}
	renderSecs := time.Since(renderStart).Seconds()

	if renderErr != nil {
		slog.Error("error renderizando payload", "src", "WORKER", "job_id", job.ID, "error", renderErr, "tiempo_render_sec", fmt.Sprintf("%.3fs", renderSecs))
		w.finalize(job.ID, EstadoError, renderErr.Error())
		return
	}

	if w.capturePRN && w.capturePRNDir != "" {
		if err := os.MkdirAll(w.capturePRNDir, 0755); err != nil {
			slog.Error("error creando directorio de captura PRN", "src", "WORKER", "dir", w.capturePRNDir, "error", err)
		} else {
			filePath := filepath.Join(w.capturePRNDir, job.ID+".prn")
			if err := os.WriteFile(filePath, data, 0644); err != nil {
				slog.Error("error escribiendo captura PRN", "src", "WORKER", "path", filePath, "error", err)
			} else {
				slog.Info("captura de bytes PRN guardada", "src", "WORKER", "path", filePath)
			}
		}
	}

	printStart := time.Now()
	printErr := p.Print(data)
	printSecs := time.Since(printStart).Seconds()
	totalSecs := renderSecs + printSecs

	if printErr != nil {
		slog.Error("fallo de impresión",
			"src", "WORKER",
			"job_id", job.ID,
			"error", printErr,
			"tiempo_render_sec", fmt.Sprintf("%.3fs", renderSecs),
			"tiempo_print_sec", fmt.Sprintf("%.3fs", printSecs),
			"tiempo_total_sec", fmt.Sprintf("%.3fs", totalSecs),
		)
		w.finalize(job.ID, EstadoError, printErr.Error())
		return
	}

	slog.Info("impresión exitosa",
		"src", "WORKER",
		"job_id", job.ID,
		"tamanio_bytes", len(data),
		"tiempo_render_sec", fmt.Sprintf("%.3fs", renderSecs),
		"tiempo_print_sec", fmt.Sprintf("%.3fs", printSecs),
		"tiempo_total_sec", fmt.Sprintf("%.3fs", totalSecs),
	)
	w.finalize(job.ID, EstadoPrinted, "")
}

// finalize updates the job state in SQLite and calls the notify callback.
func (w *Worker) finalize(jobID string, estado Estado, errMsg string) {
	if err := w.repo.UpdateStatus(jobID, estado, errMsg); err != nil {
		slog.Error("error guardando estado final", "job_id", jobID, "estado", estado, "error", err)
	}
	if w.notify != nil {
		w.notify(jobID, estado, errMsg)
	}
}
