package queue

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"usqay-print-client/internal/printer"
)

// ErrDuplicate is returned by Insert when the job ID already exists in the local queue.
var ErrDuplicate = errors.New("trabajo duplicado")

// ErrNoConfig is returned by LoadConfig when no cached configuration exists in the local database.
var ErrNoConfig = errors.New("no hay configuración cacheada")

// ErrCachedConfigForeignTerminal is returned by LoadCachedConfig when the terminal_id
// stored in the cache does not match the terminal_id configured in config.json, meaning
// the cached configuration belongs to a different terminal and must be discarded.
var ErrCachedConfigForeignTerminal = errors.New("la configuración cacheada pertenece a otra terminal")

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
	Intentos      int
	NextRetryAt   *time.Time
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
		`INSERT INTO print_jobs (id, payload, tipo_documento, impresora_id, estado, intentos, next_retry_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Payload, job.TipoDocumento, job.ImpresoraID, EstadoPending, job.Intentos, job.NextRetryAt, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("insertar trabajo %s: %w", job.ID, ErrDuplicate)
		}
		return fmt.Errorf("insertar trabajo %s: %w", job.ID, err)
	}
	return nil
}

// Upsert replaces an existing job (or inserts a new one) resetting it to PENDING and resetting attempts.
// Used for reprints where the server explicitly requests re-processing.
func (r *Repository) Upsert(job PrintJob) error {
	_, err := r.db.Exec(
		`INSERT OR REPLACE INTO print_jobs (id, payload, tipo_documento, impresora_id, estado, intentos, next_retry_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 0, NULL, ?, ?)`,
		job.ID, job.Payload, job.TipoDocumento, job.ImpresoraID, EstadoPending, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert trabajo %s: %w", job.ID, err)
	}
	return nil
}

// Enqueue inserts a new job, or replaces an existing one when reimpresion is true.
// Shared by the WebSocket and local HTTP entry points so both apply the same duplicate/reprint semantics.
// Returns ErrDuplicate (via errors.Is) when reimpresion is false and the job ID already exists.
func (r *Repository) Enqueue(job PrintJob, reimpresion bool) error {
	if reimpresion {
		return r.Upsert(job)
	}
	return r.Insert(job)
}

// NextPending returns the oldest PENDING job whose next_retry_at is null or has passed,
// or nil if the queue is empty or all pending jobs are waiting for retry.
func (r *Repository) NextPending() (*PrintJob, error) {
	row := r.db.QueryRow(
		`SELECT id, payload, tipo_documento, impresora_id, estado, intentos, next_retry_at, COALESCE(error_msg,''), created_at, updated_at
		 FROM print_jobs
		 WHERE estado = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)
		 ORDER BY created_at
		 LIMIT 1`,
		EstadoPending, time.Now().UTC(),
	)
	var job PrintJob
	var nextRetry sql.NullTime
	err := row.Scan(
		&job.ID, &job.Payload, &job.TipoDocumento, &job.ImpresoraID, &job.Estado,
		&job.Intentos, &nextRetry,
		&job.ErrorMsg, &job.CreatedAt, &job.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consultar siguiente PENDING: %w", err)
	}
	if nextRetry.Valid {
		job.NextRetryAt = &nextRetry.Time
	}
	return &job, nil
}

// RecordRetry updates a job with an incremented attempt count, a future retry timestamp,
// and records the error message while keeping the state as PENDING.
func (r *Repository) RecordRetry(id string, intentos int, nextRetryAt time.Time, errorMsg string) error {
	_, err := r.db.Exec(
		`UPDATE print_jobs
		 SET estado = ?, intentos = ?, next_retry_at = ?, error_msg = ?, updated_at = ?
		 WHERE id = ?`,
		EstadoPending, intentos, nextRetryAt, errorMsg, time.Now().UTC(), id,
	)
	if err != nil {
		return fmt.Errorf("registrar reintento del trabajo %s: %w", id, err)
	}
	return nil
}

// UpdateStatus transitions a job to the given state and records an optional error message.
func (r *Repository) UpdateStatus(id string, estado Estado, errorMsg string) error {
	var query string
	if estado == EstadoPrinted || estado == EstadoError {
		query = `UPDATE print_jobs SET estado = ?, next_retry_at = NULL, error_msg = ?, updated_at = ? WHERE id = ?`
	} else {
		query = `UPDATE print_jobs SET estado = ?, error_msg = ?, updated_at = ? WHERE id = ?`
	}
	_, err := r.db.Exec(query, estado, errorMsg, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("actualizar trabajo %s a %s: %w", id, estado, err)
	}
	return nil
}

// UpdateStatusWithAttempts transitions a job to the given state, sets intentos, and records an optional error message.
func (r *Repository) UpdateStatusWithAttempts(id string, estado Estado, intentos int, errorMsg string) error {
	var query string
	if estado == EstadoPrinted || estado == EstadoError {
		query = `UPDATE print_jobs SET estado = ?, intentos = ?, next_retry_at = NULL, error_msg = ?, updated_at = ? WHERE id = ?`
	} else {
		query = `UPDATE print_jobs SET estado = ?, intentos = ?, error_msg = ?, updated_at = ? WHERE id = ?`
	}
	res, err := r.db.Exec(query, estado, intentos, errorMsg, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("actualizar trabajo %s a %s con %d intentos: %w", id, estado, intentos, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("verificar filas afectadas job %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("trabajo no encontrado para actualizar estado: %s", id)
	}
	return nil
}

// ListByStatus returns all jobs with the given state, ordered by creation time.
func (r *Repository) ListByStatus(estado Estado) ([]PrintJob, error) {
	rows, err := r.db.Query(
		`SELECT id, payload, tipo_documento, impresora_id, estado, intentos, next_retry_at, COALESCE(error_msg,''), created_at, updated_at
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
		var nextRetry sql.NullTime
		if err := rows.Scan(
			&job.ID, &job.Payload, &job.TipoDocumento, &job.ImpresoraID, &job.Estado,
			&job.Intentos, &nextRetry,
			&job.ErrorMsg, &job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan trabajo: %w", err)
		}
		if nextRetry.Valid {
			job.NextRetryAt = &nextRetry.Time
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// SaveProfile persists or updates a printer profile in the local database.
func (r *Repository) SaveProfile(impresoraID string, p printer.DeviceProfile) error {
	_, err := r.db.Exec(
		`INSERT OR REPLACE INTO printer_profiles (
			impresora_id, width_dots, dpi, char_width_dots, supports_cut,
			supports_drawer, supports_qr_native, supports_print_area, supports_raster
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		impresoraID, p.WidthDots, p.DPI, p.CharWidthDots,
		boolToInt(p.SupportsCut), boolToInt(p.SupportsDrawer), boolToInt(p.SupportsQRNative),
		boolToInt(p.SupportsPrintArea), boolToInt(p.SupportsRaster),
	)
	if err != nil {
		return fmt.Errorf("guardar perfil de impresora %s: %w", impresoraID, err)
	}
	return nil
}

// GetProfile retrieves a printer profile by its printer ID.
// Returns nil if no profile is cached for this ID.
func (r *Repository) GetProfile(impresoraID string) (*printer.DeviceProfile, error) {
	row := r.db.QueryRow(
		`SELECT width_dots, dpi, char_width_dots, supports_cut,
		        supports_drawer, supports_qr_native, supports_print_area, supports_raster
		 FROM printer_profiles
		 WHERE impresora_id = ?`,
		impresoraID,
	)
	var p printer.DeviceProfile
	var cut, drawer, qr, area, raster int
	err := row.Scan(
		&p.WidthDots, &p.DPI, &p.CharWidthDots, &cut, &drawer, &qr, &area, &raster,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("obtener perfil de impresora %s: %w", impresoraID, err)
	}
	p.SupportsCut = intToBool(cut)
	p.SupportsDrawer = intToBool(drawer)
	p.SupportsQRNative = intToBool(qr)
	p.SupportsPrintArea = intToBool(area)
	p.SupportsRaster = intToBool(raster)
	return &p, nil
}

// SaveConfig persists or updates the cached printer configuration in the local database.
func (r *Repository) SaveConfig(terminalID string, printersJSON []byte) error {
	_, err := r.db.Exec(
		`INSERT OR REPLACE INTO agent_config (id, terminal_id, printers_json, updated_at)
		 VALUES (1, ?, ?, ?)`,
		terminalID, string(printersJSON), time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("guardar configuración cacheada: %w", err)
	}
	return nil
}

// LoadConfig retrieves the cached printer configuration from the local database.
// Returns ErrNoConfig if no configuration has been cached yet.
func (r *Repository) LoadConfig() (terminalID string, printersJSON []byte, err error) {
	row := r.db.QueryRow(`SELECT terminal_id, printers_json FROM agent_config WHERE id = 1`)
	var tID string
	var pJSON string
	err = row.Scan(&tID, &pJSON)
	if err == sql.ErrNoRows {
		return "", nil, ErrNoConfig
	}
	if err != nil {
		return "", nil, fmt.Errorf("cargar configuración cacheada: %w", err)
	}
	return tID, []byte(pJSON), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool {
	return i != 0
}
