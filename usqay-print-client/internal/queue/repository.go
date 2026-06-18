package queue

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrDuplicate is returned by Insert when the job ID already exists in the local queue.
var ErrDuplicate = errors.New("trabajo duplicado")

// Estado represents the lifecycle state of a print job.
type Estado string

const (
	EstadoPending    Estado = "PENDING"
	EstadoProcessing Estado = "PROCESSING"
	EstadoPrinted    Estado = "PRINTED"
	EstadoError      Estado = "ERROR"
)

// PrintJob is a single print request stored in the local SQLite queue.
type PrintJob struct {
	ID            string
	Payload       string
	TipoDocumento string
	ImpresoraID   string // UUID from impresoras table; used to resolve the target printer
	Estado        Estado
	ErrorMsg      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Repository provides CRUD access to the print_jobs table.
type Repository struct {
	db *sql.DB
}

// NewRepository wraps a database connection.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// Insert persists a new job with state PENDING.
// Returns ErrDuplicate (via errors.Is) when the job ID already exists.
func (r *Repository) Insert(job PrintJob) error {
	_, err := r.db.Exec(
		`INSERT INTO print_jobs (id, payload, tipo_documento, impresora_id, estado, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Payload, job.TipoDocumento, job.ImpresoraID, EstadoPending, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("insertar trabajo %s: %w", job.ID, ErrDuplicate)
		}
		return fmt.Errorf("insertar trabajo %s: %w", job.ID, err)
	}
	return nil
}

// Upsert replaces an existing job (or inserts a new one) resetting it to PENDING.
// Used for reprints where the server explicitly requests re-processing.
func (r *Repository) Upsert(job PrintJob) error {
	_, err := r.db.Exec(
		`INSERT OR REPLACE INTO print_jobs (id, payload, tipo_documento, impresora_id, estado, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Payload, job.TipoDocumento, job.ImpresoraID, EstadoPending, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert trabajo %s: %w", job.ID, err)
	}
	return nil
}

// NextPending returns the oldest PENDING job, or nil if the queue is empty.
func (r *Repository) NextPending() (*PrintJob, error) {
	row := r.db.QueryRow(
		`SELECT id, payload, tipo_documento, impresora_id, estado, COALESCE(error_msg,''), created_at, updated_at
		 FROM print_jobs
		 WHERE estado = ?
		 ORDER BY created_at
		 LIMIT 1`,
		EstadoPending,
	)
	var job PrintJob
	err := row.Scan(
		&job.ID, &job.Payload, &job.TipoDocumento, &job.ImpresoraID, &job.Estado,
		&job.ErrorMsg, &job.CreatedAt, &job.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consultar siguiente PENDING: %w", err)
	}
	return &job, nil
}

// UpdateStatus transitions a job to the given state and records an optional error message.
func (r *Repository) UpdateStatus(id string, estado Estado, errorMsg string) error {
	_, err := r.db.Exec(
		`UPDATE print_jobs SET estado = ?, error_msg = ?, updated_at = ? WHERE id = ?`,
		estado, errorMsg, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("actualizar trabajo %s a %s: %w", id, estado, err)
	}
	return nil
}

// ListByStatus returns all jobs with the given state, ordered by creation time.
func (r *Repository) ListByStatus(estado Estado) ([]PrintJob, error) {
	rows, err := r.db.Query(
		`SELECT id, payload, tipo_documento, impresora_id, estado, COALESCE(error_msg,''), created_at, updated_at
		 FROM print_jobs
		 WHERE estado = ?
		 ORDER BY created_at`,
		estado,
	)
	if err != nil {
		return nil, fmt.Errorf("listar trabajos %s: %w", estado, err)
	}
	defer rows.Close()

	var jobs []PrintJob
	for rows.Next() {
		var job PrintJob
		if err := rows.Scan(
			&job.ID, &job.Payload, &job.TipoDocumento, &job.ImpresoraID, &job.Estado,
			&job.ErrorMsg, &job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan trabajo: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}
