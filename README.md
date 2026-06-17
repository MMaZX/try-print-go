# USQAY Print Service

Agente de impresión térmica distribuida para restaurantes.

Arquitectura: `Go + WebSocket + SQLite local + ESC/POS`

---

## Principio fundamental

**Autoridad de Impresión Local** — el cliente es la única entidad que puede confirmar que un documento fue impreso. El servidor nunca asume que la impresión ocurrió.

---

## Estructura del repositorio

```
usqay-print-server/   → servidor WebSocket (intermediario entre Laravel y los agentes)
usqay-print-client/   → agente local instalado en cada PC del restaurante
```

---

## Arquitectura

```
┌─────────────────────────────────────┐
│         Backend Laravel             │
│                                     │
│  POST /api/tenant/print → servidor  │
│  POST /agents/validate  ← servidor  │
└──────────────┬──────────────────────┘
               │ HTTP interno (X-Internal-Token)
               ▼
┌──────────────────────────────────────┐
│      usqay-print-server              │
│      (WebSocket Server :8090)        │
│                                      │
│  • Valida tokens vía Laravel         │
│  • Registra terminales por token     │
│  • Despacha trabajos a los agentes   │
│  • Recibe confirmaciones             │
│  • Notifica estado de vuelta a       │
│    Laravel (PUT /queue/{id}/status)  │
└──────────────┬───────────────────────┘
               │ WS / WSS
               ▼
┌──────────────────────────────────────┐
│      usqay-print-client              │
│      (Agente local en la PC)         │
│                                      │
│  • SQLite local (agent.db)           │
│  • Registry de impresoras en memoria │
│  • Worker de impresión multi-printer │
│  • Reconexión automática con backoff │
└──────────────┬───────────────────────┘
               │ TCP (RED) / Spooler OS (USB/SERIE)
               ▼
         Impresora térmica
```

---

## Flujo completo

### 1. Conexión del agente

```
Cliente se conecta al servidor con solo el token
        ↓
Servidor llama a Laravel:
  POST /api/tenant/print-configuration/agents/validate
  { "token": "agt_xxx" }
        ↓
Laravel responde:
  { valid: true, terminal_id: "15", business_id: "uuid", printers: [...] }
        ↓
Servidor envía al cliente:
  { type: "config", terminal_id: "15", printers: [{id, tipo, addr}, ...] }
        ↓
Cliente puebla su registry de impresoras en memoria
```

### 2. Ciclo de impresión

```
Backend Laravel crea trabajo de impresión
        ↓
Servidor envía por WebSocket:
  { type: "print", job_id, tipo_documento, impresora_name_id, payload }
        ↓
Cliente recibe → guarda en SQLite { estado: PENDING, impresora_id }
        ↓
Cliente responde: { type: "received", job_id }   ← ACK inmediato
        ↓
Worker local: PENDING → PROCESSING → resuelve impresora → imprime → PRINTED
        ↓
Cliente notifica: { type: "printed", job_id }
        ↓
Servidor llama a Laravel:
  PUT /api/tenant/print-configuration/queue/{id}/status  { estado: "IMPRESO" }
```

Si se cae la conexión: los trabajos recibidos siguen en SQLite y se imprimen. Al reconectar, el cliente envía sync con los IDs ya impresos y vuelve a recibir su config de impresoras.

---

## Requisitos

- **Go 1.21+**
- **Windows:** no requiere ninguna dependencia extra (binario único, sin CGO)
- **Linux:** `lp` / CUPS instalado para impresoras USB/SERIE; impresoras RED no requieren nada

---

## Configuración

### Servidor — `usqay-print-server/config.json`

```json
{
  "port": 8090,
  "log_level": "info",
  "laravel_base_url": "http://localhost:8000",
  "internal_token": "token-interno-compartido"
}
```

| Campo | Descripción |
|---|---|
| `port` | Puerto HTTP/WebSocket del servidor |
| `log_level` | `debug` / `info` / `warn` / `error` |
| `laravel_base_url` | URL base del backend Laravel |
| `internal_token` | Secreto compartido entre servidor y Laravel (`X-Internal-Token`) |

> **Modo local (dev sin Laravel):** si `laravel_base_url` está vacío y se define `tokens` como mapa `terminal_id → token`, el servidor valida localmente. Si ambos están vacíos, acepta cualquier conexión (modo abierto para pruebas).

### Cliente — `usqay-print-client/config.json`

```json
{
  "server_url": "ws://localhost:8090/ws",
  "token": "agt_UPn0WkUtePvN2YMt35BkXCgGUfEQr08i0dVqONuaoIWpzqho",
  "log_level": "info"
}
```

| Campo | Descripción |
|---|---|
| `server_url` | URL WebSocket del servidor (`ws://` o `wss://`) |
| `token` | Token del agente generado en el panel de Laravel |
| `log_level` | `debug` / `info` / `warn` / `error` (opcional, default: `info`) |

> La configuración de impresoras **no va en config.json**. El servidor la entrega automáticamente tras validar el token.

### Impresoras — vienen del servidor

El servidor envía la lista de impresoras del sucursal en el mensaje `config` tras conectar:

```json
{
  "type": "config",
  "terminal_id": "15",
  "printers": [
    { "id": "uuid-impresora", "tipo": "RED",   "addr": "192.168.1.100:9100" },
    { "id": "uuid-impresora", "tipo": "USB",   "addr": "EPSON TM-T20 Receipt" },
    { "id": "uuid-impresora", "tipo": "SERIE", "addr": "EPSON TM-T20 Receipt" }
  ]
}
```

