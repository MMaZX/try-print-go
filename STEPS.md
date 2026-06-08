# STEPS — Plan de implementación del PoC

**Basado en:** poc-impresion-termica-remastered.md
**Asignado a:** Guillermo
**Fecha:** 2026-06-08

---

## Resumen de etapas

```text
Etapa 1 → Núcleo Físico     → imprimir desde Go sin nada más
Etapa 2 → Fortaleza Local   → SQLite + worker autónomo
Etapa 3 → Cable de Cobre    → WebSocket server + client conectados
```

Cada etapa es independiente y entregable.

No pasar a la siguiente hasta que la anterior esté funcionando.

---

# Etapa 1 — El Núcleo Físico

**Objetivo:** ejecutar `go run main.go` y ver un ticket físico salir de la impresora.

Sin WebSocket. Sin SQLite. Sin servidor. Un solo archivo.

**Valida los Criterios de éxito #4, #5 y #6 del PoC.**

---

## 1.1 Estructura mínima

```text
usqay-print-client/
└── cmd/
    └── client/
        └── main.go
```

---

## 1.2 Impresión por red (TCP)

Conectar directamente a la impresora por IP y enviar comandos ESC/POS.

```go
conn, err := net.DialTimeout("tcp", "192.168.x.x:9100", 5*time.Second)
```

Comandos ESC/POS mínimos a validar:

```text
ESC @        → inicializar impresora
ESC E 1      → negrita ON
ESC E 0      → negrita OFF
ESC a 1      → centrar
GS V 66 0   → corte parcial
```

**Prueba exitosa:** ticket con número de mesa, productos, cantidades y corte de papel.

---

## 1.3 Impresión por sistema operativo (spooler)

Aquí está el riesgo técnico principal del proyecto.

Go no tiene una API unificada para Windows spooler y CUPS. Se requiere compilación condicional.

Crear dos archivos separados:

```text
internal/printer/
├── printer_windows.go   → //go:build windows
└── printer_linux.go     → //go:build linux
```

### Windows — `printer_windows.go`

```go
//go:build windows

import "github.com/alexbrainman/printer"

func PrintByName(name string, data []byte) error {
    p, err := printer.Open(name)
    if err != nil {
        return err
    }
    defer p.Close()

    if err := p.StartDocument("usqay-job", "RAW"); err != nil {
        return err
    }
    defer p.EndDocument()

    if err := p.StartPage(); err != nil {
        return err
    }

    _, err = p.Write(data)
    p.EndPage()
    return err
}
```

### Linux — `printer_linux.go`

```go
//go:build linux

import "os/exec"

func PrintByName(name string, data []byte) error {
    cmd := exec.Command("lp", "-d", name, "-")
    cmd.Stdin = bytes.NewReader(data)
    return cmd.Run()
}
```

**Prueba exitosa:** mismo ticket impreso usando el nombre de la impresora (`"Epson TM-T20 Receipt"`) sin especificar IP ni puerto COM.

**Pregunta a responder aquí:** ¿es suficiente la interfaz `PrintByName(name string, data []byte) error` para abstraer ambos OS, o hace falta más contexto por plataforma?

---

## 1.4 Criterio de salida de Etapa 1

```text
✓ Ticket impreso por TCP (red)
✓ Ticket impreso por nombre de sistema (spooler / CUPS)
✓ El binario compila con: GOOS=windows go build
✓ El binario compila con: GOOS=linux go build
✓ Boceto de comanda impreso: mesa, productos, cantidades, corte
```

---

# Etapa 2 — La Fortaleza Local

**Objetivo:** el cliente puede recibir una orden, guardarla en SQLite y procesarla en orden aunque la impresora no esté disponible en ese momento.

Sin WebSocket. Sin servidor. El worker opera solo.

**Valida el Criterio de éxito #3 del PoC.**

---

## 2.1 Estructura

```text
usqay-print-client/
├── cmd/client/main.go
└── internal/
    ├── queue/
    │   ├── sqlite.go       → crear/abrir agent.db
    │   ├── repository.go   → insert, getNextPending, updateStatus
    │   └── worker.go       → loop de procesamiento
    └── printer/
        ├── printer_windows.go
        └── printer_linux.go
```

---

## 2.2 Tabla en SQLite

```sql
CREATE TABLE IF NOT EXISTS print_jobs (
    id          TEXT PRIMARY KEY,
    payload     TEXT NOT NULL,
    estado      TEXT NOT NULL DEFAULT 'PENDING',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);
```

Estados:

```text
PENDING     → recibido, esperando ser procesado
PROCESSING  → worker lo tomó
PRINTED     → impresión exitosa
ERROR       → fallo en la impresión
```

---

## 2.3 Worker

El worker corre en una goroutine separada y procesa un trabajo a la vez.

```text
loop cada 500ms
    ↓
busca un registro en estado PENDING
    ↓
si no hay → espera siguiente tick
    ↓
si hay → cambia a PROCESSING
    ↓
intenta imprimir
    ↓
éxito → PRINTED
error → ERROR (guarda mensaje de error)
```

