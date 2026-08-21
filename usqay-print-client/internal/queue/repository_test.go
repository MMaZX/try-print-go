package queue_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"usqay-print-client/internal/queue"
)

func setupTestDB(t *testing.T) *queue.Repository {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := queue.Open(dbPath)
	if err != nil {
		t.Fatalf("error abriendo db de test: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})
	return queue.NewRepository(db)
}

func TestRepository_Config(t *testing.T) {
	t.Run("LoadConfig en DB recién creada retorna ErrNoConfig", func(t *testing.T) {
		repo := setupTestDB(t)

		tID, pJSON, err := repo.LoadConfig()
		if err == nil {
			t.Fatalf("se esperaba error, got nil (tID: %s, pJSON: %s)", tID, string(pJSON))
		}
		if !errors.Is(err, queue.ErrNoConfig) {
			t.Errorf("esperaba errors.Is(err, queue.ErrNoConfig), got %v", err)
		}
		if tID != "" {
			t.Errorf("esperaba terminalID vacío, got %q", tID)
		}
		if pJSON != nil {
			t.Errorf("esperaba printersJSON nil, got %v", pJSON)
		}
	})

	t.Run("SaveConfig seguido de LoadConfig round-trip exacto", func(t *testing.T) {
		repo := setupTestDB(t)

		terminalID := "term-01"
		printersJSON := []byte(`[{"id":"printer-1","tipo":"RED","addr":"192.168.1.100:9100","mode":"escpos"}]`)

		if err := repo.SaveConfig(terminalID, printersJSON); err != nil {
			t.Fatalf("SaveConfig falló: %v", err)
		}

		gotTermID, gotPrintersJSON, err := repo.LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig falló: %v", err)
		}

		if gotTermID != terminalID {
			t.Errorf("terminalID = %q, want %q", gotTermID, terminalID)
		}
		if !bytes.Equal(gotPrintersJSON, printersJSON) {
			t.Errorf("printersJSON = %s, want %s", string(gotPrintersJSON), string(printersJSON))
		}
	})

	t.Run("Segundo SaveConfig sobrescribe el anterior (fila única)", func(t *testing.T) {
		repo := setupTestDB(t)

		// Primera versión
		if err := repo.SaveConfig("term-01", []byte(`[{"id":"p1"}]`)); err != nil {
			t.Fatalf("primer SaveConfig falló: %v", err)
		}

		// Segunda versión (sobrescribe)
		updatedTermID := "term-02"
		updatedJSON := []byte(`[{"id":"p2","tipo":"USB","addr":"POS-80"}]`)
		if err := repo.SaveConfig(updatedTermID, updatedJSON); err != nil {
			t.Fatalf("segundo SaveConfig falló: %v", err)
		}

		gotTermID, gotPrintersJSON, err := repo.LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig falló: %v", err)
		}

		if gotTermID != updatedTermID {
			t.Errorf("terminalID = %q, want %q", gotTermID, updatedTermID)
		}
		if !bytes.Equal(gotPrintersJSON, updatedJSON) {
			t.Errorf("printersJSON = %s, want %s", string(gotPrintersJSON), string(updatedJSON))
		}
	})
}
