package ws

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"usqay-print-client/internal/config"
	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/queue"
)

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

func TestE2E_DropAndReconnect(t *testing.T) {
	// 1. Levantar impresora fake vía TCP puro
	printerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("error iniciando listener TCP para impresora fake: %v", err)
	}
	defer printerListener.Close()

	go func() {
		for {
			conn, err := printerListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
			}(conn)
		}
	}()

	printerAddr := printerListener.Addr().String()

	// 2. Levantar servidor WebSocket falso
	var (
		connCount       atomic.Int32
		allowReconnect  = make(chan struct{})
		reconnectSyncCh = make(chan SyncMsg, 1)
		dropNow         = make(chan struct{})
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if connCount.Load() >= 1 {
			select {
			case <-allowReconnect:
			case <-r.Context().Done():
				return
			}
		}

		wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Logf("websocket.Accept error: %v", err)
			return
		}
		defer wsConn.CloseNow()

		count := connCount.Add(1)

		// Paso 1: Leer register
		var reg RegisterMsg
		if err := wsjson.Read(r.Context(), wsConn, &reg); err != nil {
			t.Logf("error leyendo register en conn %d: %v", count, err)
			return
		}

		// Paso 2: Leer sync
		var syncMsg SyncMsg
		if err := wsjson.Read(r.Context(), wsConn, &syncMsg); err != nil {
			t.Logf("error leyendo sync en conn %d: %v", count, err)
			return
		}

		// Paso 3: Enviar config con impresora fake
		cfgMsg := ConfigMsg{
			Type:       TypeConfig,
			TerminalID: "caja-e2e",
			Printers: []PrinterSpec{
				{
					ID:   "impresora-e2e",
					Tipo: "RED",
					Addr: printerAddr,
					Mode: "escpos",
				},
			},
		}
		if err := wsjson.Write(r.Context(), wsConn, cfgMsg); err != nil {
			t.Logf("error enviando config en conn %d: %v", count, err)
			return
		}

		if count == 1 {
			// Primera conexión: enviar job de impresión
			payload := json.RawMessage(`{"options":{"cut":false,"drawer":false},"paper_properties":{"width":80.0},"body":[{"type":"text","value":"E2E DROP-RECONNECT"}]}`)
			printMsg := PrintJobMsg{
				Type:            TypePrint,
				JobID:           "e2e-job-1",
				TipoDocumento:   "comanda",
				ImpresoraNameID: "impresora-e2e",
				Payload:         payload,
			}
			if err := wsjson.Write(r.Context(), wsConn, printMsg); err != nil {
				t.Logf("error enviando print job en conn %d: %v", count, err)
				return
			}

			// Loop no bloqueante para descartar mensajes adicionales (ACKs)
			readDone := make(chan struct{})
			go func() {
				defer close(readDone)
				for {
					var discard json.RawMessage
					if err := wsjson.Read(r.Context(), wsConn, &discard); err != nil {
						return
					}
				}
			}()

			// Esperar señal de corte del test
			select {
			case <-dropNow:
				_ = wsConn.Close(websocket.StatusNormalClosure, "corte simulado")
			case <-r.Context().Done():
			}
			<-readDone
		} else {
			// Segunda conexión (reconexión): publicar SyncMsg para verificación
			select {
			case reconnectSyncCh <- syncMsg:
			default:
			}

			// Mantener conexión abierta y descartar lecturas
			for {
				var discard json.RawMessage
				if err := wsjson.Read(r.Context(), wsConn, &discard); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()

	// 3. Ensamblar componentes reales del cliente
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	registry := printer.NewRegistry()
	dbPath := filepath.Join(t.TempDir(), "e2e.db")
	db, err := queue.Open(dbPath)
	if err != nil {
		t.Fatalf("error abriendo DB de test: %v", err)
	}
	defer db.Close()
	repo := queue.NewRepository(db)

	cfg := &config.Config{
		ServerURL:  wsURL,
		TerminalID: "caja-e2e",
		Token:      "test-token",
		LogLevel:   "debug",
		MaxRetries: 3,
	}

	conn := NewConnection(cfg, repo, registry)
	worker := queue.NewWorker(repo, registry, conn.Notify, false, "", cfg.MaxRetries)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go conn.Run(ctx)
	go worker.Run(ctx)

	// 4. Secuencia de aserciones:
	// Paso A: Esperar a que la config sea aplicada (registry con 1 impresora)
	waitFor(t, 5*time.Second, func() bool {
		return registry.Len() == 1
	})

	// Paso B (AC #1): Esperar a que el job esté guardado en SQLite en estado PENDING
	waitFor(t, 5*time.Second, func() bool {
		jobs, err := repo.ListByStatus(queue.EstadoPending)
		if err != nil || len(jobs) == 0 {
			return false
		}
		return jobs[0].ID == "e2e-job-1"
	})

	// Paso C: Señalizar el corte de conexión (simular caída)
	close(dropNow)

	// Paso D (AC #2): Worker procesa el job localmente mientras la conexión está caída -> PRINTED en SQLite
	waitFor(t, 10*time.Second, func() bool {
		jobs, err := repo.ListByStatus(queue.EstadoPrinted)
		if err != nil || len(jobs) == 0 {
			return false
		}
		for _, j := range jobs {
			if j.ID == "e2e-job-1" {
				return true
			}
		}
		return false
	})

	// Señalizar que el servidor puede aceptar la reconexión y recibir el SyncMsg
	close(allowReconnect)

	// Paso E (AC #3): Esperar reconexión y verificar que el SyncMsg incluye el job impreso
	select {
	case syncMsg := <-reconnectSyncCh:
		found := false
		for _, id := range syncMsg.PrintedJobs {
			if id == "e2e-job-1" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("SyncMsg.PrintedJobs no contiene 'e2e-job-1': %+v", syncMsg.PrintedJobs)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout esperando SyncMsg de reconexión tras caída")
	}

	// Paso F: Verificar que tras sincronizar con el servidor, el trabajo impreso fue eliminado de SQLite
	waitFor(t, 3*time.Second, func() bool {
		jobs, err := repo.ListByStatus(queue.EstadoPrinted)
		return err == nil && len(jobs) == 0
	})
}