El binario no debe hacer panic ante ningún error de impresión.

Todo error se captura, se loguea y se guarda en el campo `error_msg`.

---

## 2.4 Prueba de simulación

Insertar un trabajo manualmente en SQLite (código duro en main.go o DB browser):

```sql
INSERT INTO print_jobs (id, payload, estado, created_at, updated_at)
VALUES ('test-001', '{"mesa":5,"items":[{"nombre":"Lomo Saltado","cantidad":2}]}', 'PENDING', datetime('now'), datetime('now'));
```

El worker debe detectarlo y procesarlo sin intervención manual.

---

## 2.5 Prueba de caos

```text
1. Insertar un trabajo en PENDING
2. Apagar la impresora antes de que el worker lo tome
3. Verificar que el estado cambia a ERROR
4. Verificar que el binario sigue corriendo (no panic)
5. Encender la impresora
6. Insertar otro trabajo
7. Verificar que el worker lo imprime correctamente
```

---

## 2.6 Criterio de salida de Etapa 2

```text
✓ agent.db se crea automáticamente al iniciar
✓ Trabajo insertado manualmente llega a estado PRINTED
✓ Impresora apagada → estado ERROR, binario sigue vivo
✓ Worker retoma trabajos nuevos después de un error
✓ Los logs del día se escriben en logs/YYYY-MM-DD.log
```

---

# Etapa 3 — El Cable de Cobre

**Objetivo:** conectar el cliente (con su SQLite y worker ya funcionando) al servidor WebSocket y completar el flujo de extremo a extremo.

**Valida los Criterios de éxito #1, #2, #7 y #8 del PoC.**

---

## 3.1 Estructura final

```text
usqay-print-server/
├── cmd/server/main.go
└── internal/
    ├── websocket/
    │   ├── hub.go       → registro de terminales, kick de conexiones duplicadas
    │   ├── client.go    → una goroutine por conexión
    │   └── messages.go  → tipos de mensajes
    └── config/
        └── config.go

usqay-print-client/
├── cmd/client/main.go
└── internal/
    ├── websocket/
    │   ├── connection.go  → dial, reconexión con backoff
    │   └── messages.go
    ├── queue/
    │   ├── sqlite.go
    │   ├── repository.go
    │   └── worker.go
    ├── printer/
    │   ├── printer_windows.go
    │   └── printer_linux.go
    └── config/
        └── config.go
```

---

## 3.2 Servidor mínimo

El servidor solo necesita:

```text
1. Levantar HTTP en el puerto configurado
2. Aceptar upgrade a WebSocket
3. Procesar mensaje { type: "register" }
4. Validar terminal_id y token
5. Si ya existe una conexión activa para ese terminal_id → hacer kick a la anterior
6. Registrar la nueva conexión
7. Enviar mensaje { type: "print", job_id, payload }
8. Recibir { type: "received" } y { type: "printed" } / { type: "error" }
```

---

## 3.3 Manejo de conexión duplicada (kick)

```text
Cliente A conectado como "caja-01"
       ↓
Cliente A se reinicia abruptamente
       ↓
Cliente A reconecta antes de que el servidor detecte el timeout
       ↓
Servidor detecta: ya existe conexión activa para "caja-01"
       ↓
Servidor envía cierre a la conexión anterior
       ↓
Servidor cierra el socket anterior
       ↓
Servidor registra la nueva conexión
```

La nueva instancia siempre gana. No rechazar, siempre hacer kick.

---

## 3.4 Reconexión del cliente con backoff

```text
WebSocket desconectado
       ↓
espera 1s → reintenta
       ↓ falla
espera 2s → reintenta
       ↓ falla
espera 4s → reintenta
       ↓ falla
espera 8s → reintenta
       ↓ ...
máximo: 60s entre intentos
```

Mientras el cliente está desconectado, el worker local sigue procesando trabajos que ya están en SQLite.

---

## 3.5 El Flujo de Oro (end-to-end)

```text
Servidor inicia en :8080
       ↓
Cliente conecta por WebSocket
       ↓
{ type: "register", terminal_id: "caja-01", token: "xxx" }
       ↓
Servidor valida → responde { type: "config", impresoras: [...] }
       ↓
Servidor envía trabajo de prueba:
{ type: "print", job_id: "e2e-001", tipo_documento: "comanda", payload: {...} }
       ↓
Cliente recibe → guarda en SQLite { estado: PENDING }
       ↓
Cliente responde inmediatamente: { type: "received", job_id: "e2e-001" }
       ↓
Worker local toma el trabajo → PROCESSING → imprime → PRINTED
       ↓
Cliente envía: { type: "printed", job_id: "e2e-001" }
       ↓
Servidor recibe confirmación
       ↓
Ticket físico en la impresora
```

---

## 3.6 Prueba de caída de internet

