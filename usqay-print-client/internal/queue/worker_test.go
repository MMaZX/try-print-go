package queue_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/queue"
)

type mockPrinter struct {
	mu               sync.Mutex
	mode             string
	failUntilAttempt int
	currentAttempt   int
	err              error
}

func (m *mockPrinter) Mode() string {
	if m.mode == "" {
		return "text"
	}
	return m.mode
}

func (m *mockPrinter) Print(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentAttempt++
	if m.currentAttempt < m.failUntilAttempt {
		return m.err
	}
	return nil
}

type notifyRecord struct {
	jobID  string
	estado queue.Estado
	errMsg string
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condición no cumplida tras %v", timeout)
}

func TestWorker_Retry(t *testing.T) {
	t.Run("Fallo transitorio se reintenta y pasa a PRINTED al tener éxito en el segundo intento", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "test.db")
		db, err := queue.Open(dbPath)
		if err != nil {
			t.Fatalf("queue.Open falló: %v", err)
		}
		defer db.Close()

		repo := queue.NewRepository(db)
		registry := printer.NewRegistry()
		mockP := &mockPrinter{
			mode:             "text",
			failUntilAttempt: 2,
			err:              errors.New("papel atascado"),
		}
		registry.Set("p1", mockP)

		var notifications []notifyRecord
		var notifyMu sync.Mutex
		notifyFn := func(jobID string, estado queue.Estado, errMsg string) {
			notifyMu.Lock()
			defer notifyMu.Unlock()
			notifications = append(notifications, notifyRecord{jobID, estado, errMsg})
		}

		w := queue.NewWorker(repo, registry, notifyFn, false, "", 3)

		now := time.Now().UTC()
		job := queue.PrintJob{
			ID:            "job-success-retry",
			Payload:       `{"body":[{"type":"text","value":"hola"}]}`,
			TipoDocumento: "comanda",
			ImpresoraID:   "p1",
			Estado:        queue.EstadoPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := repo.Insert(job); err != nil {
			t.Fatalf("Insert falló: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.Run(ctx)

		// Esperar que se ejecute el primer intento (falla y programa retry)
		waitFor(t, 2*time.Second, func() bool {
			pending, err := repo.ListByStatus(queue.EstadoPending)
			return err == nil && len(pending) == 1 && pending[0].Intentos == 1
		})

		notifyMu.Lock()
		if len(notifications) != 0 {
			t.Errorf("se esperaba 0 notificaciones tras fallo 1, got %d", len(notifications))
		}
		notifyMu.Unlock()

		// Verificar que el job en DB tiene intentos = 1 y next_retry_at en el futuro
		pending, err := repo.ListByStatus(queue.EstadoPending)
		if err != nil {
			t.Fatalf("ListByStatus falló: %v", err)
		}
		if len(pending) != 1 || pending[0].Intentos != 1 {
			t.Fatalf("job en DB esperado con intentos=1, got %+v", pending)
		}
		if pending[0].NextRetryAt == nil || !pending[0].NextRetryAt.After(now) {
			t.Errorf("job en DB esperado con next_retry_at en el futuro, got %v", pending[0].NextRetryAt)
		}

		// Adelantar next_retry_at para no esperar el backoff completo en el test
		if err := repo.RecordRetry("job-success-retry", 1, time.Now().UTC().Add(-1*time.Second), "papel atascado"); err != nil {
			t.Fatalf("RecordRetry falló: %v", err)
		}

		// Esperar segundo ciclo del worker (éxito)
		waitFor(t, 2*time.Second, func() bool {
			notifyMu.Lock()
			defer notifyMu.Unlock()
			return len(notifications) == 1
		})

		notifyMu.Lock()
		if notifications[0].estado != queue.EstadoPrinted {
			t.Errorf("notificación estado = %v, want PRINTED", notifications[0].estado)
		}
		notifyMu.Unlock()

		printed, err := repo.ListByStatus(queue.EstadoPrinted)
		if err != nil {
			t.Fatalf("ListByStatus PRINTED falló: %v", err)
		}
		if len(printed) != 1 {
			t.Errorf("se esperaba 1 job PRINTED en DB, got %d", len(printed))
		}
	})

	t.Run("Fallo persistente supera max_retries y pasa a ERROR definitivo con notificación al servidor", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "test.db")
		db, err := queue.Open(dbPath)
		if err != nil {
			t.Fatalf("queue.Open falló: %v", err)
		}
		defer db.Close()

		repo := queue.NewRepository(db)
		registry := printer.NewRegistry()
		mockP := &mockPrinter{
			mode:             "text",
			failUntilAttempt: 99,
			err:              errors.New("spooler no responde"),
		}
		registry.Set("p1", mockP)

		var notifications []notifyRecord
		var notifyMu sync.Mutex
		notifyFn := func(jobID string, estado queue.Estado, errMsg string) {
			notifyMu.Lock()
			defer notifyMu.Unlock()
			notifications = append(notifications, notifyRecord{jobID, estado, errMsg})
		}

		w := queue.NewWorker(repo, registry, notifyFn, false, "", 2) // max 2 intentos

		now := time.Now().UTC()
		job := queue.PrintJob{
			ID:            "job-max-retries",
			Payload:       `{"body":[{"type":"text","value":"hola"}]}`,
			TipoDocumento: "comanda",
			ImpresoraID:   "p1",
			Estado:        queue.EstadoPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := repo.Insert(job); err != nil {
			t.Fatalf("Insert falló: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.Run(ctx)

		// Intento 1 (falla -> programa retry)
		waitFor(t, 2*time.Second, func() bool {
			pending, err := repo.ListByStatus(queue.EstadoPending)
			return err == nil && len(pending) == 1 && pending[0].Intentos == 1
		})

		notifyMu.Lock()
		if len(notifications) != 0 {
			t.Errorf("intento 1 no debería notificar ERROR, got %d notificaciones", len(notifications))
		}
		notifyMu.Unlock()

		// Forzar vencimiento del backoff para el intento 2
		if err := repo.RecordRetry("job-max-retries", 1, time.Now().UTC().Add(-1*time.Second), "spooler no responde"); err != nil {
			t.Fatalf("RecordRetry falló: %v", err)
		}

		// Intento 2 (alcanza max_retries = 2 -> pasa a ERROR definitivo)
		waitFor(t, 2*time.Second, func() bool {
			notifyMu.Lock()
			defer notifyMu.Unlock()
			return len(notifications) == 1
		})

		notifyMu.Lock()
		if notifications[0].estado != queue.EstadoError {
			t.Errorf("notificación estado = %v, want ERROR", notifications[0].estado)
		}
		notifyMu.Unlock()

		errJobs, err := repo.ListByStatus(queue.EstadoError)
		if err != nil {
			t.Fatalf("ListByStatus ERROR falló: %v", err)
		}
		if len(errJobs) != 1 || errJobs[0].Intentos != 2 {
			t.Errorf("job en DB esperado en ERROR con intentos=2, got %+v", errJobs)
		}
	})

	t.Run("Error permanente (payload corrupto) pasa directamente a ERROR sin reintentos", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "test.db")
		db, err := queue.Open(dbPath)
		if err != nil {
			t.Fatalf("queue.Open falló: %v", err)
		}
		defer db.Close()

		repo := queue.NewRepository(db)
		registry := printer.NewRegistry()
		mockP := &mockPrinter{
			mode:             "text",
			failUntilAttempt: 1, // la impresora funciona, el error es del payload
		}
		registry.Set("p1", mockP)

		var notifications []notifyRecord
		var notifyMu sync.Mutex
		notifyFn := func(jobID string, estado queue.Estado, errMsg string) {
			notifyMu.Lock()
			defer notifyMu.Unlock()
			notifications = append(notifications, notifyRecord{jobID, estado, errMsg})
		}

		w := queue.NewWorker(repo, registry, notifyFn, false, "", 3)

		now := time.Now().UTC()
		job := queue.PrintJob{
			ID:            "job-corrupt-payload",
			Payload:       `esto no es json`,
			TipoDocumento: "comanda",
			ImpresoraID:   "p1",
			Estado:        queue.EstadoPending,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := repo.Insert(job); err != nil {
			t.Fatalf("Insert falló: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go w.Run(ctx)

		waitFor(t, 2*time.Second, func() bool {
			notifyMu.Lock()
			defer notifyMu.Unlock()
			return len(notifications) == 1
		})

		notifyMu.Lock()
		if notifications[0].estado != queue.EstadoError {
			t.Errorf("estado = %v, want ERROR", notifications[0].estado)
		}
		notifyMu.Unlock()
	})
}
