---
baseline_commit: f5c5c8aae4079dfe7d91186ddf6eb19b0e104ecb
---

# Story 5.3: Test end-to-end del escenario caída/reconexión

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

Como desarrollador de USQAY Print,
Quiero una prueba automatizada que reproduzca el flujo completo de caída y recuperación de conexión,
para demostrar que el sistema no pierde trabajos y sincroniza el estado real, tal como exige el POC.

## Acceptance Criteria

1. **Given** un servidor y un cliente conectados (o mockeados a nivel de WebSocket) **When** el servidor envía un job de impresión y luego se simula la caída de la conexión **Then** el cliente ya guardó el job en SQLite como `PENDING` antes del corte.
2. **Given** la conexión está caída **When** el worker local procesa el job **Then** el job pasa a `PRINTED` en SQLite local sin necesidad de conexión activa.
3. **Given** la conexión se restablece **When** el cliente reconecta y envía `type: "sync"` (protocolo existente, AD-2) **Then** el mensaje de sync refleja el job como impreso — verificado contra la lógica real de `Hub.HandleSync` del servidor (server-side), que actualiza `cola_impresion` al estado `"impreso"`.
4. **And** el test corre de forma aislada vía `go test ./...`, sin depender de hardware físico de impresión — usa un registry de impresoras fake/mock (impresora de red apuntando a un listener TCP local), consistente con la regla del proyecto de que toda prueba con impresión física se hace en Windows, nunca en Linux.

## Tasks / Subtasks

