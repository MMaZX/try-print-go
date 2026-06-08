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
Cliente actualiza a PRINTED y notifica al servidor
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

El cliente carga `config.json` al arrancar. Si no existe, termina con un error claro indicando que debe crearse. No usar valores por defecto silenciosos para campos críticos como `token` o `server_url`.

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
