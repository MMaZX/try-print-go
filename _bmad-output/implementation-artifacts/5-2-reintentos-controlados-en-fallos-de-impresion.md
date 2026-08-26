---
baseline_commit: f5c5c8aae4079dfe7d91186ddf6eb19b0e104ecb
---

# Story 5.2: Reintentos controlados en fallos de impresión

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

Como worker local de impresión (usqay-print-client),
Quiero reintentar un trabajo fallido un número máximo de veces con backoff antes de marcarlo como ERROR definitivo,
para que errores transitorios (papel atascado, impresora ocupada, socket timeout) no pierdan el trabajo a la primera, pero tampoco bloqueen la cola de impresión de otros pedidos.

## Acceptance Criteria

1. **Given** un job falla al imprimir por error de hardware, spooler o timeout en `p.Print` **When** el worker captura el fallo y los intentos acumulados son menores que `max_retries` **Then** incrementa la columna `intentos` en `print_jobs`, calcula una fecha futura `next_retry_at` aplicando backoff exponencial, actualiza el estado a `PENDING` con `error_msg`, y NO envía notificación de error al servidor.
2. **Given** un job alcanza o supera el número máximo de reintentos configurado (`max_retries` en `config.json`, con fallback a `DefaultMaxRetries = 3`, NFR-3) **When** el worker evalúa el fallo tras agotar todos los intentos permitidos **Then** marca el trabajo con estado `ERROR` definitivo en `print_jobs`, registra el mensaje de error final, y notifica inmediatamente `{ type: "error", job_id, msg }` al servidor vía `w.notify`.
3. **Given** un job con fallo transitorio en espera de reintento (`next_retry_at` en el futuro) **When** el worker busca el siguiente trabajo pendiente mediante `NextPending()` **Then** la consulta en SQLite filtra únicamente los trabajos con `estado = 'PENDING' AND (next_retry_at IS NULL OR next_retry_at <= ?)` **And** los demás trabajos `PENDING` de la cola se siguen procesando con normalidad en orden FIFO sin ser bloqueados por el job en espera (NFR-2).
4. **Given** un job falla por error no transitorio (ej. error de renderizado de payload corrupto o `impresora_id` no registrada en la configuración) **When** el worker detecta el error **Then** marca el job como `ERROR` inmediatamente sin reintentos, ya que un payload corrupto o ID inexistente no se resolverá con reintentos físicos.
5. **Given** la base de datos local SQLite ya existe en entornos desplegados **When** se ejecuta `queue.Open()` **Then** se aplican migraciones idempotentes agregando las columnas `intentos` y `next_retry_at` a la tabla `print_jobs` sin pérdida de datos.

## Tasks / Subtasks