- [x] Task 1 — Test de integración cliente: ciclo completo caída/reconexión (AC: #1, #2, #4)
  - [x] Crear `usqay-print-client/internal/ws/e2e_dropreconnect_test.go`, `package ws` (mismo paquete que `connection_test.go`, no `ws_test`, para mantener el mismo patrón del repo).
  - [x] **Restricción de arquitectura importante — NO omitir:** `usqay-print-client` y `usqay-print-server` son **módulos Go separados** (dos `go.mod` distintos, sin `go.work` en el repo). Un test no puede importar `usqay-print-server/internal/...` desde `usqay-print-client` (ni viceversa) — las reglas de paquetes `internal` de Go lo bloquean incluso si se agregara un `go.work`, porque el ancestro del path de import no coincide. Por eso este test **mockea el servidor a nivel de mensajes WebSocket** dentro del propio módulo cliente, tal como el AC #1 permite explícitamente ("o mockeados a nivel de WebSocket"). La verificación contra la lógica *real* del servidor se hace por separado en la Task 2, en el módulo servidor.
  - [x] Levantar un **servidor WebSocket falso** con `httptest.NewServer` + `github.com/coder/websocket` (ya es dependencia del módulo cliente — ver `usqay-print-client/internal/ws/connection.go` imports). El handler debe, por cada conexión aceptada:
    1. `websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})`.
    2. Leer el primer mensaje (`RegisterMsg`, `type: "register"`) con `wsjson.Read`.
    3. Leer el segundo mensaje (`SyncMsg`, `type: "sync"`) con `wsjson.Read` — el cliente lo envía inmediatamente después del register como parte del handshake (ver `Connection.handshake` en `connection.go:143-162`). Guardar este `SyncMsg` en un canal/slice protegido por mutex, indexado por número de conexión (1ª o 2ª), para poder aserirlo después desde el test.
    4. Enviar `ConfigMsg{Type: TypeConfig, TerminalID: <terminal_id>, Printers: []PrinterSpec{...}}` con **un único** `PrinterSpec{ID: "impresora-e2e", Tipo: "RED", Addr: <addr del listener TCP fake>, Mode: "escpos"}` (ver Task de impresora fake abajo).
    5. **Solo en la primera conexión:** inmediatamente después de enviar `ConfigMsg`, enviar `PrintJobMsg{Type: TypePrint, JobID: "e2e-job-1", TipoDocumento: "comanda", ImpresoraNameID: "impresora-e2e", Payload: json.RawMessage(<payload válido>)}` (ver payload sugerido abajo).
    6. Después de enviar el job, entrar en un loop de lectura no bloqueante (goroutine) que descarta cualquier mensaje adicional del cliente (p.ej. el ACK `type: "received"`) hasta que el test señalice el corte mediante un `chan struct{}` (`dropNow`) — al recibir la señal, cerrar la conexión con `wsConn.Close(websocket.StatusNormalClosure, "corte simulado")` para simular la caída.
    7. **En la segunda conexión** (reconexión tras el corte): una vez leído el `SyncMsg` del paso 3, publicar su contenido en un canal `reconnectSyncCh chan SyncMsg` para que el test lo consuma con timeout.
  - [x] Levantar una **impresora fake** vía TCP puro (sin usar código de `usqay-print-client/internal/printer`, ya que ese paquete ya provee `NewNetworkPrinter` que dialéa TCP y escribe bytes — reutilizarlo tal cual, no reinventarlo): un `net.Listen("tcp", "127.0.0.1:0")` cuyo `Accept()` loop simplemente lee y descarta los bytes entrantes (`io.Copy(io.Discard, conn)`), simulando una impresora ESC/POS de red que siempre "imprime" con éxito. Usar la dirección resuelta (`listener.Addr().String()`) como `Addr` del `PrinterSpec` RED. Esto satisface AC #4 (sin hardware físico) sin necesitar mocks del paquete `printer` (no hay interfaz inyectable para eso hoy — no crearla solo para este test, el patrón real de `NewNetworkPrinter` contra un listener local ya es suficiente y más realista).
  - [x] Ensamblar los componentes reales del cliente exactamente como lo hace `cmd/client/main.go` (líneas 78-127), pero apuntando `cfg.ServerURL` a la URL del `httptest.Server` (convertir `http://127.0.0.1:PORT` a `ws://127.0.0.1:PORT`):
    ```go
    registry := printer.NewRegistry()
    dbPath := filepath.Join(t.TempDir(), "e2e.db")
    db, err := queue.Open(dbPath)
    repo := queue.NewRepository(db)
    cfg := &config.Config{ServerURL: wsURL, TerminalID: "caja-e2e", Token: "test-token", LogLevel: "debug", MaxRetries: 3}
    conn := NewConnection(cfg, repo, registry) // sin prefijo "ws." — el test vive dentro de package ws
    worker := queue.NewWorker(repo, registry, conn.Notify, false, "", cfg.MaxRetries)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    go conn.Run(ctx)
    go worker.Run(ctx)
    ```
  - [x] Payload de prueba sugerido (formato validado en `renderer_test.go:47-54`, funciona con `RenderImage(nil, payload)` que es lo que el worker invoca para impresoras `Mode() == "escpos"`, que es el caso de `NetworkPrinter`):
    ```json
    {"options":{"cut":false,"drawer":false},"margins":{"ancho_dimension":80.0},"body":[{"type":"text","value":"E2E DROP-RECONNECT"}]}
    ```
  - [x] Secuencia de aserciones del test (usar el helper `waitFor(t, timeout, cond func() bool)` ya existente como patrón en `worker_test.go:46-56` — replicarlo en este archivo, no importarlo entre paquetes de test):
    1. Poll `registry.Len() == 1` (config aplicada) antes de continuar — sin esto el worker no procesa nada (`Worker.processNext` retorna temprano si `registry.Len() == 0`, ver `worker.go:87-90`).
    2. **AC #1**: Poll `repo.ListByStatus(queue.EstadoPending)` hasta que contenga el job `"e2e-job-1"` — esto prueba que el cliente ya persistió el job en SQLite. Solo **después** de confirmar esto, cerrar `dropNow` (simular el corte). El orden es crítico: si se cierra antes de confirmar el PENDING, el test no prueba realmente el AC.
    3. **AC #2**: Poll `repo.ListByStatus(queue.EstadoPrinted)` hasta que contenga `"e2e-job-1"` — esto ocurre mientras la conexión sigue caída (el worker no depende de `conn`), demostrando que la impresión local no requiere conexión activa.
    4. **AC #3**: Esperar en `reconnectSyncCh` con timeout generoso (10s — el backoff de reconexión empieza en 1s, ver `connection.go:78,84-90`) y assertar que el `SyncMsg.PrintedJobs` recibido en la 2ª conexión contiene `"e2e-job-1"`. Esto prueba que el cliente reconectó y ejecutó el protocolo `sync` real (`buildSync()` en `connection.go:298-308`, que lee `ListByStatus(EstadoPrinted)`).
  - [x] Todo el test debe usar polling con deadline (patrón `waitFor`), **nunca** `time.Sleep` fijo como única sincronización — ya fue un finding de revisión en la story 5.2 (worker_test.go:94, ahora corregido) que un `time.Sleep` fijo contra un ticker real es flaky en CI.

- [x] Task 2 — Test de integración servidor: `Hub.HandleSync` refleja el estado real (AC: #3, verificación server-side real)
  - [x] Crear `usqay-print-server/internal/ws/hub_sync_test.go`, `package ws_test` (toda la superficie necesaria — `NewHub`, `EnqueueLaravel`, `HandleSync`, `JobSummary`, `GetAgentStatus` — ya está exportada, no requiere acceso a internos del paquete).
  - [x] `Hub.HandleSync(terminalID string, printedIDs []string, c *Client)` (ver `hub.go:211-239`) **no usa su parámetro `c`** dentro del cuerpo de la función — confirmado leyendo la implementación completa. Es seguro pasar `nil` como tercer argumento en el test; no hace falta construir un `*Client` real ni abrir un WebSocket.
  - [x] Test `TestHub_HandleSync_ReflectsPrintedState`:
    1. `hub := ws.NewHub(&config.Config{})` (import `usqay-print-server/internal/config`).
    2. `job, _ := hub.EnqueueLaravel("job-sync-1", "caja-e2e", "impresora-e2e", "RED", "COMANDA", json.RawMessage(`{}`), 300, false)` — como el terminal no está conectado (`h.clients` vacío), el job queda en `h.pending["caja-e2e"]` con `Estado: "pendiente"` (ver `hub.go:138-166`, rama `!connected`).
    3. Precondición: `hub.JobSummary()` debe mostrar el job con `Estado == "pendiente"`.
    4. Ejecutar `hub.HandleSync("caja-e2e", []string{job.ID}, nil)`.
    5. **Assert**: `hub.JobSummary()` muestra el job con `Estado == "impreso"` — esto es la verificación *real* (no mockeada) de que el servidor refleja `cola_impresion` como impreso al recibir el sync, cerrando el AC #3 desde el lado del servidor.
    6. Assert adicional: `hub.PendingJobsCount("caja-e2e") == 0` tras el sync (el job impreso ya no cuenta como pendiente, ver `countPendingJobsLocked` en `hub.go:357-365`).
  - [x] Test complementario `TestHub_HandleSync_IgnoresUnknownOrForeignJobID`: llamar `hub.HandleSync("caja-e2e", []string{"id-inexistente"}, nil)` y `hub.HandleSync("otra-terminal", []string{job.ID}, nil)` (con `job` de otra terminal) y assertar que **no** cambian el estado del job real — cubre la guarda `job.TerminalID == terminalID` en `hub.go:215`.
  - [x] `updateLaravelJobStatus` es un no-op silencioso cuando `cfg.LaravelBaseURL == ""` (ver `hub.go:459-462`) — no se necesita ningún servidor HTTP falso de Laravel para este test; usar `&config.Config{}` vacío es suficiente y no dispara llamadas de red.

## Dev Notes

- **Restricción arquitectónica central de esta historia**: `usqay-print-client` y `usqay-print-server` son módulos Go independientes (`go.mod` separados, sin workspace). Ningún test puede importar paquetes `internal` del otro módulo. Por eso la prueba end-to-end de esta historia se divide en dos tests de integración independientes (Task 1 en el módulo cliente con servidor mockeado a nivel de mensajes WS, Task 2 en el módulo servidor contra la lógica real de `Hub`) que en conjunto cubren el AC #5 original del epic sin necesitar un binario combinado ni `go.work`. Esto es consistente con la redacción del propio AC #1 ("o mockeados a nivel de WebSocket") — el epic ya anticipaba esta restricción.
- **No reinventar mocks de impresora**: no existe hoy una interfaz inyectable para mockear a nivel de `printer.Printer` desde el paquete `ws` del cliente sin acceso a internals — y no hace falta crear una. Reutilizar `NewNetworkPrinter` (ya existe, `internal/printer/network.go`) contra un `net.Listen` local es más simple, no requiere tocar código de producción, y es fiel a un escenario real de impresora de red.
- **Autoridad de Impresión Local**: el test de la Task 1 demuestra explícitamente esta regla del proyecto — el job pasa a `PRINTED` en SQLite local mientras la conexión WebSocket está caída, sin ninguna confirmación del servidor.
- **Trabajo en progreso sin commitear detectado en el working tree**: `git status` muestra `usqay-print-client/{cmd/client/main.go, internal/config/config.go, internal/queue/{repository.go,repository_test.go,sqlite.go,worker.go}}` modificados y `internal/config/config_test.go`, `internal/queue/worker_test.go` como nuevos, todos sin commitear, encima del commit `f5c5c8a` (baseline de esta historia). Esto corresponde a la implementación ya completada de la story 5.2 (retries/backoff) que aún no se ha commiteado — **no revertir ni descartar estos cambios**; son parte del estado actual esperado del árbol de trabajo, y esta historia se construye encima de ellos (usa `queue.NewWorker(..., maxRetries)` con la firma de 6 parámetros que ya incluyen retries).
- **No usar `time.Sleep` fijo como mecanismo de sincronización** — usar siempre polling con deadline (`waitFor`), replicando el patrón ya usado en `worker_test.go`. Fue un finding de revisión explícito en la story 5.2.
- **`Worker.processNext` no hace nada mientras `registry.Len() == 0`** (`worker.go:87-90`) — el test de la Task 1 debe esperar a que la config haya sido aplicada (registry poblado) antes de esperar cualquier procesamiento de job.
- **Backoff de reconexión**: arranca en 1s y se duplica hasta un tope de 60s (`connection.go:22-23,78-96`). Con un solo ciclo de caída/reconexión el timeout de espera del test para la 2ª conexión no debería necesitar más de ~5-10s.

### Project Structure Notes

- Archivos nuevos (no se modifica ningún archivo existente):
  - `usqay-print-client/internal/ws/e2e_dropreconnect_test.go` (`package ws`)
  - `usqay-print-server/internal/ws/hub_sync_test.go` (`package ws_test`)
- No se requiere ninguna migración de esquema, cambio de configuración, ni tocar código de producción — esta historia es exclusivamente de pruebas (FR-5, AD-2).
- Ambos módulos siguen siendo compilables y testeables de forma independiente vía `go test ./...` ejecutado dentro de cada uno (`usqay-print-client/` y `usqay-print-server/`), consistente con el comando estándar del proyecto.

### Testing Requirements

- Ejecutar `go test -v ./...` dentro de `usqay-print-client/` — debe incluir el nuevo `TestXxx` del escenario caída/reconexión pasando de forma determinista (sin flakiness por temporización).
- Ejecutar `go test -v ./...` dentro de `usqay-print-server/` — debe incluir `TestHub_HandleSync_ReflectsPrintedState` y el test complementario de guardas.
- Ningún test de esta historia requiere hardware de impresión física ni el entorno Windows — todo corre en Linux vía `go test`, consistente con la regla del proyecto (impresión física real solo se prueba en la VM Windows).
- `go vet ./...` y `go build ./...` deben pasar limpio en ambos módulos tras agregar los tests.

### References

- [Source: _bmad-output/planning-artifacts/epics-offline.md#Story 5.3: Test end-to-end del escenario caída/reconexión]
- [Source: usqay-print-client/internal/ws/connection.go] — `Run`, `connectAndServe`, `handshake`, `buildSync`, `dispatch`, `handlePrintJob`
- [Source: usqay-print-client/internal/ws/connection_test.go] — patrón `setupTestConnection`, estructura de tests existente en `package ws`
- [Source: usqay-print-client/internal/queue/worker.go] — `processNext`, dependencia de `registry.Len()`
- [Source: usqay-print-client/internal/queue/worker_test.go] — patrón `waitFor`, ensamblaje de `Worker` + `Repository` + `Registry` en tests
- [Source: usqay-print-client/internal/queue/renderer_test.go] — payload JSON válido para `RenderImage`/`render`
- [Source: usqay-print-client/internal/printer/network.go] — `NewNetworkPrinter`
- [Source: usqay-print-client/cmd/client/main.go] — orden real de ensamblaje `registry` → `db`/`repo` → `conn` → `worker` → `go conn.Run` / `go worker.Run`
- [Source: usqay-print-server/internal/ws/hub.go] — `EnqueueLaravel`, `HandleSync`, `JobSummary`, `PendingJobsCount`, `updateLaravelJobStatus` (no-op sin `LaravelBaseURL`)
- [Source: usqay-print-server/cmd/server/main_test.go] — patrón de test existente en el módulo servidor (`httptest`, `package main`)
- [Source: _bmad-output/implementation-artifacts/5-2-reintentos-controlados-en-fallos-de-impresion.md] — contexto de la historia previa, finding sobre `time.Sleep` flaky ya corregido

## Dev Agent Record

### Agent Model Used

Gemini 3.7 Flash

### Debug Log References

- Ejecución de tests cliente (`go test -v -run TestE2E_DropAndReconnect ./internal/ws`): exitoso en 1.03s.
- Ejecución de suite completa cliente (`go test -count=1 ./...`): todos los paquetes pasan sin errores ni flakiness.
- Ejecución de tests servidor (`go test -v ./internal/ws`): exitoso en 0.005s.
- Verificación estática y de compilación (`go vet ./... && go build ./...`): limpio en ambos módulos.

### Completion Notes List

- Implementado `TestE2E_DropAndReconnect` en `usqay-print-client/internal/ws/e2e_dropreconnect_test.go`:
  - Levanta un servidor WebSocket simulado con `httptest` + `github.com/coder/websocket`.
  - Levanta una impresora fake TCP con `net.Listen("tcp", "127.0.0.1:0")` que consume bytes sin fallar.
  - Verifica que el trabajo es persistido como `PENDING` en SQLite antes del corte de conexión (AC #1).
  - Corta la conexión WebSocket deliberadamente y verifica que el worker local imprime y actualiza el job a `PRINTED` en SQLite sin conexión activa (AC #2).
  - Espera la reconexión automática y verifica que el mensaje `type: "sync"` enviado por el cliente contiene el ID del trabajo impreso (AC #3 cliente).
  - Utiliza sincronización basada en polling determinista con deadline (`waitFor`) sin depender de hardware físico ni `time.Sleep` frágil (AC #4).
- Implementados tests de servidor en `usqay-print-server/internal/ws/hub_sync_test.go`:
  - `TestHub_HandleSync_ReflectsPrintedState`: valida que al invocar `Hub.HandleSync` con trabajos impresos por el agente, `JobSummary()` transiciona el estado a `"impreso"` y `PendingJobsCount` decrece a 0 (AC #3 servidor).
  - `TestHub_HandleSync_IgnoresUnknownOrForeignJobID`: valida las guardas de aislamiento por `TerminalID` y trabajos inexistentes en `HandleSync`.

### File List

- `usqay-print-client/internal/ws/e2e_dropreconnect_test.go` (nuevo)
- `usqay-print-server/internal/ws/hub_sync_test.go` (nuevo)
- `_bmad-output/implementation-artifacts/5-3-test-end-to-end-del-escenario-caida-reconexion.md` (modificado)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` (modificado)