| `tipo` | Mecanismo | `addr` |
|---|---|---|
| `RED` | TCP socket directo → ESC/POS raw | `IP:9100` |
| `USB` | Windows Spooler / CUPS | nombre del driver en el OS |
| `SERIE` | Windows Spooler / CUPS | nombre del driver en el OS |

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
cd usqay-print-server/build
./usqay-print-server
# → level=INFO msg="usqay-print-server iniciado" addr=:8090
```

### 2. Iniciar el cliente (en la PC del restaurante)

```bash
cd usqay-print-client/build
./usqay-print-client
# → level=INFO msg="usqay-print-client iniciado"
# → level=INFO msg="conectando al servidor" url=ws://...
# → level=INFO msg="registrado con el servidor"
# → level=INFO msg="configuración aplicada" terminal_id=15 impresoras=2
# → level=INFO msg="worker iniciado"
```

### 3. Listar impresoras del OS disponibles

```bash
./usqay-print-client -list
# → Impresoras disponibles:
# →   - Epson TM-T20 Receipt
# →   - Microsoft Print to PDF
```

---

## Pruebas

### End-to-end con Laravel

```bash
# Enviar un trabajo desde Laravel al servidor Go, que lo despacha al cliente
curl -X POST http://localhost:8090/api/jobs \
  -H "Content-Type: application/json" \
  -H "X-Internal-Token: token-interno-compartido" \
  -d '{
    "terminal_id": "15",
    "impresora_name_id": "uuid-de-impresora",
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

### Prueba local de impresión (-insert)

```bash
cd usqay-print-client

# Insertar trabajo de prueba directo en SQLite
go run ./cmd/client -insert

# El worker lo detecta en <500ms e imprime (requiere que el registry
# tenga al menos una impresora, es decir, que el cliente esté conectado)

# Verificar en SQLite:
sqlite3 build/agent.db "SELECT id, tipo_documento, impresora_id, estado FROM print_jobs;"
```

### Prueba de caída de conexión

```bash
# 1. Iniciar servidor + cliente
# 2. Enviar un trabajo → cliente lo guarda en SQLite
# 3. Matar el servidor (Ctrl+C)
# 4. El worker sigue imprimiendo con los trabajos ya guardados
# 5. Reiniciar el servidor
# 6. El cliente reconecta automáticamente con backoff (1s → 2s → 4s → … → 60s)
# 7. El cliente envía sync con los IDs ya impresos
# 8. El servidor actualiza sus registros en Laravel
```

---

## Protocolo WebSocket

### Cliente → Servidor

```json
// Registro inicial — solo token, sin terminal_id
{ "type": "register", "token": "agt_xxx", "version": "1.0.3" }

// Sincronización de trabajos ya impresos (enviado junto al registro)
{ "type": "sync", "printed_jobs": ["id1", "id2"] }

// ACK de recepción (inmediato al recibir el trabajo)
{ "type": "received", "job_id": "abc123" }

// Confirmación de impresión exitosa
{ "type": "printed", "job_id": "abc123" }

// Reporte de error de impresión
{ "type": "error", "job_id": "abc123", "msg": "impresora no disponible" }

// Heartbeat (cada 30s)
{ "type": "ping" }
```

### Servidor → Cliente

```json
// Configuración inicial: identidad + impresoras disponibles
{
  "type": "config",
  "terminal_id": "15",
  "printers": [
    { "id": "uuid", "tipo": "RED", "addr": "192.168.1.100:9100" }
  ]
}

// Trabajo de impresión
{
  "type": "print",
  "job_id": "abc123",
  "tipo_documento": "comanda",
  "impresora_name_id": "uuid-impresora",
  "payload": { ... }
}

// Respuesta al heartbeat
{ "type": "pong" }

// Solicitud de reconexión (cuando cambia la config en el panel)
{ "type": "config_refresh" }

// Cuando una nueva instancia desplaza a esta conexión
{ "type": "kick", "reason": "nueva conexión registrada" }
```

---

## Estados de un trabajo (SQLite cliente)

```
PENDING     → recibido del servidor, guardado en SQLite
PROCESSING  → worker lo tomó
PRINTED     → impresión física confirmada por el OS/impresora
ERROR       → fallo (el worker continúa con el siguiente trabajo)
```

---

## Endpoints de Laravel requeridos por el servidor

El servidor Go llama a estos endpoints. Todos van protegidos con `X-Internal-Token`.

| Método | Ruta | Descripción |
|---|---|---|
| `POST` | `/api/tenant/print-configuration/agents/validate` | Valida token y devuelve `terminal_id` + `printers` |
| `PUT` | `/api/tenant/print-configuration/queue/{id}/status` | Actualiza estado de un trabajo |
| `POST` | `/api/tenant/print-configuration/monitor-token/validate` | Valida token del monitor web |

### `POST /agents/validate` — contrato

Request body:
```json
{ "token": "agt_xxx" }
```

Response esperada:
```json
{
  "valid": true,
  "terminal_id": "15",
  "business_id": "uuid-empresa",
  "printers": [
    { "id": "uuid-impresora", "tipo": "RED", "addr": "192.168.1.100:9100" }
  ]
}
```

---

## Logs

Cada binario escribe logs en `logs/` junto al ejecutable.

```
logs/
├── 2026-06-16.log          ← día actual
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

> **Importante:** nunca usar `mattn/go-sqlite3`. Requiere GCC en Windows y rompe la compilación de binario único.