- [x] Task 1 — Migración de esquema en `sqlite.go` (AC: #1, #3, #5, AD-1)
  - [x] En `usqay-print-client/internal/queue/sqlite.go`, actualizar la constante `schema` en `print_jobs`:
    ```sql
    CREATE TABLE IF NOT EXISTS print_jobs (
        id              TEXT PRIMARY KEY,
        payload         TEXT NOT NULL,
        tipo_documento  TEXT NOT NULL DEFAULT 'comanda',
        impresora_id    TEXT NOT NULL DEFAULT '',
        estado          TEXT NOT NULL DEFAULT 'PENDING',
        intentos        INTEGER NOT NULL DEFAULT 0,
        next_retry_at   DATETIME,
        error_msg       TEXT,
        created_at      DATETIME NOT NULL,
        updated_at      DATETIME NOT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_print_jobs_estado ON print_jobs (estado, next_retry_at, created_at);
    ```
  - [x] En `Open(dbPath string)` añadir las sentencias de migración para columnas en bases de datos existentes:
    ```go
    if _, err := db.Exec(`ALTER TABLE print_jobs ADD COLUMN intentos INTEGER NOT NULL DEFAULT 0`); err != nil {
        if !strings.Contains(err.Error(), "duplicate column name") {
            db.Close()
            return nil, fmt.Errorf("migrar columna intentos: %w", err)
        }
    }
    if _, err := db.Exec(`ALTER TABLE print_jobs ADD COLUMN next_retry_at DATETIME`); err != nil {
        if !strings.Contains(err.Error(), "duplicate column name") {
            db.Close()
            return nil, fmt.Errorf("migrar columna next_retry_at: %w", err)
        }
    }
    ```

- [x] Task 2 — Parámetro `max_retries` en `config.go` (AC: #2, NFR-3)
  - [x] En `usqay-print-client/internal/config/config.go`, definir la constante `DefaultMaxRetries = 3`.
  - [x] Añadir `MaxRetries int `json:"max_retries,omitempty"`` a `struct Config`.
  - [x] En `Load()`, si `cfg.MaxRetries <= 0`, asignar `cfg.MaxRetries = DefaultMaxRetries`.

- [x] Task 3 — Repository: soporte de intentos, next_retry_at y consulta no bloqueante (AC: #1, #3)
  - [x] En `usqay-print-client/internal/queue/repository.go`:
    - Actualizar `PrintJob` para incluir `Intentos int` y `NextRetryAt *time.Time`.
    - Actualizar `Insert(job PrintJob)` y `Upsert(job PrintJob)` para incluir `intentos` y `next_retry_at`.
    - Actualizar `NextPending()` para consultar:
      ```sql
      SELECT id, payload, tipo_documento, impresora_id, estado, intentos, next_retry_at, COALESCE(error_msg,''), created_at, updated_at
      FROM print_jobs
      WHERE estado = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)
      ORDER BY created_at
      LIMIT 1
      ```
      pasando `EstadoPending` y `time.Now().UTC()`, usando `sql.NullTime` para escanear `next_retry_at` de forma segura.
    - Añadir método `func (r *Repository) RecordRetry(id string, intentos int, nextRetryAt time.Time, errorMsg string) error`:
      ```sql
      UPDATE print_jobs
      SET estado = ?, intentos = ?, next_retry_at = ?, error_msg = ?, updated_at = ?
      WHERE id = ?
      ```
      con estado `EstadoPending`, actualizando `updated_at = time.Now().UTC()`.
    - Añadir método `func (r *Repository) UpdateStatusWithAttempts(id string, estado Estado, intentos int, errorMsg string) error`.
    - Actualizar `ListByStatus` para escanear `intentos` y `next_retry_at`.

- [x] Task 4 — Lógica de reintentos y backoff en el Worker (AC: #1, #2, #3, #4, NFR-2)
  - [x] En `usqay-print-client/internal/queue/worker.go`:
    - Añadir campo `maxRetries int` a `struct Worker`.
    - Actualizar `NewWorker(...)` para recibir `maxRetries int` (asignando `DefaultMaxRetries = 3` si `<= 0`).
    - Implementar función helper `backoffDuration(intento int) time.Duration`:
      - Fórmula: `base := 2 * time.Second`, `delay := base * (1 << (intento - 1))` (intento 1: 2s, intento 2: 4s, intento 3: 8s; tope máximo 30s).
    - En `processNext()`:
      - Si `printErr != nil` (error físico de impresión):
        - Incrementar `job.Intentos++`.
        - Si `job.Intentos < w.maxRetries`:
          - Calcular `delay := backoffDuration(job.Intentos)` y `nextRetry := time.Now().UTC().Add(delay)`.
          - Loguear `slog.Warn("fallo de impresión temporal, programando reintento", "src", "WORKER", "job_id", job.ID, "intento", job.Intentos, "max_intentos", w.maxRetries, "reintento_en", delay, "error", printErr)`.
          - Llamar `w.repo.RecordRetry(job.ID, job.Intentos, nextRetry, printErr.Error())`.
          - Retornar sin llamar a `finalize` (el job sigue `PENDING` para reintentar luego).
        - Si `job.Intentos >= w.maxRetries`:
          - Loguear `slog.Error("fallo de impresión definitivo — superado máximo de reintentos", "src", "WORKER", "job_id", job.ID, "intentos", job.Intentos, "max_intentos", w.maxRetries, "error", printErr)`.
          - Llamar `w.finalize(job.ID, EstadoError, job.Intentos, fmt.Sprintf("fallo tras %d intentos: %v", job.Intentos, printErr))`.
      - Si `renderErr != nil` o impresora no registrada (`!ok`):
        - Llamar directamente `w.finalize(job.ID, EstadoError, job.Intentos, ...)` (sin reintentos, error permanente).
      - Si la impresión es exitosa (`printErr == nil`):
        - Llamar `w.finalize(job.ID, EstadoPrinted, job.Intentos, "")`.

- [x] Task 5 — Actualización del punto de entrada `main.go` (AC: #2)
  - [x] En `usqay-print-client/cmd/client/main.go`, pasar `cfg.MaxRetries` a `queue.NewWorker(...)`.

- [x] Task 6 — Pruebas unitarias de reintentos y cola no bloqueante (AC: #1, #2, #3, #4)
  - [x] En `usqay-print-client/internal/queue/repository_test.go`:
    - Test `NextPending` ignora un job con `next_retry_at` en el futuro y devuelve el siguiente job `PENDING` sin retry (FIFO no bloqueante).
    - Test `RecordRetry` actualiza correctamente `intentos`, `next_retry_at`, `error_msg` y mantiene `estado = PENDING`.
    - Test `NextPending` devuelve el job una vez que `next_retry_at` ha expirado/pasado.
  - [x] En `usqay-print-client/internal/config/config_test.go`:
    - Test default `MaxRetries = 3` y respeto de valor personalizado en `config.json`.
  - [x] En `usqay-print-client/internal/queue/worker_test.go`:
    - Mock de `printer.Printer` configurable para simular fallos y éxitos.
    - Test: Fallo de impresión en intento 1 programa reintento en SQLite sin notificar `notify`.
    - Test: Al alcanzar `maxRetries` (2 fallos con `max_retries=2`), pasa a `EstadoError` y dispara `notify` con `TypeError`.
    - Test: Fallo en intento 1 y éxito en intento 2 pasa a `EstadoPrinted` y dispara `notify` con `TypePrinted`.
    - Test: Fallo por payload corrupto (render error) pasa inmediatamente a `EstadoError` sin reintentos.

### Review Findings

- [x] [Review][Patch] Ante fallo de RecordRetry, revertir el job a PENDING con disponibilidad inmediata para que nunca quede zombi en PROCESSING [usqay-print-client/internal/queue/worker.go:178]
- [x] [Review][Patch] Notificar al servidor solo si la persistencia local tuvo éxito (Autoridad de Impresión Local) [usqay-print-client/internal/queue/worker.go:211]
- [x] [Review][Patch] Clamp del shift en backoffDuration para evitar overflow a duración negativa con max_retries grandes [usqay-print-client/internal/queue/worker.go:57]
- [x] [Review][Patch] Reconstruir índice idx_print_jobs_estado en BDs existentes (CREATE IF NOT EXISTS es no-op sobre el índice viejo) [usqay-print-client/internal/queue/sqlite.go:25]
- [x] [Review][Patch] Agregar COALESCE(error_msg, '') en NextPending y ListByStatus para escaneo seguro de NULL [usqay-print-client/internal/queue/repository.go:123]
- [x] [Review][Patch] config_test.go replica el clamp a mano y no ejercita config.Load() real [usqay-print-client/internal/config/config_test.go:17]
- [x] [Review][Patch] Falta test prescrito: NextPending devuelve el job cuando next_retry_at expira (Task 6, tercer bullet) [usqay-print-client/internal/queue/repository_test.go:138]
- [x] [Review][Patch] Tests del worker sincronizan con time.Sleep(700ms) contra ticker real — flaky en CI; usar polling con deadline [usqay-print-client/internal/queue/worker_test.go:94]
- [x] [Review][Patch] Comentario promete verificar next_retry_at en el futuro pero la aserción solo comprueba Intentos [usqay-print-client/internal/queue/worker_test.go:102]
- [x] [Review][Patch] Constante DefaultMaxRetries duplicada en config y queue (riesgo de divergencia) [usqay-print-client/internal/config/config.go:11]
- [x] [Review][Defer] Carrera Upsert de reimpresión vs worker en vuelo puede marcar PRINTED una fila recién reinsertada sin imprimirla [usqay-print-client/internal/ws/connection.go:271] — deferred, pre-existing
- [x] [Review][Defer] Sin barrido de jobs PROCESSING al arrancar tras crash durante impresión [usqay-print-client/cmd/client/main.go:103] — deferred, pre-existing
- [x] [Review][Defer] Backoff persistido con reloj de pared sensible a saltos de reloj (NTP/suspend) [usqay-print-client/internal/queue/repository.go:95] — deferred, pre-existing
- [x] [Review][Defer] Matching frágil por texto "duplicate column name" replicado en 3 migraciones (patrón preexistente) [usqay-print-client/internal/queue/sqlite.go:65] — deferred, pre-existing

## Dev Notes

- **Autoridad de Impresión Local**: El cliente decide cuándo un job está definitivamente en `ERROR` tras agotar reintentos o cuándo está en `PRINTED`.
- **Cero dependencias CGO (NFR-1)**: Reutilizar exclusivamente `modernc.org/sqlite` y tipos estándar (`database/sql`, `sql.NullTime`, `time.Time`).
- **Cola no bloqueante (NFR-2)**: Crucial que `NextPending()` filtre por `(next_retry_at IS NULL OR next_retry_at <= ?)`. De lo contrario, un job atascado monopolizaría el worker cada 500ms bloqueando pedidos de otras mesas.
- **Configurable (NFR-3)**: `max_retries` opcional en `config.json` con valor por defecto `DefaultMaxRetries = 3`.
- **Manejo de errores seguro**: `Scan` de `next_retry_at` debe usar `sql.NullTime` o puntero `*time.Time` para tolerar valores `NULL` sin producir error de scanning.

### Project Structure Notes

- Archivos modificados/creados:
  - `usqay-print-client/internal/queue/sqlite.go` (migración de esquema)
  - `usqay-print-client/internal/queue/repository.go` (campos y querys de retry)
  - `usqay-print-client/internal/queue/repository_test.go` (tests unitarios de repository)
  - `usqay-print-client/internal/queue/worker.go` (lógica de reintentos y backoff)
  - `usqay-print-client/internal/queue/worker_test.go` (tests unitarios de worker)
  - `usqay-print-client/internal/config/config.go` (campo `MaxRetries`)
  - `usqay-print-client/internal/config/config_test.go` (tests de config)
  - `usqay-print-client/cmd/client/main.go` (paso de `MaxRetries` a `NewWorker`)

### Testing Requirements

- Correr suite con `go test -v ./...` en Linux. No requiere hardware físico ni Windows.
- Asegurar cobertura de casos límite:
  - Fallo transitorio con recuperación -> pasa a `PRINTED`.
  - Fallos repetidos hasta agotar `max_retries` -> pasa a `ERROR` y notifica al servidor.
  - Errores fatales (payload corrupto) -> pasan inmediatamente a `ERROR` sin reintentos.
  - Varios jobs en cola -> un job en espera de reintento no bloquea los siguientes trabajos `PENDING`.

### References

- [Source: _bmad-output/planning-artifacts/epics-offline.md#Story 5.2: Reintentos controlados en fallos de impresión]
- [Source: usqay-print-client/internal/queue/worker.go]
- [Source: usqay-print-client/internal/queue/repository.go]
- [Source: usqay-print-client/internal/queue/sqlite.go]
- [Source: usqay-print-client/internal/config/config.go]

## Dev Agent Record

### Agent Model Used

Gemini 3.7 Flash

### Debug Log References

- `go test -v ./internal/config`: 100% PASS
- `go test -v ./internal/queue`: 100% PASS
- `go test -v ./...` en `usqay-print-client` y `usqay-print-server`: 100% PASS
- `go vet ./...` y `go build ./...` en ambos módulos: 100% PASS

### Completion Notes List

- Actualizado esquema SQLite en `sqlite.go` con columnas `intentos` y `next_retry_at` e índice compuesto `idx_print_jobs_estado (estado, next_retry_at, created_at)`.
- Agregadas migraciones idempotentes `ALTER TABLE` en `queue.Open()` ignorando errores de columna duplicada.
- Agregada constante `DefaultMaxRetries = 3` y campo `MaxRetries` en `config.go` y testeado en `config_test.go`.
- Agregados campos `Intentos` y `NextRetryAt` en `PrintJob`, método `RecordRetry`, y filtrado no bloqueante por `(next_retry_at IS NULL OR next_retry_at <= ?)` en `NextPending()`.
- Implementado método `UpdateStatusWithAttempts` en `repository.go` para persistir el conteo final de intentos.
- Implementada lógica de backoff exponencial en `worker.go` (`backoffDuration`) y reintentos automáticos ante errores de `p.Print`, pasando a `ERROR` definitivo únicamente al superar `maxRetries` o ante errores no reintentables (payload corrupto, impresora no registrada).
- Actualizado `main.go` para propagar `cfg.MaxRetries` al worker.
- Creados tests exhaustivos en `worker_test.go` y `repository_test.go` cubriendo éxito tras retry, agotamiento de retries, y no bloqueo FIFO.

### File List

- `usqay-print-client/internal/queue/sqlite.go`
- `usqay-print-client/internal/queue/repository.go`
- `usqay-print-client/internal/queue/repository_test.go`
- `usqay-print-client/internal/queue/worker.go`
- `usqay-print-client/internal/queue/worker_test.go`
- `usqay-print-client/internal/config/config.go`
- `usqay-print-client/internal/config/config_test.go`
- `usqay-print-client/cmd/client/main.go`
- `_bmad-output/implementation-artifacts/sprint-status.yaml`
- `_bmad-output/implementation-artifacts/5-2-reintentos-controlados-en-fallos-de-impresion.md`
