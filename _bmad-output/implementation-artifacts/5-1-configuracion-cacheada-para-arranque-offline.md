---
baseline_commit: 9186d4d471eaf51d2ce6d4ebdcd8bf6db407f3f9
---

# Story 5.1: Configuración cacheada para arranque offline

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

Como agente de impresión (usqay-print-client),
Quiero persistir en SQLite local la última configuración de impresoras y rutas recibida del servidor, y cargarla al arrancar si no hay conexión,
para que el restaurante pueda seguir imprimiendo aunque el agente arranque sin internet.

## Acceptance Criteria

1. **Given** el cliente recibe un mensaje `type: "config"` válido del servidor **When** `applyConfig()` procesa el mensaje **Then** la lista de impresoras y su ruteo se persiste en SQLite (nueva tabla `agent_config`), reemplazando la versión anterior.
2. **Given** el cliente arranca sin poder establecer conexión WebSocket **When** se inicializa el worker **Then** el cliente carga la última configuración persistida en SQLite y la usa para resolver impresoras, **And** la persistencia usa `modernc.org/sqlite` (sin CGO), sin nuevas dependencias (NFR-1).
3. **Given** no existe ninguna configuración previa persistida (primer arranque, nunca llegó a conectar) **When** el cliente intenta arrancar sin conexión **Then** registra un error explícito en el log indicando que no hay configuración disponible y no puede operar, consistente con la regla del proyecto de no usar valores por defecto silenciosos.

## Tasks / Subtasks

