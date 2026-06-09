package queue

import (
	"context"
	"log/slog"
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
	repo    *Repository
	printer printer.Printer
	notify  NotifyFunc // may be nil
}

// NewWorker creates a Worker backed by repo and p.
// notify is called after each job reaches PRINTED or ERROR; pass nil to skip.
func NewWorker(repo *Repository, p printer.Printer, notify NotifyFunc) *Worker {
	return &Worker{repo: repo, printer: p, notify: notify}
}

// Run blocks, polling for PENDING jobs every 500ms, until ctx is done.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	slog.Info("worker iniciado", "intervalo", pollInterval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("worker detenido")
			return
		case <-ticker.C:
			w.processNext()
		}
	}
}

func (w *Worker) processNext() {
	job, err := w.repo.NextPending()
	if err != nil {
		slog.Error("error consultando trabajo pendiente", "error", err)
		return
	}
	if job == nil {
		return
	}

	slog.Info("procesando trabajo", "job_id", job.ID, "tipo", job.TipoDocumento)

	if err := w.repo.UpdateStatus(job.ID, EstadoProcessing, ""); err != nil {
		slog.Error("error actualizando a PROCESSING", "job_id", job.ID, "error", err)
		return
	}

	data, renderErr := render(job.TipoDocumento, job.Payload)
	if renderErr != nil {
		slog.Error("error renderizando payload", "job_id", job.ID, "error", renderErr)
		w.finalize(job.ID, EstadoError, renderErr.Error())
		return
	}

	start := time.Now()
	printErr := w.printer.Print(data)
	duration := time.Since(start)

	if printErr != nil {
		slog.Error("fallo de impresión", "job_id", job.ID, "error", printErr, "duracion", duration)
		w.finalize(job.ID, EstadoError, printErr.Error())
		return
	}

	slog.Info("impresión exitosa", "job_id", job.ID, "duracion", duration)
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
