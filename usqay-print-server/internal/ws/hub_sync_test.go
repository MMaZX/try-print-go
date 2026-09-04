package ws_test

import (
	"encoding/json"
	"testing"

	"usqay-print-server/internal/config"
	"usqay-print-server/internal/ws"
)

func TestHub_HandleSync_ReflectsPrintedState(t *testing.T) {
	hub := ws.NewHub(&config.Config{})

	job, err := hub.EnqueueLaravel(
		"job-sync-1",
		"empresa-e2e",
		"caja-e2e",
		"impresora-e2e",
		"RED",
		"COMANDA",
		json.RawMessage(`{}`),
		300,
		false,
	)
	if err != nil {
		t.Fatalf("EnqueueLaravel falló: %v", err)
	}

	// Precondición: el terminal no está conectado, el job queda en estado "pendiente"
	summary := hub.JobSummary()
	if len(summary) != 1 {
		t.Fatalf("JobSummary len = %d, want 1", len(summary))
	}
	if summary[0].Estado != "pendiente" {
		t.Fatalf("job estado antes de sync = %q, want 'pendiente'", summary[0].Estado)
	}
	if pending := hub.PendingJobsCount("empresa-e2e", "caja-e2e"); pending != 1 {
		t.Fatalf("PendingJobsCount antes de sync = %d, want 1", pending)
	}

	// Ejecutar HandleSync simulando el reporte del cliente al reconectar
	hub.HandleSync("empresa-e2e", "caja-e2e", []string{job.ID}, nil)

	// Aserción (AC #3): el servidor refleja el job como impreso
	summaryAfter := hub.JobSummary()
	if len(summaryAfter) != 1 {
		t.Fatalf("JobSummary tras sync len = %d, want 1", len(summaryAfter))
	}
	if summaryAfter[0].Estado != "impreso" {
		t.Errorf("job estado tras sync = %q, want 'impreso'", summaryAfter[0].Estado)
	}
	if pendingAfter := hub.PendingJobsCount("empresa-e2e", "caja-e2e"); pendingAfter != 0 {
		t.Errorf("PendingJobsCount tras sync = %d, want 0", pendingAfter)
	}
}

func TestHub_HandleSync_IgnoresUnknownOrForeignJobID(t *testing.T) {
	hub := ws.NewHub(&config.Config{})

	job, err := hub.EnqueueLaravel(
		"job-sync-2",
		"empresa-e2e",
		"caja-e2e",
		"impresora-e2e",
		"RED",
		"COMANDA",
		json.RawMessage(`{}`),
		300,
		false,
	)
	if err != nil {
		t.Fatalf("EnqueueLaravel falló: %v", err)
	}

	// 1. Sync con ID inexistente no afecta el job real
	hub.HandleSync("empresa-e2e", "caja-e2e", []string{"id-inexistente"}, nil)

	summary := hub.JobSummary()
	if len(summary) != 1 || summary[0].Estado != "pendiente" {
		t.Errorf("job estado tras sync desconocido = %q, want 'pendiente'", summary[0].Estado)
	}

	// 2. Sync de otra terminal (misma empresa) intentando confirmar el job de caja-e2e es ignorado
	hub.HandleSync("empresa-e2e", "otra-terminal", []string{job.ID}, nil)

	summaryAfterForeign := hub.JobSummary()
	if len(summaryAfterForeign) != 1 || summaryAfterForeign[0].Estado != "pendiente" {
		t.Errorf("job estado tras sync desde terminal ajena = %q, want 'pendiente'", summaryAfterForeign[0].Estado)
	}

	// 3. Sync desde OTRA EMPRESA con el mismo terminal_id="caja-e2e" tampoco
	// debe poder confirmar el job — es exactamente el escenario cross-tenant
	// que motivó la key compuesta business_id+terminal_id.
	hub.HandleSync("otra-empresa", "caja-e2e", []string{job.ID}, nil)

	summaryAfterForeignBusiness := hub.JobSummary()
	if len(summaryAfterForeignBusiness) != 1 || summaryAfterForeignBusiness[0].Estado != "pendiente" {
		t.Errorf("job estado tras sync desde OTRA EMPRESA con mismo terminal_id = %q, want 'pendiente' (no debe cruzar tenants)", summaryAfterForeignBusiness[0].Estado)
	}

	if pending := hub.PendingJobsCount("empresa-e2e", "caja-e2e"); pending != 1 {
		t.Errorf("PendingJobsCount = %d, want 1", pending)
	}
}
