# USQAY Print Service

Agente de impresión térmica distribuida para restaurantes.

Stack: `Go + WebSocket + SQLite local + ESC/POS`

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
│  POST /api/v1/jobs      → servidor  │
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
│  • Worker de impresión               │
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
Cliente conecta al servidor con su token
        ↓
Servidor valida el token contra Laravel:
  POST /api/tenant/print-configuration/agents/validate
  { "token": "agt_xxx" }
        ↓
Laravel responde:
  { valid: true, terminal_id: "15", business_id: "uuid", printers: [...] }
        ↓
Servidor envía al cliente su configuración:
  { type: "config", terminal_id: "15", printers: [{id, tipo, addr}, ...] }
        ↓
Cliente carga las impresoras en su registry en memoria
```

### 2. Ciclo de impresión

```
Backend Laravel crea trabajo de impresión
        ↓
Servidor envía por WebSocket:
  { type: "print", job_id, tipo_documento, impresora_name_id, payload }
        ↓
[CLIENTE] recibe → guarda en SQLite { estado: PENDING }
        ↓
[CLIENTE] responde: { type: "received", job_id }   ← ACK inmediato
        ↓
[WORKER] cada 500ms: PENDING → PROCESSING → imprime → PRINTED
        ↓
[CLIENTE] notifica: { type: "printed", job_id }
        ↓
Servidor llama a Laravel:
  PUT /api/tenant/print-configuration/queue/{id}/status  { estado: "IMPRESO" }
```

Si la conexión cae: los trabajos guardados en SQLite siguen procesándose. Al reconectar, el cliente envía un `sync` con los IDs ya impresos para que el servidor actualice Laravel.

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

### Cliente — `usqay-print-client/config.json`

```json
{
  "server_url": "ws://localhost:8090/ws",
  "token": "agt_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "log_level": "info"
}
```

| Campo | Descripción |
|---|---|
| `server_url` | URL WebSocket del servidor (`ws://` o `wss://`) |
| `token` | Token del agente generado desde el panel de Laravel |
| `log_level` | `debug` / `info` / `warn` / `error` (opcional, default `info`) |

> La configuración de impresoras **no va en config.json**. El servidor la entrega automáticamente tras validar el token.

### Cómo obtener el token del agente

El token lo genera Laravel al registrar una terminal desde el panel:

```
POST /api/tenant/print-configuration/agents/add
Authorization: Bearer <JWT del usuario>
{ "terminal_id": 1 }

→ { "success": true, "data": { "terminal_id": 1, "token": "agt_xxx..." } }
```

Ese `agt_xxx...` va al campo `token` del `config.json` del cliente. Laravel solo muestra el token una vez — después guarda únicamente su hash SHA-256.

### Impresoras — vienen del servidor

El servidor entrega la lista al cliente en el mensaje `config` tras conectar:

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
| `RED` | TCP socket directo → ESC/POS raw | `IP:puerto` (ej: `192.168.1.100:9100`) |
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

O con los scripts de la raíz:

```bash
./server.sh   # build + ejecuta el servidor
./client.sh   # build + ejecuta el cliente
```

---

## Binario del servidor (`usqay-print-server`)

### Endpoints WebSocket

| Ruta | Descripción |
|---|---|
| `GET /ws` | Conexión de agentes de impresión |
| `GET /ws/agent` | Alias de `/ws` |
| `GET /ws/monitor` | Conexión de monitores web (browsers) |

### API REST — requieren `X-Internal-Token`

| Método | Ruta | Descripción |
|---|---|---|
| `POST` | `/api/v1/jobs` | Laravel despacha un nuevo trabajo de impresión |
| `PUT` | `/api/v1/jobs/{job_id}/status` | Actualiza el estado de un trabajo |
| `POST` | `/api/v1/agents/{terminal_id}/config-refresh` | Empuja config nueva al agente en caliente, sin reconectar (ver `docs/config-refresh-impresoras.md`) |
| `GET` | `/api/v1/agents` | Lista todos los agentes conectados actualmente |
| `GET` | `/api/v1/agents/{terminal_id}/status` | Estado de un agente específico |
| `GET` | `/api/v1/agents/{terminal_id}/printers` | Solicita al agente las impresoras del OS disponibles |
| `POST` | `/api/v1/agents/{terminal_id}/kick` | Desconecta forzadamente un agente |

### API REST — sin autenticación (desarrollo / testing)

| Método | Ruta | Descripción |
|---|---|---|
| `POST` | `/api/print` | Crea un trabajo de prueba sin pasar por Laravel |
| `GET` | `/api/status` | Estado general: terminales conectadas y trabajos en curso |

**Ejemplo — crear trabajo de prueba:**

```bash
curl -X POST http://localhost:8090/api/print \
  -H "Content-Type: application/json" \
  -d '{
    "terminal_id": "15",
    "tipo_documento": "comanda",
    "payload": {
      "mesa": 3,
      "items": [
        {"nombre": "Lomo Saltado",    "cantidad": 2, "precio": 21.00},
        {"nombre": "Inca Kola 500ml", "cantidad": 2, "precio": 4.00}
      ]
    }
  }'
```

**Ejemplo — crear trabajo desde Laravel (con auth):**

