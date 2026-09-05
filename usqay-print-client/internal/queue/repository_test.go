package queue_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"
	"time"

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

func TestRepository_Enqueue(t *testing.T) {
	baseJob := func(id string) queue.PrintJob {
		now := time.Now().UTC()
		return queue.PrintJob{
			ID:            id,
			TipoDocumento: "comanda",
			ImpresoraID:   "printer-1",
			Payload:       `{"mesa":1}`,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
	}

	t.Run("reimpresion=false inserta un job nuevo", func(t *testing.T) {
		repo := setupTestDB(t)

		if err := repo.Enqueue(baseJob("job-1"), false); err != nil {
			t.Fatalf("Enqueue falló: %v", err)
		}

		job, err := repo.NextPending()
		if err != nil {
			t.Fatalf("NextPending falló: %v", err)
		}
		if job == nil || job.ID != "job-1" {
			t.Fatalf("esperaba job-1 en PENDING, got %+v", job)
		}
	})

	t.Run("reimpresion=false con job_id duplicado devuelve ErrDuplicate", func(t *testing.T) {
		repo := setupTestDB(t)

		if err := repo.Enqueue(baseJob("job-2"), false); err != nil {
			t.Fatalf("primer Enqueue falló: %v", err)
		}

		err := repo.Enqueue(baseJob("job-2"), false)
		if !errors.Is(err, queue.ErrDuplicate) {
			t.Errorf("esperaba errors.Is(err, queue.ErrDuplicate), got %v", err)
		}
	})

	t.Run("reimpresion=true reemplaza un job existente", func(t *testing.T) {
		repo := setupTestDB(t)

		if err := repo.Enqueue(baseJob("job-3"), false); err != nil {
			t.Fatalf("Enqueue inicial falló: %v", err)
		}
		if err := repo.UpdateStatus("job-3", queue.EstadoPrinted, ""); err != nil {
			t.Fatalf("UpdateStatus falló: %v", err)
		}

		replacement := baseJob("job-3")
		replacement.Payload = `{"mesa":2}`
		if err := repo.Enqueue(replacement, true); err != nil {
			t.Fatalf("Enqueue con reimpresion falló: %v", err)
		}

		job, err := repo.NextPending()
		if err != nil {
			t.Fatalf("NextPending falló: %v", err)
		}
		if job == nil || job.ID != "job-3" || job.Payload != `{"mesa":2}` {
			t.Fatalf("esperaba job-3 reemplazado y en PENDING, got %+v", job)
		}
	})
}

func TestRepository_Retry(t *testing.T) {
	t.Run("RecordRetry actualiza intentos y next_retry_at", func(t *testing.T) {
		repo := setupTestDB(t)

		now := time.Now().UTC().Truncate(time.Second)
		job := queue.PrintJob{
			ID:            "job-retry-1",
			Payload:       `{"body":[{"type":"text","value":"hola"}]}`,
			TipoDocumento: "comanda",
			ImpresoraID:   "impr-1",
			Estado:        queue.EstadoPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := repo.Insert(job); err != nil {
			t.Fatalf("Insert falló: %v", err)
		}

		nextRetry := now.Add(5 * time.Second)
		if err := repo.RecordRetry("job-retry-1", 1, nextRetry, "impresora desconectada"); err != nil {
			t.Fatalf("RecordRetry falló: %v", err)
		}

		pending, err := repo.ListByStatus(queue.EstadoPending)
		if err != nil {
			t.Fatalf("ListByStatus falló: %v", err)
		}
		if len(pending) != 1 {
			t.Fatalf("se esperaba 1 job PENDING, got %d", len(pending))
		}
		if pending[0].Intentos != 1 {
			t.Errorf("intentos = %d, want 1", pending[0].Intentos)
		}
		if pending[0].ErrorMsg != "impresora desconectada" {
			t.Errorf("error_msg = %q, want 'impresora desconectada'", pending[0].ErrorMsg)
		}
		if pending[0].NextRetryAt == nil || !pending[0].NextRetryAt.Equal(nextRetry) {
			t.Errorf("next_retry_at = %v, want %v", pending[0].NextRetryAt, nextRetry)
		}
	})

	t.Run("NextPending no bloquea la cola FIFO cuando un job tiene next_retry_at en el futuro", func(t *testing.T) {
		repo := setupTestDB(t)

		t0 := time.Now().UTC().Add(-10 * time.Second).Truncate(time.Second)
		t1 := t0.Add(2 * time.Second)

		job1 := queue.PrintJob{
			ID:            "job-1",
			Payload:       `{"body":[{"type":"text","value":"item 1"}]}`,
			TipoDocumento: "comanda",
			ImpresoraID:   "impr-1",
			CreatedAt:     t0,
			UpdatedAt:     t0,
		}
		job2 := queue.PrintJob{
			ID:            "job-2",
			Payload:       `{"body":[{"type":"text","value":"item 2"}]}`,
			TipoDocumento: "comanda",
			ImpresoraID:   "impr-1",
			CreatedAt:     t1,
			UpdatedAt:     t1,
		}

		if err := repo.Insert(job1); err != nil {
			t.Fatalf("Insert job1 falló: %v", err)
		}
		if err := repo.Insert(job2); err != nil {
			t.Fatalf("Insert job2 falló: %v", err)
		}

		// Job 1 falla y se programa para 10 segundos en el futuro
		futureRetry := time.Now().UTC().Add(10 * time.Second)
		if err := repo.RecordRetry("job-1", 1, futureRetry, "papel agotado"); err != nil {
			t.Fatalf("RecordRetry job1 falló: %v", err)
		}

		// NextPending debe devolver Job 2 (Job 1 está en backoff)
		next, err := repo.NextPending()
		if err != nil {
			t.Fatalf("NextPending falló: %v", err)
		}
		if next == nil {
			t.Fatal("NextPending devolvió nil, se esperaba job-2")
		}
		if next.ID != "job-2" {
			t.Errorf("NextPending ID = %q, want 'job-2' (FIFO no bloqueante)", next.ID)
		}
	})

	t.Run("NextPending devuelve el job una vez que next_retry_at ha expirado", func(t *testing.T) {
		repo := setupTestDB(t)

		now := time.Now().UTC().Add(-10 * time.Second).Truncate(time.Second)
		job := queue.PrintJob{
			ID:            "job-expired-retry",
			Payload:       `{"body":[{"type":"text","value":"item 1"}]}`,
			TipoDocumento: "comanda",
			ImpresoraID:   "impr-1",
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := repo.Insert(job); err != nil {
			t.Fatalf("Insert falló: %v", err)
		}

		// Retry programado en el pasado (ya expiró)
		pastRetry := time.Now().UTC().Add(-2 * time.Second)
		if err := repo.RecordRetry("job-expired-retry", 1, pastRetry, "error temporal"); err != nil {
			t.Fatalf("RecordRetry falló: %v", err)
		}

		next, err := repo.NextPending()
		if err != nil {
			t.Fatalf("NextPending falló: %v", err)
		}
		if next == nil {
			t.Fatal("NextPending devolvió nil para job con retry expirado")
		}
		if next.ID != "job-expired-retry" {
			t.Errorf("NextPending ID = %q, want 'job-expired-retry'", next.ID)
		}
	})
}

func TestRepository_Delete(t *testing.T) {
	t.Run("Delete elimina trabajo existente y es idempotente", func(t *testing.T) {
		repo := setupTestDB(t)

		job := queue.PrintJob{
			ID:            "job-del-1",
			Payload:       `{"body":[{"type":"text","value":"hola"}]}`,
			TipoDocumento: "comanda",
			ImpresoraID:   "impr-1",
			Estado:        queue.EstadoPending,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		}
		if err := repo.Insert(job); err != nil {
			t.Fatalf("Insert falló: %v", err)
		}

		if err := repo.Delete("job-del-1"); err != nil {
			t.Fatalf("Delete falló: %v", err)
		}

		// Verificar que ya no existe
		pending, err := repo.ListByStatus(queue.EstadoPending)
		if err != nil {
			t.Fatalf("ListByStatus falló: %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("se esperaba 0 jobs, got %d", len(pending))
		}

		// Idempotencia: borrar nuevamente no debe retornar error
		if err := repo.Delete("job-del-1"); err != nil {
			t.Errorf("Delete segundo llamado retornó error: %v", err)
		}
	})

	t.Run("DeleteBatch elimina múltiples trabajos", func(t *testing.T) {
		repo := setupTestDB(t)

		for _, id := range []string{"batch-1", "batch-2", "batch-3"} {
			job := queue.PrintJob{
				ID:            id,
				Payload:       `{}`,
				TipoDocumento: "boleta",
				ImpresoraID:   "impr-1",
				Estado:        queue.EstadoPrinted,
				CreatedAt:     time.Now().UTC(),
				UpdatedAt:     time.Now().UTC(),
			}
			if err := repo.Insert(job); err != nil {
				t.Fatalf("Insert falló: %v", err)
			}
		}

		if err := repo.DeleteBatch([]string{"batch-1", "batch-2"}); err != nil {
			t.Fatalf("DeleteBatch falló: %v", err)
		}

		pending, err := repo.ListByStatus(queue.EstadoPending)
		if err != nil {
			t.Fatalf("ListByStatus falló: %v", err)
		}
		if len(pending) != 1 || pending[0].ID != "batch-3" {
			t.Errorf("se esperaba solo batch-3 restante, got %+v", pending)
		}
	})
}

