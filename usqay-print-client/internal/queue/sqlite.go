// Package queue manages the local SQLite job queue and the background worker.
package queue

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS print_jobs (
    id              TEXT PRIMARY KEY,
    payload         TEXT NOT NULL,
    tipo_documento  TEXT NOT NULL DEFAULT 'comanda',
    impresora_id    TEXT NOT NULL DEFAULT '',
    estado          TEXT NOT NULL DEFAULT 'PENDING',
    error_msg       TEXT,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_print_jobs_estado ON print_jobs (estado, created_at);

CREATE TABLE IF NOT EXISTS printer_profiles (
    impresora_id          TEXT PRIMARY KEY,
    width_dots            INTEGER NOT NULL,
    dpi                   INTEGER NOT NULL,
    char_width_dots       INTEGER NOT NULL,
    supports_cut          INTEGER NOT NULL,
    supports_drawer       INTEGER NOT NULL,
    supports_qr_native    INTEGER NOT NULL,
    supports_print_area   INTEGER NOT NULL,
    supports_raster       INTEGER NOT NULL
);
`

// Open opens (or creates) the SQLite database at dbPath and runs schema migrations.
func Open(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("abrir sqlite %s: %w", dbPath, err)
	}
	// Single writer connection prevents SQLITE_BUSY on concurrent access.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("ejecutar migración de schema: %w", err)
	}

	// Add impresora_id column to existing databases created before this migration.
	// SQLite does not support ADD COLUMN IF NOT EXISTS, so we ignore the
	// "duplicate column name" error that fires when the column already exists.
	if _, err := db.Exec(`ALTER TABLE print_jobs ADD COLUMN impresora_id TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			db.Close()
			return nil, fmt.Errorf("migrar columna impresora_id: %w", err)
		}
	}

	return db, nil
}