```bash
curl -X POST http://localhost:8090/api/v1/jobs \
  -H "Content-Type: application/json" \
  -H "X-Internal-Token: token-interno-compartido" \
  -d '{
    "job_id": "123",
    "terminal_id": "15",
    "impresora_name_id": "uuid-de-impresora",
    "documento_slug": "comanda",
    "payload": { "mesa": 8, "items": [] },
    "expira_en": 300
  }'
```

---

## Binario del cliente (`usqay-print-client`)

### Flags disponibles

```bash
./usqay-print-client              # modo normal — conecta y procesa trabajos
./usqay-print-client -list        # lista impresoras del OS y sale
./usqay-print-client -insert      # inserta un trabajo de prueba en SQLite y sale
```

### Logs en consola

Los logs salen con colores y etiquetas de origen para facilitar el soporte:

```
15:04:05 INFO  [CLIENTE] usqay-print-client iniciado
15:04:05 INFO  [CLIENTE] base de datos abierta  path=./agent.db
15:04:05 INFO  [CLIENTE] conectando al servidor  url=ws://...
15:04:05 INFO  [WORKER]  worker iniciado  intervalo=500ms
15:04:05 INFO  [CLIENTE] sync enviado  trabajos_impresos_locales=0
15:04:05 INFO  [SERVER]  configuración aplicada  terminal_id=15 impresoras=2
```

Cuando llega y se procesa un trabajo:

```
15:04:10 INFO  [SERVER]  trabajo de impresión recibido  job_id=abc123 tipo=comanda
15:04:10 INFO  [WORKER]  procesando trabajo  job_id=abc123 tipo=comanda impresora_id=uuid
15:04:10 INFO  [WORKER]  impresión exitosa  job_id=abc123 duracion=142ms
```

| Etiqueta | Color | Qué representa |
|---|---|---|
| `[CLIENTE]` | verde | Conexión WebSocket, handshake, reconexión |
| `[SERVER]` | azul | Mensajes y órdenes recibidas del servidor |
| `[WORKER]` | magenta | Procesamiento e impresión física local |

El archivo de log diario (`logs/YYYY-MM-DD.log`) se escribe sin colores ANSI.

### Archivos que genera el cliente

```
build/
├── usqay-print-client      ← binario
├── config.json             ← configuración
├── agent.db                ← SQLite con la cola local de trabajos
└── logs/
    ├── 2026-06-17.log      ← log del día (texto plano)
    └── archive/
        └── 2026/
            └── mayo/
                └── 2026-mayo.zip   ← logs del mes anterior (comprimidos al iniciar)
```

---

## Protocolo WebSocket

### Cliente → Servidor

```json
// Registro — solo el token, sin terminal_id
{ "type": "register", "token": "agt_xxx", "version": "1.0.3" }

// Sincronización al reconectar (IDs impresos localmente desde la última sesión)
{ "type": "sync", "printed_jobs": ["id1", "id2"] }

// ACK de recepción (inmediato, antes de imprimir)
{ "type": "received", "job_id": "abc123" }

// Confirmación de impresión física exitosa
{ "type": "printed", "job_id": "abc123" }

// Reporte de fallo de impresión
{ "type": "error", "job_id": "abc123", "msg": "sin papel" }

// Heartbeat (cada 30s)
{ "type": "ping" }
```

### Servidor → Cliente

```json
// Configuración inicial: identidad + impresoras del sucursal
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

// Config del panel cambió — se aplica en caliente, mismo mensaje que la config inicial
{ "type": "config", "terminal_id": "2", "printers": [ ... ] }

// Esta terminal fue desplazada por una conexión más reciente
{ "type": "kick", "reason": "nueva conexión registrada" }
```

---

## Estados de un trabajo (SQLite del cliente)

```
PENDING     → recibido del servidor, guardado en SQLite
PROCESSING  → worker lo tomó y está enviando a la impresora
PRINTED     → impresión física confirmada
ERROR       → fallo (el worker continúa con el siguiente trabajo)
```

---

## Endpoints de Laravel requeridos por el servidor

El servidor Go llama a estos endpoints. Todos van con `X-Internal-Token`.

| Método | Ruta | Descripción |
|---|---|---|
| `POST` | `/api/tenant/print-configuration/agents/validate` | Valida token del agente y devuelve `terminal_id` + `printers` |
| `PUT` | `/api/tenant/print-configuration/queue/{id}/status` | Actualiza estado de un trabajo |
| `POST` | `/api/tenant/print-configuration/monitor-token/validate` | Valida token del monitor web |

**Contrato de `/agents/validate`**

```json
// Request
{ "token": "agt_xxx" }

// Response
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

## Requisitos

- **Go 1.21+**
- **Windows:** binario único sin dependencias extra (sin CGO)
- **Linux:** `lp` / CUPS instalado para impresoras USB/SERIE; impresoras RED no requieren nada

---

## Dependencias clave

| Paquete | Módulo | Motivo |
|---|---|---|
| `modernc.org/sqlite` | cliente | SQLite sin CGO — binario único en Windows |
| `github.com/alexbrainman/printer` | cliente | Windows Print Spooler via `winspool.drv` |
| `github.com/coder/websocket` | ambos | WebSocket ligero, context-aware |

> **Importante:** nunca usar `mattn/go-sqlite3`. Requiere GCC en Windows y rompe la compilación de binario único.
