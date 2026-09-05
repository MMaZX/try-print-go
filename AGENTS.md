# USQAY Print Service — Arquitectura y Reglas

## Contexto del Proyecto

Agente de impresión térmica distribuida para restaurantes.

Principio fundamental: **Autoridad de Impresión Local** — el cliente es la única entidad que puede confirmar que un documento fue impreso. El servidor nunca asume que la impresión ocurrió.

## Tech Stack

- **Servidor:** Go, WebSockets nativos
- **Cliente:** Go, SQLite local (`agent.db`), comandos ESC/POS

## Dependencias obligatorias

| Paquete | Uso | Motivo |
|---|---|---|
| `modernc.org/sqlite` | SQLite en el cliente | **No usa CGO.** Compila a binario único en Windows sin necesitar GCC/MinGW. Nunca usar `mattn/go-sqlite3`. |
| `github.com/alexbrainman/printer` | Spooler Windows | API nativa `winspool.drv`. Solo en `//go:build windows`. |

## Estructura del Repositorio

```text
usqay-print-server/   → servidor WebSocket
usqay-print-client/   → agente local (SQLite + worker + impresión)
```

## Comandos

```bash
# Build
go build -o build/usqay-print-server.exe ./cmd/server
go build -o build/usqay-print-client.exe ./cmd/client

# Test
go test ./...
```

## Reglas de Código Obligatorias

### 1. Flujo de impresión

El flujo tiene un orden fijo. No alterarlo:

```text
Servidor envía JSON
    ↓
Cliente inserta en SQLite { estado: PENDING }
    ↓
Cliente responde ACK inmediato: { type: "received", job_id }
    ↓
Worker local procesa e imprime físicamente
    ↓
Cliente actualiza a PRINTED y notifica al servidor (o retiene offline hasta reconectar y enviar sync)
    ↓
Cliente elimina el trabajo de SQLite tras notificar exitosamente (la persistencia histórica la maneja Laravel)
```

### 2. Compilación condicional para impresión por sistema operativo

El manejo del spooler debe aislarse obligatoriamente en archivos separados:

```text
internal/printer/printer_windows.go  →  //go:build windows  (usa alexbrainman/printer)
internal/printer/printer_linux.go    →  //go:build linux    (usa lp -d o libcups)
```

Nunca mezclar lógica de Windows y Linux en el mismo archivo.

### 3. WebSocket

No usar librerías externas pesadas de WebSocket. Preferir paquetes ligeros o la implementación nativa de Go.

### 4. Conexiones duplicadas

Si un terminal_id ya tiene una conexión activa, el servidor debe hacer **kick** a la conexión anterior y registrar la nueva. Nunca rechazar la nueva conexión.

### 5. Logs

Los logs se rotan diariamente (`logs/YYYY-MM-DD.log`). Al inicio de cada mes se comprimen los logs del mes anterior en `logs/archive/YYYY/mes/YYYY-mes.zip` y se eliminan los `.log` originales. El propio binario ejecuta este mantenimiento al arrancar.

### 6. Configuración

No usar `.env`. Toda la configuración se lee desde `config.json` ubicado junto al ejecutable.

```json
{
  "server_url": "wss://app.usqay.com/ws",
  "terminal_id": "caja-01",
  "token": "xxxxx",
  "log_level": "info"
}
```

El cliente carga `config.json` al arrancar. **Si no existe**, no termina con error: lanza un wizard interactivo por consola que pregunta los campos obligatorios (`server_url`, `terminal_id`, `token`), reintenta si alguno llega vacío, y arma `config.json` en el mismo directorio del ejecutable. Si `config.json` ya existe, el wizard nunca se ejecuta — se carga directo. `log_level` no se pregunta: siempre queda en `"info"` por default en la primera corrida. No usar valores por defecto silenciosos para campos críticos como `token` o `server_url` cuando sí vienen en el JSON (es decir, un `token` vacío en un `config.json` existente sigue siendo un error de `Validate()`, no se rellena solo).

Implementación: `usqay-print-client/internal/config/config.go` (`runFirstTimeSetup`, `promptRequired`).

### 7. Pruebas de impresión física: SIEMPRE en Windows

Se comprobó (2026-08-18) que escribir ESC/POS crudo a `/dev/usb/lp0` en Linux corrompe la impresión en
impresoras térmicas clon (artefactos, corte que no llega, impresión incompleta), incluso con pacing
agresivo. El driver Windows del fabricante (spooler `winspool.drv`) no tiene ese problema.

**Regla:** toda prueba que implique imprimir físicamente (layout, raster, corte, cajón, velocidad real) se
hace en la VM Windows por SSH, nunca en Linux. Compilar, `go vet`, `go test ./...` sí se hacen en Linux
normalmente — solo la impresión física migra a Windows.

Flujo y credenciales: ver [`docs/entorno-pruebas-windows.md`](docs/entorno-pruebas-windows.md). Las
credenciales de conexión viven en `usqay-print-client/windows-test.yaml` (gitignored, nunca en
documentación ni commits) — copiar desde `usqay-print-client/windows-test.example.yaml`.

---

## Regla de Colaboración

### Preguntar antes de actuar

Ante cualquier instrucción ambigua o que implique cambios en múltiples archivos, **siempre preguntar** antes de escribir código. No asumir la intención del usuario.

Ejemplos obligatorios de cuestionamiento previo:
- Si una indicación implica un nuevo componente o puerto ("el cliente necesita puerto X") → preguntar para qué lo usará.
- Si se pide eliminar algo ("eliminar tokens") → confirmar qué exactamente se elimina y si hay side effects.
- Si el scope no está claro → acotar antes de implementar.

---

## Skills instaladas

Estas skills están instaladas globalmente y deben cargarse antes de escribir código según el contexto:

### `golang-pro`

**Cargar cuando:** se escribe cualquier código Go en este proyecto — goroutines, channels, interfaces, generics, tests con tabla, benchmarks, CLI tools, o cualquier estructura de paquete.

**No escribir código Go sin haberla cargado.**

### `golang-error-handling`

**Cargar cuando:** se implementa manejo de errores en Go — creación, wrapping con `%w`, `errors.Is` / `errors.As`, errores sentinel, tipos de error custom, `panic/recover`, logging estructurado con `slog`.

Crítico para el worker del cliente: ningún error de impresión debe provocar un panic ni perderse silenciosamente.

### `websocket-engineer`

**Cargar cuando:** se trabaja en la capa WebSocket — hub del servidor, gestión de conexiones, reconexión con backoff, ping/pong, detección de zombies, mensajes bidireccionales.

Aplica tanto a `usqay-print-server` como a la capa de conexión de `usqay-print-client`.