- [x] Task 1 — Migración de esquema: tabla `agent_config` (AC: #1, #2)
  - [x] En `usqay-print-client/internal/queue/sqlite.go`, añadir a la constante `schema` una tabla de fila única:
    ```sql
    CREATE TABLE IF NOT EXISTS agent_config (
        id            INTEGER PRIMARY KEY CHECK (id = 1),
        terminal_id   TEXT NOT NULL,
        printers_json TEXT NOT NULL,
        updated_at    DATETIME NOT NULL
    );
    ```
  - [x] `CHECK (id = 1)` fuerza fila única; el upsert usa `INSERT OR REPLACE ... VALUES (1, ...)`, igual al patrón ya usado en `SaveProfile` (`repository.go:144-158`).
  - [x] No añadir `ALTER TABLE` para esto: es tabla nueva, se cubre con `CREATE TABLE IF NOT EXISTS` — el patrón `ALTER TABLE ... ADD COLUMN` (líneas 55-60 de `sqlite.go`) es solo para columnas añadidas a tablas *existentes* en DBs ya desplegadas; no aplica aquí.

- [x] Task 2 — Repository: guardar y cargar config cacheada (AC: #1, #2, #3)
  - [x] En `usqay-print-client/internal/queue/repository.go`, añadir sentinel: `var ErrNoConfig = errors.New("no hay configuración cacheada")`.
  - [x] Añadir `func (r *Repository) SaveConfig(terminalID string, printersJSON []byte) error` — `INSERT OR REPLACE INTO agent_config (id, terminal_id, printers_json, updated_at) VALUES (1, ?, ?, ?)`, wrapea error con `fmt.Errorf("guardar configuración cacheada: %w", err)`.
  - [x] Añadir `func (r *Repository) LoadConfig() (terminalID string, printersJSON []byte, err error)` — `SELECT terminal_id, printers_json FROM agent_config WHERE id = 1`; si `sql.ErrNoRows`, retornar `"", nil, ErrNoConfig` (usar `fmt.Errorf("%w", ErrNoConfig)` para mantener la cadena inspeccionable con `errors.Is`); otros errores wrapeados con `%w`.
  - [x] **Diseño deliberado:** `Repository` NO debe importar el paquete `ws` (evita ciclo: `ws` ya importa `queue`, ver `connection.go:18`). Por eso `printers_json` viaja como `[]byte` opaco — el `Repository` nunca deserializa `PrinterSpec`, solo persiste el JSON tal cual se lo entrega `ws.Connection`.

- [x] Task 3 — Refactor de `applyConfig` + nuevo hidratador offline (AC: #1, #2)
  - [x] En `usqay-print-client/internal/ws/connection.go`, extraer de `applyConfig` (líneas 314-360) la parte que puebla el registry en un método privado reutilizable: `func (c *Connection) hydrateRegistry(printers []PrinterSpec, terminalID string) (count int)` — hace `c.terminalID = terminalID`, `c.registry.Clear()`, el loop `buildPrinterFromSpec` + `registry.Set` + logs `"impresora registrada"`. NO debe llamar `SaveProfile` ni loguear banners de refresh — eso se queda en `applyConfig`.
  - [x] `applyConfig` pasa a: llamar `hydrateRegistry`, luego el loop de `SaveProfile` (sin duplicar lo que ya movió a `hydrateRegistry`), y al final — solo si `count > 0` — serializar `msg.Printers` con `json.Marshal` y llamar `c.repo.SaveConfig(msg.TerminalID, printersJSON)`, logueando error con `slog.Error` si falla (no debe abortar el flujo de config).
  - [x] Añadir método público `func (c *Connection) LoadCachedConfig() error`: llama `c.repo.LoadConfig()`; si `errors.Is(err, queue.ErrNoConfig)` retorna ese error tal cual (el llamador decide el mensaje de log, AC #3); si otro error, wrapea con `fmt.Errorf("cargar configuración cacheada: %w", err)`; si éxito, `json.Unmarshal(printersJSON, &printers)` y llama `count := c.hydrateRegistry(printers, terminalID)`, logueando `slog.Info("configuración offline cargada desde caché local", "terminal_id", terminalID, "impresoras", count)`.

- [x] Task 4 — Integración en arranque (AC: #2, #3)
  - [x] En `usqay-print-client/cmd/client/main.go`, entre la creación de `conn := ws.NewConnection(...)` (línea 106) y `go conn.Run(ctx)` (línea 114), insertar la hidratación offline:
    ```go
    if err := conn.LoadCachedConfig(); err != nil {
        if errors.Is(err, queue.ErrNoConfig) {
            slog.Error("no hay configuración cacheada disponible — el agente no podrá resolver impresoras hasta reconectar con el servidor")
        } else {
            slog.Error("error cargando configuración cacheada desde SQLite", "error", err)
        }
    }
    ```
  - [x] Añadir `"errors"` al bloque de imports de `main.go` (no está importado actualmente).
  - [x] No condicionar esta llamada a "si hay o no conexión": se ejecuta siempre, incondicionalmente, antes de que `conn.Run` intente conectar. Si el WebSocket conecta exitosamente segundos después, el `applyConfig` real del servidor sobrescribe el registry de forma normal (mismo mecanismo de hoy) — no hay carrera dañina porque `hydrateRegistry` siempre hace `Clear()` antes de repoblar.

## Dev Notes

- **Autoridad de Impresión Local** (CLAUDE.md): esta story no cambia el flujo de impresión ni quién decide `PRINTED`; solo resuelve routing de impresoras cuando no hay config en vivo del servidor.
- **Regla NFR-1**: reutilizar `database/sql` + `encoding/json` (stdlib) + `modernc.org/sqlite` ya presente. No agregar dependencias nuevas al `go.mod`.
- **Regla del proyecto — sin defaults silenciosos**: el mensaje de log del AC #3 debe ser explícito y a nivel `Error`, no `Warn` ni `Debug` — es la señal operativa de que el agente arrancó ciego.
- **Manejo de errores** (skill `golang-error-handling` cargada para esta story): usar `errors.New`/sentinel para `ErrNoConfig`, wrapping con `%w` en todo el resto, `errors.Is` para detectar el caso "sin config" en `main.go`. Regla de manejo único: cada error se loguea O se retorna, nunca ambos — por eso `LoadCachedConfig` retorna el error sin loguearlo, y es `main.go` quien decide el mensaje final.
- **Nunca panic**: ningún fallo de esta ruta (JSON corrupto en `printers_json`, fila ausente, error de SQLite) debe detener el arranque del binario; todo se loguea y el agente sigue esperando reconectar — coherente con el comportamiento ya establecido en `worker.go` (`w.registry.Len() == 0` → espera, no crashea).
- **No reinventar**: el patrón de upsert de fila única (`INSERT OR REPLACE ... id = 1`) ya existe en el proyecto para `printer_profiles` vía `SaveProfile` (`repository.go:144-158`) — seguir la misma forma, no una tabla con múltiples filas históricas.
- **Qué NO tocar**: `printer_profiles` y `SaveProfile`/`GetProfile` (`repository.go:143-187`) ya cachean el `DeviceProfile` por impresora — esta story no duplica esa persistencia. `agent_config` solo guarda `terminal_id` + el array `printers_json` (id, tipo, addr, mode — sin el campo `profile` es indistinto, se puede serializar completo, el `Registry` no lo usa; los profiles se resuelven aparte vía `GetProfile` en `worker.go:96-104`, que ya funciona offline porque lee de SQLite).
- **Preservar comportamiento existente**: `applyConfig` sigue disparándose igual en cada `TypeConfig` (conexión inicial y `config_refresh`); los logs de banner `▶ CONFIGURACIÓN ACTUALIZADA...` y `▶ REFRESH COMPLETADO ◀` (líneas 320-323, 354-356) no deben moverse a `hydrateRegistry` ni duplicarse.

### Project Structure Notes

- Todos los cambios caen en archivos ya existentes: `internal/queue/sqlite.go`, `internal/queue/repository.go`, `internal/ws/connection.go`, `cmd/client/main.go`. No se crean paquetes nuevos.
- Aislamiento por plataforma (regla CLAUDE.md #2): esta story no toca `printer_linux.go` ni `printer_windows.go` — no aplica aquí.
- Sin conflictos de estructura detectados contra el layout actual del repo.

### Testing Requirements

- Cargar skill `golang-pro` (tests table-driven, subtests) y `golang-error-handling` antes de escribir los tests — ya se cargaron durante la creación de esta story y siguen aplicando en `dev-story`.
- **Nuevo `usqay-print-client/internal/queue/repository_test.go`** (no existe archivo de test de repository hoy): usar `queue.Open(filepath.Join(t.TempDir(), "test.db"))` para una DB real de archivo (no hay precedente de `:memory:` en el repo; seguir el patrón de DB en disco temporal ya usado implícitamente por `Open`). Casos tabla-driven mínimos:
  - `LoadConfig` sobre DB recién creada (sin `SaveConfig` previo) → retorna `queue.ErrNoConfig` verificable con `errors.Is`.
  - `SaveConfig` seguido de `LoadConfig` → round-trip exacto de `terminal_id` y `printers_json`.
  - Segundo `SaveConfig` con datos distintos → `LoadConfig` refleja solo la versión más reciente (una sola fila, `id = 1`).
- **`usqay-print-client/internal/ws` (nuevo o existente `connection_test.go` si no hay uno)**: test de `LoadCachedConfig` — pre-poblar `agent_config` vía `Repository.SaveConfig` con un JSON de `[]PrinterSpec` serializado a mano, invocar `LoadCachedConfig()` sobre una `Connection` construida con `NewConnection`, y verificar que `registry.Len()` y `registry.Resolve(id)` reflejan lo esperado. Cubrir también el caso `ErrNoConfig` (debe propagarse sin panic).
- Ejecutar `go test ./...` (sin hardware, sin Windows) — cumple la regla del proyecto de que solo la impresión física migra a la VM Windows; esto es persistencia y lógica pura, se prueba en Linux normalmente.
- No hace falta test end-to-end de caída/reconexión completo aquí — eso es el alcance explícito de la Story 5.3 (AD-2, reutiliza `sync`/`HandleSync`); esta story solo cubre persistencia + hidratación de config.

### References

- [Source: _bmad-output/planning-artifacts/epics-offline.md#Story 5.1: Configuración cacheada para arranque offline] — historia origen, ACs, FR-1/FR-2/NFR-1/AD-1.
- [Source: _bmad-output/project-context.md#Critical Implementation Rules] — reglas 1, 3, 5 (autoridad local, worker sin panic, sin defaults silenciosos).
- [Source: usqay-print-client/internal/queue/sqlite.go] — schema actual y patrón de migración `ALTER TABLE` (no aplica a esta story, solo referencia de estilo).
- [Source: usqay-print-client/internal/queue/repository.go#SaveProfile,GetProfile] — patrón de upsert de fila única a replicar para `agent_config`.
- [Source: usqay-print-client/internal/ws/connection.go#applyConfig,dispatch] — punto de integración para persistir config y refactor a `hydrateRegistry`.
- [Source: usqay-print-client/internal/ws/messages.go#ConfigMsg,PrinterSpec] — forma del JSON a persistir.
- [Source: usqay-print-client/cmd/client/main.go] — orden de arranque donde se inserta la hidratación offline.
- [Source: usqay-print-client/internal/queue/worker.go#processNext] — por qué un registry vacío no rompe el worker (ya maneja el caso, solo espera).

## Dev Agent Record

### Agent Model Used

Gemini 3.7 Flash

### Debug Log References

- Tests unitarios de cola y repositorio: `go test -v ./internal/queue` (100% PASS)
- Tests unitarios de WebSocket y caché offline: `go test -v ./internal/ws` (100% PASS)
- Suite completa del proyecto: `go test -v ./...`, `go vet ./...`, `go build ./...` en `usqay-print-client` y `usqay-print-server` (100% PASS)

### Completion Notes List

- Implementada tabla de fila única `agent_config` en el esquema SQLite de `sqlite.go` con restricción `CHECK (id = 1)`.
- Creado sentinel `ErrNoConfig` y métodos `SaveConfig` y `LoadConfig` en `repository.go`.
- Implementados tests unitarios exhaustivos para `Repository` en `internal/queue/repository_test.go`.
- Refactorizado `applyConfig` en `connection.go`, extrayendo `hydrateRegistry` y agregando persistencia automática a `agent_config` al recibir configuración del servidor.
- Implementado método `LoadCachedConfig` en `Connection` para hidratar el registro de impresoras de forma offline desde SQLite.
- Implementados tests unitarios de hidratación y persistencia de configuración en `internal/ws/connection_test.go`.
- Integrada la carga incondicional de configuración cacheada en el arranque de `cmd/client/main.go` antes de iniciar las goroutines del worker y WebSocket, con logueo explícito a nivel `Error` ante ausencia de configuración o fallos de lectura.

### File List

- `usqay-print-client/internal/queue/sqlite.go`
- `usqay-print-client/internal/queue/repository.go`
- `usqay-print-client/internal/queue/repository_test.go`
- `usqay-print-client/internal/ws/connection.go`
- `usqay-print-client/internal/ws/connection_test.go`
- `usqay-print-client/cmd/client/main.go`
- `_bmad-output/implementation-artifacts/sprint-status.yaml`
- `_bmad-output/implementation-artifacts/5-1-configuracion-cacheada-para-arranque-offline.md`

### Review Findings

**Resumen:** El feature de configuración cacheada (AC1-AC3 + restricciones de diseño) está correctamente implementado y testeado. El diff, sin embargo, arrastra un feature grande e independiente (renderer de imágenes/QR/raster) que viola reglas duras de la historia (NFR-1 "sin nuevas dependencias", "no se crean paquetes nuevos", "no toca printer_*, worker"). Se recomienda dividir el PR.

**Decision-needed (requieren decisión de Fulanito):**
- [x] [Review][Decision] Nueva dependencia `github.com/skip2/go-qrcode` viola NFR-1 / AC2 — RESUELTO: el renderer que la introduce se movió a su propio commit (Fase 0-3) fuera de 5.1.
- [x] [Review][Decision] Archivo fuera de alcance `image_renderer.go` — RESUELTO: movido a commit de Fase 0-3.
- [x] [Review][Decision] `printer_linux.go` / `printer_windows.go` modificados — RESUELTO: movidos a commit de Fase 0-3.
- [x] [Review][Decision] `worker.go` acoplado al renderer — RESUELTO: movido a commit de Fase 0-3.
- [x] [Review][Decision] Bundling fuera de alcance (`cmd/test_*`, `docs/`, `windows-test.example.yaml`, `CLAUDE.md`, `.gitignore`) — RESUELTO: movido a commit de Fase 0-3.
- [x] [Review][Decision] `LoadCachedConfig` no valida `terminal_id` del caché contra `config.json` — RESUELTO (D6): commit `73e242b` agrega `Config.TerminalID`+`Validate()`, nuevo sentinel `ErrCachedConfigForeignTerminal`, comparación en `LoadCachedConfig` y log específico en `main.go`.

**Resolución del split (commits en `feature/render-printer`):**
- `renderer "Fase 0-3"`: image_renderer.go, printer_linux/windows.go (DirectDevicePrinter), worker.go (RenderImage+timing), go.mod/go.sum (go-qrcode, x/image), cmd/test_esc_star, cmd/test_image_print, docs, windows-test.example.yaml, CLAUDE.md, .gitignore, y bits de `connection.go` (import `strings` + routing `/dev/`).
- `5.1 config-caching`: repository.go (SaveConfig/LoadConfig/ErrNoConfig), sqlite.go (agent_config), connection.go (LoadCachedConfig+hydrateRegistry+applyConfig persiste), main.go (hidratación offline), repository_test.go, connection_test.go.
- D6 (validación terminal_id): config.go, repository.go (sentinel), connection.go, main.go, connection_test.go.

**Patch (fix no ambiguo):**
- [x] [Review][Patch] Validar terminal_id en `applyConfig` antes de sobreescribir SQLite [`usqay-print-client/internal/ws/connection.go:379`]
- [x] [Review][Patch] Evitar llamada redundante a `buildPrinterFromSpec` en `applyConfig` [`usqay-print-client/internal/ws/connection.go:387`]
- [x] [Review][Patch] Retornar sentinel `ErrNoConfig` directamente sin doble wrapping en `LoadConfig` [`usqay-print-client/internal/queue/repository.go:225`]
- [x] [Review][Patch] `connection.go:332-334` (P7) — caché con 0 impresoras ahora loguea Warning en vez de éxito silencioso. Aplicado en `f5c5c8a`.
- [x] [Review][Patch] `connection.go:381-390` (P8) — `applyConfig` no persiste perfiles de impresoras de tipo desconocido. Aplicado en `f5c5c8a`.
- [ ] [Review][Patch] `drawBlock` case "barcode" nunca dibuja el contenido (se mide pero no se renderiza) — `image_renderer.go:352` / `default:632`. **Fuera de alcance 5.1** → action item de la story del renderer (Fase 0-3).
- [ ] [Review][Patch] `usableWidth = widthDots - 2*paddingDots` no acotado; padding extremo → ancho negativo en `WordWrap` — `image_renderer.go:118`. **Action item renderer**.
- [ ] [Review][Patch] Nil font → panic en `drawBlock` (`dc.SetFontFace(face)` sin guarda) — `image_renderer.go:392`. **Action item renderer**.
- [ ] [Review][Patch] División por cero en imagen con `bounds.Dx()==0` — `image_renderer.go:375,612`. **Action item renderer**.
- [ ] [Review][Patch] Bloque de payload corrupto eliminado en silencio — `image_renderer.go:134`. **Action item renderer**.
- [ ] [Review][Patch] QR/imagen con error de encode/decode omitidos en silencio — `image_renderer.go:581,603`. **Action item renderer**.
- [ ] [Review][Patch] Escritura corta en dispositivo tratada como éxito (`f.Write` ignora `n`) — `printer_linux.go:46`. **Action item renderer**.
- [ ] [Review][Patch] Overflow de ancho raster 16-bit para rollos >2040 dots — `image_renderer.go:171,215`. **Action item renderer**.
- [ ] [Review][Patch] `go.mod` marca `go-qrcode`/`golang.org/x/image` como `// indirect` estando importados directamente (hygiene) — `go.mod`. **Action item renderer**.

**Defer (pre-existing, no introducido por este cambio):**
- [x] [Review][Defer] Race concurrente en `registry` (goroutine WS vs worker) — preexistente, `connection.go`/`worker.go`.
- [x] [Review][Defer] `SQLITE_BUSY` no reintentado en `SaveConfig`/`LoadConfig` (patrón previo en `SaveProfile`) — preexistente.
- [x] [Review][Defer] Cuerpo vacío de `RenderImage` emite corte/feed en blanco — estilo preexistente.

