# USQAY Print Service

Agente de impresión térmica distribuida para restaurantes.

Arquitectura: `Go + WebSocket + SQLite local + ESC/POS`

---

## Principio fundamental

**Autoridad de Impresión Local** — el cliente es la única entidad que puede confirmar que un documento fue impreso. El servidor nunca asume que la impresión ocurrió.

---

## Estructura del repositorio

```
usqay-print-server/   → servidor WebSocket (backend del agente)
usqay-print-client/   → agente local instalado en cada PC del restaurante
```

---

## Arquitectura

```
┌─────────────────────────────────┐
│        Backend USQAY            │
│  (Laravel / cualquier backend)  │
│                                 │
│  POST /api/print → servidor Go  │
└──────────────┬──────────────────┘
               │ HTTP interno
               ▼
┌──────────────────────────────────┐
│      usqay-print-server          │
│      (WebSocket Server :8080)    │
│                                  │
│  • Registra terminales           │
│  • Despacha trabajos             │
│  • Recibe confirmaciones         │
└──────────────┬───────────────────┘
               │ WSS / WS
               ▼
┌──────────────────────────────────┐
│      usqay-print-client          │
│      (Agente local en la PC)     │
│                                  │
│  • SQLite local (agent.db)       │
│  • Worker de impresión           │
│  • Reconexión automática         │
└──────────────┬───────────────────┘
               │ TCP / Spooler OS
               ▼
         Impresora térmica
```

---

## Flujo de impresión

```
Backend crea orden de impresión
        ↓
Servidor envía por WebSocket:
{ type: "print", job_id, tipo_documento, payload }
        ↓
Cliente recibe → guarda en SQLite { estado: PENDING }
        ↓
Cliente responde: { type: "received", job_id }   ← ACK inmediato
        ↓
Worker local: PENDING → PROCESSING → imprime → PRINTED
        ↓
Cliente notifica: { type: "printed", job_id }
        ↓
Servidor actualiza registro
```

Si se cae la internet: los trabajos recibidos siguen en SQLite y se imprimen. Al reconectar, el cliente sincroniza los estados.

---

## Requisitos

- **Go 1.21+**
- **Windows:** no requiere ninguna dependencia extra (binario único, sin CGO)
- **Linux:** `lp` / CUPS instalado para impresión por nombre de sistema

---

## Configuración

### Servidor — `usqay-print-server/config.json`

```json
{
  "port": 8080,
  "log_level": "info",
  "tokens": {
    "caja-01": "token-secreto-caja",
    "cocina-01": "token-secreto-cocina"
  }
}
```

| Campo | Descripción |
|---|---|
| `port` | Puerto HTTP/WebSocket del servidor |
| `log_level` | `debug` / `info` / `warn` / `error` |
| `tokens` | Mapa `terminal_id → token` para autenticar agentes |

### Cliente — `usqay-print-client/config.json`

```json
{
  "server_url":   "ws://localhost:8080/ws",
  "terminal_id":  "caja-01",
  "token":        "token-secreto-caja",
  "log_level":    "info",
  "printer_type": "network",
  "printer_addr": "192.168.1.100:9100",
  "printer_name": "Epson TM-T20 Receipt"
}
```

| Campo | Descripción |
|---|---|
| `server_url` | URL WebSocket del servidor (`ws://` o `wss://`) |
| `terminal_id` | Identificador único del terminal en este restaurante |
| `token` | Token de autenticación (debe coincidir con el servidor) |
| `printer_type` | `network` (TCP directo) o `system` (spooler del OS) |
| `printer_addr` | Para `network`: `IP:9100` |
| `printer_name` | Para `system`: nombre exacto en Windows/CUPS |

---

## Build

```bash
# Servidor
cd usqay-print-server
go build -o build/usqay-print-server ./cmd/server

# Cliente — Linux
cd usqay-print-client
go build -o build/usqay-print-client ./cmd/client

# Cliente — Windows (cross-compile desde Linux)
GOOS=windows GOARCH=amd64 go build -o build/usqay-print-client.exe ./cmd/client
```

---

## Ejecución

### 1. Iniciar el servidor

```bash
cd usqay-print-server
go run ./cmd/server
# → time=... level=INFO msg="usqay-print-server iniciado" addr=:8080
```

### 2. Iniciar el cliente (en la PC del restaurante)

```bash
cd usqay-print-client
go run ./cmd/client
# → time=... level=INFO msg="conectando al servidor" url=ws://localhost:8080/ws
# → time=... level=INFO msg="registrado con el servidor" terminal_id=caja-01
# → time=... level=INFO msg="worker iniciado"
```

