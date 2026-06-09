// Package queue manages the local SQLite job queue and the background worker.
package queue

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS print_jobs (
    id              TEXT PRIMARY KEY,
    payload         TEXT NOT NULL,
    tipo_documento  TEXT NOT NULL DEFAULT 'comanda',
    estado          TEXT NOT NULL DEFAULT 'PENDING',
    error_msg       TEXT,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_print_jobs_estado ON print_jobs (estado, created_at);
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
	return db, nil
}
