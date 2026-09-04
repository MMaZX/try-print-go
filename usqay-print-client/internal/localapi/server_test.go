package localapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"usqay-print-client/internal/config"
	"usqay-print-client/internal/localapi"
	"usqay-print-client/internal/queue"
)

// setupTestServer wires a Server against a fresh SQLite DB and returns it alongside
// the repository, so tests can assert on the resulting queue state.
func setupTestServer(t *testing.T, cfg *config.Config) (http.Handler, *queue.Repository) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := queue.Open(dbPath)
	if err != nil {
		t.Fatalf("error abriendo DB de test: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	repo := queue.NewRepository(db)
	if cfg.LocalAPIPort == 0 {
		cfg.LocalAPIPort = config.DefaultLocalAPIPort
	}

	return localapi.NewServer(cfg, repo).Handler(), repo
}

func postPrint(t *testing.T, handler http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/print", bytes.NewBufferString(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestServer_HandlePrint(t *testing.T) {
	validBody := `{"job_id":"job-1","impresora_name_id":"printer-1","documento_slug":"COMANDA","payload":{"mesa":1}}`

	t.Run("job válido sin token configurado responde 200 y encola PENDING", func(t *testing.T) {
		handler, repo := setupTestServer(t, &config.Config{})

		rec := postPrint(t, handler, validBody, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			JobID  string `json:"job_id"`
			Estado string `json:"estado"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decodificar respuesta: %v", err)
		}
		if resp.JobID != "job-1" || resp.Estado != "recibido" {
			t.Errorf("respuesta = %+v, want job_id=job-1 estado=recibido", resp)
		}

		job, err := repo.NextPending()
		if err != nil {
			t.Fatalf("NextPending falló: %v", err)
		}
		if job == nil || job.ID != "job-1" || job.TipoDocumento != "comanda" || job.ImpresoraID != "printer-1" {
			t.Fatalf("job encolado inesperado: %+v", job)
		}
	})

	t.Run("campos requeridos faltantes responden 400", func(t *testing.T) {
		cases := []struct {
			name string
			body string
		}{
			{"sin job_id", `{"impresora_name_id":"printer-1","documento_slug":"comanda","payload":{"mesa":1}}`},
			{"sin impresora_name_id", `{"job_id":"job-2","documento_slug":"comanda","payload":{"mesa":1}}`},
			{"sin documento_slug", `{"job_id":"job-2","impresora_name_id":"printer-1","payload":{"mesa":1}}`},
			{"sin payload", `{"job_id":"job-2","impresora_name_id":"printer-1","documento_slug":"comanda"}`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				handler, _ := setupTestServer(t, &config.Config{})
				rec := postPrint(t, handler, tc.body, nil)
				if rec.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
				}
			})
		}
	})

	t.Run("JSON malformado responde 400", func(t *testing.T) {
		handler, _ := setupTestServer(t, &config.Config{})
		rec := postPrint(t, handler, `{no-es-json`, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("job_id duplicado responde 200 idempotente sin duplicar fila", func(t *testing.T) {
		handler, repo := setupTestServer(t, &config.Config{})

		if rec := postPrint(t, handler, validBody, nil); rec.Code != http.StatusOK {
			t.Fatalf("primer POST falló: status %d", rec.Code)
		}
		rec := postPrint(t, handler, validBody, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("segundo POST (duplicado) status = %d, want 200, body: %s", rec.Code, rec.Body.String())
		}

		jobs, err := repo.ListByStatus(queue.EstadoPending)
		if err != nil {
			t.Fatalf("ListByStatus falló: %v", err)
		}
		if len(jobs) != 1 {
			t.Fatalf("esperaba 1 job en PENDING tras duplicado, got %d", len(jobs))
		}
	})

	t.Run("reimpresion=true reemplaza el job existente", func(t *testing.T) {
		handler, repo := setupTestServer(t, &config.Config{})

		if rec := postPrint(t, handler, validBody, nil); rec.Code != http.StatusOK {
			t.Fatalf("POST inicial falló: status %d", rec.Code)
		}
		reprintBody := `{"job_id":"job-1","impresora_name_id":"printer-1","documento_slug":"comanda","payload":{"mesa":2},"reimpresion":true}`
		rec := postPrint(t, handler, reprintBody, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST de reimpresión status = %d, want 200, body: %s", rec.Code, rec.Body.String())
		}

		job, err := repo.NextPending()
		if err != nil {
			t.Fatalf("NextPending falló: %v", err)
		}
		if job == nil || job.Payload != `{"mesa":2}` {
			t.Fatalf("esperaba job reemplazado con nuevo payload, got %+v", job)
		}
	})

	t.Run("token configurado exige header correcto", func(t *testing.T) {
		handler, _ := setupTestServer(t, &config.Config{LocalAPIToken: "secreto"})

		t.Run("sin header responde 401", func(t *testing.T) {
			rec := postPrint(t, handler, validBody, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})

		t.Run("header incorrecto responde 401", func(t *testing.T) {
			rec := postPrint(t, handler, validBody, map[string]string{"X-Local-Api-Token": "otro"})
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})

		t.Run("header correcto responde 200", func(t *testing.T) {
			rec := postPrint(t, handler, validBody, map[string]string{"X-Local-Api-Token": "secreto"})
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
			}
		})
	})
}