### 3. Listar impresoras disponibles

```bash
go run ./cmd/client -list
# → Impresoras disponibles:
# →   - Epson TM-T20 Receipt
# →   - Microsoft Print to PDF
```

---

## Pruebas

### Flujo de Oro (end-to-end completo)

Con el servidor y el cliente corriendo:

```bash
# Enviar un trabajo de impresión
curl -X POST http://localhost:8080/api/print \
  -H "Content-Type: application/json" \
  -d '{"terminal_id": "caja-01"}'

# Respuesta: {"estado":"enviado","job_id":"a1b2c3d4"}
```

El cliente imprime automáticamente y confirma al servidor.

```bash
# Verificar estado
curl http://localhost:8080/api/status
```

### Envío con payload personalizado

```bash
curl -X POST http://localhost:8080/api/print \
  -H "Content-Type: application/json" \
  -d '{
    "terminal_id": "caja-01",
    "tipo_documento": "comanda",
    "payload": {
      "mesa": 8,
      "items": [
        {"nombre": "Ceviche",       "cantidad": 2, "precio": 28.00},
        {"nombre": "Chicha Morada", "cantidad": 2, "precio": 5.00}
      ]
    }
  }'
```

### Prueba local sin servidor (Etapa 2)

```bash
cd usqay-print-client

# Insertar trabajo de prueba directo en SQLite
go run ./cmd/client -insert

# El worker lo detecta en <500ms e imprime
# Verificar en SQLite:
sqlite3 agent.db "SELECT id, tipo_documento, estado FROM print_jobs;"
```

### Prueba de caída de internet

```bash
# 1. Iniciar servidor + cliente
# 2. Enviar un trabajo → cliente lo guarda en SQLite
# 3. Matar el servidor (Ctrl+C)
# 4. El worker sigue imprimiendo con los trabajos ya guardados
# 5. Reiniciar el servidor
# 6. El cliente reconecta automáticamente con backoff (1s → 2s → 4s → ...)
# 7. El cliente envía sync con los IDs ya impresos
# 8. El servidor actualiza sus registros
```

---

## Tipos de conexión de impresora

| `printer_type` | Sistema | Mecanismo |
|---|---|---|
| `network` | Windows / Linux | TCP socket directo → `IP:9100` (ESC/POS raw) |
| `system` | Windows | Windows Print Spooler → nombre exacto del driver |
| `system` | Linux | CUPS → `lp -d <nombre>` |

---

## Protocolo WebSocket

### Cliente → Servidor

```json
// Registro inicial (primer mensaje en cada conexión)
{ "type": "register", "terminal_id": "caja-01", "token": "xxx" }

// Sincronización de trabajos ya impresos (enviado junto al registro)
{ "type": "sync", "printed_jobs": ["id1", "id2"] }

// ACK de recepción (inmediato al recibir el trabajo)
{ "type": "received", "job_id": "abc123" }

// Confirmación de impresión exitosa
{ "type": "printed", "job_id": "abc123" }

// Reporte de error
{ "type": "error", "job_id": "abc123", "msg": "impresora no disponible" }

// Heartbeat (cada 30s)
{ "type": "ping" }
```

### Servidor → Cliente

```json
// Configuración inicial
{ "type": "config", "terminal_id": "caja-01" }

// Trabajo de impresión
{ "type": "print", "job_id": "abc123", "tipo_documento": "comanda", "payload": {...} }

// Respuesta al heartbeat
{ "type": "pong" }

// Cuando una nueva instancia desplaza a esta conexión
{ "type": "kick", "reason": "nueva conexión registrada" }
```

---

## Estados de un trabajo (cliente)

```
PENDING     → recibido del servidor, guardado en SQLite
PROCESSING  → worker lo tomó
PRINTED     → impresión física confirmada
ERROR       → fallo (el worker sigue funcionando para los siguientes)
```

---

## Logs

Cada binario escribe logs en `logs/` junto al ejecutable.

```
logs/
├── 2026-06-09.log          ← día actual
└── archive/
    └── 2026/
        └── mayo/
            └── 2026-mayo.zip   ← logs del mes anterior (comprimidos al iniciar)
```

---

## Dependencias clave

| Paquete | Módulo | Motivo |
|---|---|---|
| `modernc.org/sqlite` | cliente | SQLite sin CGO — binario único en Windows |
| `github.com/alexbrainman/printer` | cliente | Windows Print Spooler via `winspool.drv` |
| `github.com/coder/websocket` | ambos | WebSocket ligero, context-aware |

> **Importante:** nunca usar `mattn/go-sqlite3`. Requiere GCC en Windows.