```text
1. Flujo de Oro corriendo con éxito
2. Desconectar el cable de red (o deshabilitar el adaptador)
3. Servidor envía un trabajo nuevo (queda en cola pendiente en el servidor)
4. Cliente pierde WebSocket → entra en backoff
5. Worker local sigue procesando trabajos previos que tenía en SQLite
6. Reconectar la red
7. Cliente reconecta → register → sync
8. Servidor reenvía trabajos que el cliente nunca recibió
9. Worker procesa e imprime
10. Cliente confirma: { type: "printed" }
```

Ningún trabajo debe perderse.

---

## 3.7 Criterio de salida de Etapa 3

```text
✓ Servidor acepta conexión WebSocket y autentica terminal
✓ Conexión duplicada hace kick automático a la anterior
✓ Flujo de Oro completo: servidor → SQLite → worker → impresora → confirmación
✓ Cliente se reconecta automáticamente tras corte de red
✓ Trabajos recibidos antes del corte se imprimen sin pérdida
✓ Servidor recibe confirmación de impresión
```

---

# Referencias técnicas por etapa

## Etapa 1

**`alexbrainman/printer`** — API nativa de Windows para enviar bytes crudos al spooler via `winspool.drv`. Buscar: _"golang alexbrainman/printer example"_ para ver cómo abrir una impresora por nombre, iniciar un documento RAW y escribir el stream de bytes.

**Build tags** — Buscar: _"golang go:build conditional compilation rules"_ para entender cómo `printer_windows.go` y `printer_linux.go` coexisten en el mismo paquete sin colisionar al compilar en cada plataforma.

**ESC/POS** — Buscar: _"ESC/POS raw bytes commands formatting sheet"_ para la tabla de bytes estándar:

```text
ESC @        → 0x1B 0x40  → inicializar impresora
ESC E 1      → 0x1B 0x45 0x01  → negrita ON
ESC E 0      → 0x1B 0x45 0x00  → negrita OFF
ESC a 1      → 0x1B 0x61 0x01  → centrar texto
GS V 66 0   → 0x1D 0x56 0x42 0x00  → corte parcial
```

---

## Etapa 2

**`modernc.org/sqlite`** — Implementación de SQLite escrita 100% en Go puro. **No usa CGO.** Compila a binario único en Windows sin necesitar GCC ni MinGW. Buscar: _"golang modernc.org/sqlite database/sql"_.

No usar `mattn/go-sqlite3`: requiere un compilador C instalado en la máquina de compilación, lo cual es un problema real en entornos Windows.

**Worker con goroutines** — Buscar: _"golang worker pool pattern using channels and tickers"_ para estructurar el loop que lee SQLite en segundo plano sin bloquear el hilo principal.

---

## Etapa 3

**Reconexión con backoff** — Buscar: _"golang gorilla/websocket connection recovery backoff"_ para implementar el algoritmo de espera exponencial (1s → 2s → 4s → 8s…) que evita saturar el servidor cuando el restaurante pierde internet.

**Detección de conexiones zombie** — Buscar: _"websocket zombie connections detect ping pong"_ para entender cómo el servidor detecta sockets muertos que no se cerraron limpiamente — escenario habitual en cortes de luz. Implementar el ciclo ping/pong del protocolo WebSocket con timeout.

---

# Cómo trabajar con el CLI

Seguir la metodología **Explorar → Planificar → Implementar** por piezas. No pedir que programe todo el sistema de golpe.

---

## Paso 1 — Modo Plan (inicio de cada etapa)

Al abrir una sesión nueva, dar contexto y pedir diseño antes de código:

```text
"Lee CLAUDE.md. Vamos a trabajar únicamente en la Etapa 1.
Genera una estructura de interfaz común para aislar el sistema operativo
antes de escribir cualquier código."
```

No aprobar ningún archivo hasta que el diseño de la interfaz o la estructura esté clara.

---

## Paso 2 — Implementación atómica

Una vez aprobado el diseño, pedir un solo archivo por turno:

```text
"Escribe exclusivamente printer_windows.go usando alexbrainman/printer."
```

Probar en la máquina local antes de pedir el siguiente archivo.

Un archivo implementado y probado vale más que cinco archivos sin probar.

---

## Paso 3 — Limpiar contexto entre etapas

Cuando la etapa anterior funcione, ejecutar `/clear` en la terminal.

```text
/clear
```

Esto elimina el ruido de la conversación anterior y libera la ventana de contexto.

Gracias al `CLAUDE.md`, la siguiente sesión arranca con todo el contexto necesario sin repetir explicaciones: stack, reglas, skills, dependencias y flujo de impresión ya están definidos ahí.

```text
Etapa 1 completa → /clear → "Lee CLAUDE.md. Vamos a la Etapa 2: SQLite + worker."
Etapa 2 completa → /clear → "Lee CLAUDE.md. Vamos a la Etapa 3: WebSocket."
```

---

# Criterio de éxito global del PoC

```text
Los tres criterios de salida anteriores cumplidos
+
Un ticket físico real producido de extremo a extremo
```

Si este flujo funciona, queda validada la viabilidad técnica de:

```text
Go + WebSocket + SQLite local
```

como arquitectura base para impresión térmica distribuida en Usqay.
