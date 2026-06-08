# POC — Impresión Térmica vía WebSocket + Agente Local Go

**Asignado a:** Guillermo
**Tipo:** Proof of Concept / Investigación técnica
**Fecha:** 2026-06-08
**Estado:** backlog → in-progress al iniciar

---

# Objetivo de la POC

El objetivo de esta prueba no es construir el sistema definitivo de impresión.

El objetivo es validar con código real que:

1. Go puede mantener una conexión WebSocket estable.
2. Go puede recibir órdenes desde un backend remoto.
3. Go puede detectar y utilizar impresoras instaladas en Windows.
4. Go puede enviar comandos ESC/POS reales.
5. El sistema puede sobrevivir a desconexiones temporales de internet sin perder trabajos.

---

# Arquitectura propuesta

```text
┌──────────────────────┐
│ usqay-print-server   │
│ (WebSocket Server)   │
└──────────┬───────────┘
           │
           │ WSS
           │
           ▼
┌──────────────────────┐
│ usqay-print-client   │
│ (Agente Local Go)    │
└──────────┬───────────┘
           │
           ▼
      SQLite Local
           │
           ▼
      Worker Local
           │
           ▼
      Impresora
```

---

# Filosofía del diseño

La persistencia NO depende del WebSocket.

El WebSocket únicamente transporta mensajes.

La persistencia vive dentro del agente local mediante SQLite.

Esto permite que:

* una caída del WebSocket no elimine trabajos
* una caída temporal de internet no elimine trabajos
* el agente continúe procesando trabajos ya recibidos
* exista trazabilidad local

---

# Componentes del PoC

## Ejecutable 1 — usqay-print-server

Responsabilidades:

* aceptar conexiones WebSocket
* registrar terminales conectados
* enviar órdenes de impresión
* recibir confirmaciones
* reenviar trabajos pendientes si un cliente se reconecta

No imprime.

No conoce impresoras.

No ejecuta ESC/POS.

---

### Estructura

```text
usqay-print-server/

cmd/
└── server/
    └── main.go

internal/
├── websocket/
│   ├── hub.go
│   ├── client.go
│   └── messages.go
│
├── jobs/
│   └── dispatcher.go
│
├── registry/
│   └── terminals.go
│
└── config/
    └── config.go
```

---

## Ejecutable 2 — usqay-print-client

Responsabilidades:

* conectar al servidor WebSocket
* recibir órdenes
* guardar trabajos en SQLite
* procesar cola local
* imprimir
* confirmar resultados
* reconectar automáticamente

---

### Estructura

```text
usqay-print-client/

cmd/
└── client/
    └── main.go

internal/
├── websocket/
│   ├── connection.go
│   └── messages.go
│
├── queue/
│   ├── sqlite.go
│   ├── repository.go
│   └── worker.go
│
├── printer/
│   ├── windows.go
│   ├── usb.go
│   ├── network.go
│   └── escpos/
│       └── builder.go
│
└── config/
    └── config.go
```

---

# Persistencia local

El agente mantiene una base SQLite local.

```text
agent.db
```

Tabla principal:

```sql
CREATE TABLE print_jobs (
    id TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    estado TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
```

Estados:

```text
PENDING
PROCESSING
PRINTED
ERROR
```

---

# Flujo de impresión

## Paso 1

Servidor envía:

```json
{
  "type": "print",
  "job_id": "123",
  "document": "comanda"
}
```

---

## Paso 2

Cliente recibe.

Guarda inmediatamente:

```text
SQLite
estado = PENDING
```

Responde:

```json
{
  "type": "received",
  "job_id": "123"
}
```

---

## Paso 3

Worker local toma el trabajo.

```text
PENDING
    ↓
PROCESSING
```

---

## Paso 4

Impresión.

```text
ESC/POS
      ↓
Impresora
```

---

## Paso 5

Si imprime correctamente:

```text
PROCESSING
     ↓
PRINTED
```

y notifica:

```json
{
  "type": "printed",
  "job_id": "123"
}
```

---

# Reconexión automática

Si internet desaparece:

```text
Servidor
     X
Cliente
```

Los trabajos ya recibidos siguen existiendo en SQLite.

Cuando vuelve la conexión:

```text
Cliente
   ↓
Reconnect
   ↓
Servidor
```

el cliente informa:

```json
{
  "type": "sync"
}
```

y el servidor puede reenviar trabajos pendientes.

---

# Principio de autoridad de impresión

La autoridad final sobre el estado de impresión pertenece al agente local (`usqay-print-client`) y no al servidor.

## Motivo

El servidor no tiene acceso físico a:

* impresoras
* puertos USB
* spooler de Windows
* estado real de la impresora
* disponibilidad de energía eléctrica
* disponibilidad de red local

Por lo tanto, el servidor no puede afirmar que un documento fue impreso.

Únicamente puede afirmar que:

```text
solicitó una impresión
```

La confirmación real debe provenir del agente local.

---

# Flujo de responsabilidad

## Servidor

Responsabilidades:

* generar evento de impresión
* enviar trabajo al cliente
* registrar solicitudes emitidas
* recibir actualizaciones del cliente

El servidor nunca cambia un trabajo a:

```text
PRINTED
```

por iniciativa propia.

---

## Cliente

Responsabilidades:

* recibir trabajo
* persistir localmente
* ejecutar impresión
* verificar resultado
* actualizar estado real

El cliente es la única entidad autorizada para informar:

```text
PRINTED
```

o

```text
ERROR
```

---

# Escenario de pérdida de internet

Ejemplo:

```text
Servidor
     ↓
envía trabajo #100
     ↓
Cliente recibe
     ↓
guarda en SQLite
     ↓
Internet cae
     ↓
Cliente imprime correctamente
```

Estado real:

```text
Trabajo impreso
```

Pero el servidor todavía no lo sabe.

---

Cuando la conectividad regresa:

```text
Cliente
    ↓
reconecta
    ↓
sincroniza estados pendientes
    ↓
informa:
```

```json
{
  "job_id": "100",
  "status": "PRINTED"
}
```

Recién en ese momento el servidor actualiza sus registros.

---

# Sincronización eventual

El sistema trabaja bajo consistencia eventual.

Eso significa que:

```text
Servidor
```

puede estar temporalmente desactualizado.

Mientras que:

```text
Cliente
```

mantiene el estado verdadero de la operación física.

La sincronización ocurre automáticamente cuando la conectividad es restaurada.

---

# Regla fundamental

La impresión es un evento físico.

Por lo tanto:

```text
El estado real de impresión
siempre pertenece al agente local.
```

El backend únicamente mantiene una copia sincronizada de dicho estado.

---

# Modelo de identificación

Registro inicial:

```json
{
  "type": "register",
  "business_id": "empresa-001",
  "terminal_id": "caja-01",
  "token": "xxxxx"
}
```

Restricción:

```text
1 terminal_id
=
1 conexión activa
```

Si una segunda instancia intenta conectarse con el mismo terminal_id:

```text
expulsar la conexión anterior (kick)
registrar la nueva conexión
```

La nueva instancia siempre gana.

Este comportamiento es necesario porque en un reinicio abrupto el servidor puede tardar en detectar que la conexión anterior murió (conexión zombie). Si la política fuera rechazar la nueva, el terminal quedaría bloqueado hasta que el servidor libere el socket por timeout.

El servidor debe:

1. detectar que ya existe una conexión activa para ese terminal_id
2. enviar un mensaje de cierre a la conexión anterior
3. cerrar el socket anterior
4. registrar la nueva conexión

---

# Scope del Try (PoC)

El objetivo NO es construir el sistema definitivo de impresión.

El objetivo es validar los riesgos técnicos principales utilizando código real y una arquitectura basada en:

* usqay-print-server
* usqay-print-client
* WebSocket bidireccional
* SQLite local
* ESC/POS

La POC debe demostrar que el flujo completo funciona desde el backend hasta la impresora física y que puede sobrevivir a desconexiones temporales.

---

# Criterios de éxito del PoC

## 1. Comunicación WebSocket estable

El cliente Go mantiene una conexión WebSocket persistente con el servidor.

Debe poder:

* conectarse
* autenticarse
* recibir mensajes
* reconectarse automáticamente

---

## 2. Registro único de terminal

El servidor identifica correctamente cada terminal mediante:

* business_id
* terminal_id

No deben existir dos conexiones activas para el mismo terminal_id.

---

## 3. Persistencia local SQLite

Al recibir un trabajo de impresión:

* el cliente debe almacenarlo inmediatamente en SQLite
* el trabajo debe sobrevivir al cierre inesperado del proceso
* el trabajo debe sobrevivir a una pérdida temporal de internet

---

## 4. Impresión a impresora IP

El cliente Go debe poder conectarse a:

192.168.x.x:9100

y enviar comandos ESC/POS simples.

---

## 5. Impresión a impresora instalada en Windows

El cliente debe poder:

* detectar impresoras disponibles
* seleccionar una impresora
* enviar texto simple para impresión

Go no tiene acceso unificado nativo al spooler de Windows y a CUPS en Linux con el mismo paquete estándar.

En Windows se requiere llamar a la API `winspool` vía syscalls o una librería como `alexbrainman/printer`.

En Linux se ejecuta `lp -d <nombre>` o se enlaza contra libcups.

El PoC debe validar si se requieren directivas de compilación condicional para aislar los dos caminos:

```go
//go:build windows

//go:build linux
```

---

## 6. Probar un template real

Imprimir una comanda real con:

* número de mesa
* fecha
* hora
* items
* cantidades

---

## 7. Confirmación de impresión

El cliente debe informar al servidor:

* trabajo recibido
* trabajo procesado
* trabajo impreso
* trabajo con error

---

## 8. Recuperación ante caída de internet

Escenario obligatorio:

* servidor envía trabajo
* cliente guarda en SQLite
* conexión se pierde
* cliente imprime correctamente
* conexión vuelve
* cliente sincroniza el estado real

El trabajo no debe perderse.

---

## 9. Documentar problemas encontrados

Documentar:

* WebSocket
* reconexión automática
* detección de impresoras
* compatibilidad ESC/POS
* SQLite
* permisos del sistema operativo
* spooler de impresión de Windows

---

# Fuera del scope del Try

No implementar:

* sistema final de colas productivas
* dashboard administrativo
* instalador Windows
* servicio Windows
* actualización automática
* autenticación definitiva
* multiempresa completa
* monitoreo
* métricas
* observabilidad
* clustering
* templates finales
* firma digital SUNAT
* integración móvil

La POC únicamente debe demostrar la viabilidad técnica de la arquitectura propuesta.

---

# Preguntas que el Try debe responder

1. ¿El cliente Go puede mantener una conexión WebSocket estable con reconexión automática fiable?
2. ¿Podemos imprimir por nombre de impresora del sistema operativo (Windows spooler / CUPS en Linux) sin necesitar puerto COM ni IP?
3. ¿La latencia WebSocket → Go → impresora es aceptable para comandas en operación real (objetivo: < 500ms)?
4. ¿SQLite local garantiza que no se pierden trabajos ante cortes de internet?
5. ¿Una sola librería ESC/POS cubre lo que necesitamos (logo, QR, corte, negrita, alineación)?
6. ¿El manejo del spooler de Windows y CUPS de Linux requiere compilación condicional (`//go:build windows` / `//go:build linux`) o es posible abstraerlo con una interfaz común?

---

# Base de datos en el servidor

## Lo que ya existe en el backend

**`impresoras`** (existe, Epic 11-3 Arturo)
```
id | descripcion | tipo (USB|Wifi|Otros) | ip | observaciones | estado
```

**`terminales`** (existe, Epic 11-3 Arturo)
```
id | descripcion | sucursal_id | estado
```

## Cambios necesarios para la arquitectura WebSocket

---

### Tabla `impresoras` — campos a agregar

La tabla actual no distingue el mecanismo de conexión con suficiente precisión y no soporta impresión por nombre del sistema operativo.

```sql
ALTER TABLE impresoras
  ADD COLUMN tipo_conexion  ENUM('red','sistema','serie') NOT NULL DEFAULT 'red',
  ADD COLUMN nombre_sistema VARCHAR(200) NULL,   -- nombre exacto en Windows spooler o CUPS
  ADD COLUMN puerto_tcp     INT          DEFAULT 9100,
  ADD COLUMN puerto_serie   VARCHAR(30)  NULL,   -- COM3 (Windows) o /dev/ttyUSB0 (Linux)
  ADD COLUMN ancho_papel    ENUM('58','80') NOT NULL DEFAULT '80',
  ADD COLUMN terminal_id    INT NULL REFERENCES terminales(id);
```

Tipos de conexión:

| tipo_conexion | Sistema operativo | Mecanismo |
|---|---|---|
| `red` | Windows / Linux | TCP socket directo → IP:puerto (ESC/POS raw) |
| `sistema` | Windows | Windows Print Spooler → nombre_sistema |
| `sistema` | Linux | CUPS → nombre_sistema (lpstat -p) |
| `serie` | Windows / Linux | Puerto serie → COM3 o /dev/ttyUSB0 |

Para `tipo_conexion = 'sistema'` el cliente Go no necesita saber si es Windows o Linux: imprime con el nombre exacto y el OS resuelve el driver.

---

### Tabla nueva: `impresora_documentos`

Mapea qué impresora imprime qué tipo de documento por sucursal.

```sql
CREATE TABLE impresora_documentos (
  id              INT PRIMARY KEY AUTO_INCREMENT,
  sucursal_id     INT NOT NULL,
  tipo_documento  ENUM('comanda','precuenta','comprobante','cierre','reporte') NOT NULL,
  impresora_id    INT NOT NULL REFERENCES impresoras(id),
  copias          TINYINT DEFAULT 1,
  activo          BOOLEAN DEFAULT true,
  created_at      TIMESTAMP DEFAULT NOW(),
  updated_at      TIMESTAMP DEFAULT NOW() ON UPDATE NOW(),
  UNIQUE(sucursal_id, tipo_documento)
);
```

Ejemplo de datos:

```
sucursal_id | tipo_documento | impresora_id | copias
     1      |   comanda      |      2       |   1     ← impresora cocina
     1      |   precuenta    |      1       |   1     ← impresora caja
     1      |   comprobante  |      1       |   2     ← impresora caja, 2 copias
     1      |   cierre       |      1       |   1
```

---

### Tabla nueva: `agentes_impresion`

Registra cada instancia del cliente Go instalada en cada PC del restaurante.

```sql
CREATE TABLE agentes_impresion (
  id              INT PRIMARY KEY AUTO_INCREMENT,
  terminal_id     INT NOT NULL REFERENCES terminales(id),
  token           VARCHAR(64) NOT NULL UNIQUE,   -- token de autenticación WebSocket
  version         VARCHAR(20) NULL,
  ultimo_ping     TIMESTAMP NULL,
  activo          BOOLEAN DEFAULT true,
  created_at      TIMESTAMP DEFAULT NOW(),
  updated_at      TIMESTAMP DEFAULT NOW() ON UPDATE NOW()
);
```

El token viaja en el mensaje inicial de registro:

```json
{
  "type": "register",
  "business_id": "empresa-001",
  "terminal_id": "caja-01",
  "token": "xxxxx"
}
```

---

### Tabla nueva: `cola_impresion`

El servidor almacena aquí cada trabajo de impresión.

La entrega al cliente se realiza por WebSocket push, no por polling.

```sql
CREATE TABLE cola_impresion (
  id              INT PRIMARY KEY AUTO_INCREMENT,
  agente_id       INT NOT NULL REFERENCES agentes_impresion(id),
  tipo_documento  ENUM('comanda','precuenta','comprobante','cierre','reporte') NOT NULL,
  payload         JSON NOT NULL,
  estado          ENUM('pendiente','enviado','impreso','error') DEFAULT 'pendiente',
  intentos        TINYINT DEFAULT 0,
  error_msg       TEXT NULL,
  created_at      TIMESTAMP DEFAULT NOW(),
  enviado_at      TIMESTAMP NULL,
  procesado_at    TIMESTAMP NULL
);
```

Estados del servidor:

```text
pendiente  → trabajo creado, cliente desconectado o sin ACK
enviado    → servidor entregó vía WebSocket, esperando confirmación del cliente
impreso    → cliente confirmó impresión exitosa
error      → cliente reportó error
```

---

## Configuración que el servidor envía al conectar

Al registrarse, el servidor responde con la configuración completa de impresoras:

```json
{
  "type": "config",
  "sucursal": {
    "id": 1,
    "nombre": "Restaurante Centro",
    "ruc": "20123456789",
    "direccion": "Av. Lima 123"
  },
  "impresoras": [
    {
      "id": 1,
      "nombre": "Caja",
      "tipo_conexion": "sistema",
      "nombre_sistema": "Epson TM-T20 Receipt",
      "ancho_papel": "80"
    },
    {
      "id": 2,
      "nombre": "Cocina",
      "tipo_conexion": "red",
      "ip": "192.168.1.100",
      "puerto_tcp": 9100,
      "ancho_papel": "58"
    }
  ],
  "ruteo": {
    "comanda":     { "impresora_id": 2, "copias": 1 },
    "precuenta":   { "impresora_id": 1, "copias": 1 },
    "comprobante": { "impresora_id": 1, "copias": 2 }
  }
}
```

El cliente guarda esta configuración en SQLite local para operar sin internet.

---

# Flujo completo del sistema

## Registro inicial

```text
Cliente Go inicia
       ↓
WebSocket connect → servidor
       ↓
{ type: "register", terminal_id: "caja-01", token: "xxx" }
       ↓
Servidor valida token en agentes_impresion
       ↓
{ type: "config", impresoras: [...], ruteo: {...} }
       ↓
Cliente guarda config en SQLite local
       ↓
Servidor envía trabajos pendientes (si los hay)
```

---

## Trabajo de impresión en tiempo real

```text
Cajero cobra un pedido
       ↓
Backend inserta en cola_impresion { estado: "pendiente" }
       ↓
Servidor envía por WebSocket:
{ type: "print", job_id: 123, tipo_documento: "comprobante", payload: {...} }
       ↓
Cliente recibe → guarda en SQLite local { estado: PENDING }
       ↓
{ type: "received", job_id: 123 }   ← ACK inmediato al servidor
       ↓
Servidor actualiza cola_impresion { estado: "enviado" }
```

---

## Procesamiento local

```text
Worker local toma el trabajo PENDING
       ↓
PENDING → PROCESSING
       ↓
Resuelve impresora: comprobante → impresora_id 1
       ↓
tipo_conexion = "sistema"
       ↓
nombre_sistema = "Epson TM-T20 Receipt"
       ↓
Construye ESC/POS → envía al spooler (Windows o CUPS)
       ↓
Impresora imprime
       ↓
PROCESSING → PRINTED
       ↓
{ type: "printed", job_id: 123 }
       ↓
Servidor actualiza cola_impresion { estado: "impreso" }
```

---

## Reconexión y sincronización

```text
Internet cae
       ↓
Trabajos ya guardados en SQLite → se siguen imprimiendo localmente
       ↓
Internet vuelve
       ↓
Cliente reconecta WebSocket
       ↓
{ type: "register", terminal_id: "caja-01", token: "xxx" }
       ↓
{ type: "sync", printed_jobs: [123, 124, 125] }   ← informa lo que ya imprimió
       ↓
Servidor actualiza estados en cola_impresion
       ↓
Servidor reenvía los trabajos que el cliente nunca recibió
```

---

# Ciclo de vida del cliente Go

```text
1. ARRANQUE
   ├── Lee token y URL del servidor (config.json o argumento CLI)
   ├── Abre conexión WebSocket
   ├── Envía { type: "register", terminal_id, token }
   ├── Recibe config de impresoras y ruteo
   ├── Guarda config en SQLite local
   └── Inicia worker local de impresión

2. EN OPERACIÓN (event-driven, sin polling)
   ├── Recibe { type: "print", job_id, payload }
   ├── Guarda en SQLite { estado: PENDING }
   ├── Responde { type: "received", job_id }   ← ACK inmediato
   ├── Worker resuelve impresora por tipo_conexion:
   │   ├── sistema → Windows spooler / CUPS por nombre_sistema
   │   ├── red     → TCP socket directo a IP:puerto_tcp (ESC/POS)
   │   └── serie   → puerto serie COM3 o /dev/ttyUSB0
   ├── Construye y envía ESC/POS
   └── Responde { type: "printed" } o { type: "error", msg: "..." }

3. HEARTBEAT (cada 30s)
   ├── { type: "ping" } → servidor responde { type: "pong" }
   └── Detecta conexiones zombie

4. RECONEXIÓN AUTOMÁTICA
   └── WebSocket caído → backoff exponencial (1s, 2s, 4s, 8s…)
       → reconecta → register → sincroniza estados pendientes
```

---

# Logging

Cada componente registra sus eventos de forma independiente.

Ninguno depende del otro para escribir sus logs.

---

## Política de rotación y archivado

Los logs se rotan diariamente y se archivan mensualmente.

### Estructura de archivos

```text
logs/
├── 2026-06-08.log          ← día actual (en escritura)
├── 2026-06-07.log          ← día anterior (pendiente de archivar al cierre del mes)
└── archive/
    └── 2026/
        ├── enero/
        │   └── 2026-enero.zip    ← contiene 2026-01-01.log … 2026-01-31.log
        ├── febrero/
        │   └── 2026-febrero.zip
        └── mayo/
            └── 2026-mayo.zip
```

### Reglas

```text
Rotación diaria
  → Al inicio de cada día se crea un archivo nuevo: YYYY-MM-DD.log
  → El archivo del día anterior se cierra

Archivado mensual
  → Al inicio de cada mes se comprimen todos los .log del mes anterior
  → Destino: logs/archive/YYYY/mes/YYYY-mes.zip
  → Después del zip los archivos .log originales se eliminan

Mes en curso
  → Los archivos diarios del mes en curso se conservan como .log individuales
  → No se comprimen hasta que el mes cierre
```

El proceso de archivado y purga lo ejecuta el propio binario al arrancar, como tarea de mantenimiento antes de iniciar su loop principal.

---

## usqay-print-client — logs locales

Los logs se escriben en `logs/` dentro del directorio donde reside el ejecutable.

```text
usqay-print-client/
├── usqay-print-client.exe
├── agent.db
└── logs/
    ├── 2026-06-08.log
    └── archive/
        └── 2026/
            └── mayo/
                └── 2026-mayo.zip
```

Eventos que se registran:

```text
[INFO]  WebSocket conectado: wss://servidor:puerto
[INFO]  Registrado como terminal: caja-01
[INFO]  Config recibida: 2 impresoras, 3 rutas
[INFO]  Job recibido: job_id=123 tipo=comprobante
[INFO]  Job guardado en SQLite: job_id=123
[INFO]  Imprimiendo: job_id=123 impresora="Epson TM-T20 Receipt" tipo=sistema
[INFO]  Impresión exitosa: job_id=123 duración=212ms
[INFO]  Confirmación enviada al servidor: job_id=123
[WARN]  WebSocket desconectado — reintentando en 2s
[WARN]  Impresora no disponible: "Epson TM-T20 Receipt"
[ERROR] Fallo de impresión: job_id=124 error="acceso denegado al spooler"
[ERROR] No se pudo conectar a 192.168.1.100:9100 — timeout
```

El cliente nunca envía sus logs al servidor.

Los logs viven únicamente en la máquina local del restaurante.

---

## usqay-print-server — logs del servidor

Los logs se escriben en `logs/` dentro del directorio de trabajo del servidor.

```text
usqay-print-server/
├── usqay-print-server
└── logs/
    ├── 2026-06-08.log
    └── archive/
        └── 2026/
            └── mayo/
                └── 2026-mayo.zip
```

Eventos que se registran:

```text
[INFO]  WebSocket server iniciado en :8080
[INFO]  Terminal conectado: terminal_id=caja-01 business_id=empresa-001
[INFO]  Token validado: agente_id=3
[INFO]  Config enviada: terminal_id=caja-01
[INFO]  Job creado: job_id=123 agente_id=3 tipo=comprobante
[INFO]  Job enviado por WebSocket: job_id=123
[INFO]  ACK recibido: job_id=123 estado=received
[INFO]  Confirmación de impresión: job_id=123 estado=impreso
[INFO]  Sync recibido: terminal_id=caja-01 printed_jobs=[123,124,125]
[WARN]  Terminal desconectado: terminal_id=caja-01
[WARN]  Token inválido — conexión rechazada
[ERROR] No se pudo entregar job_id=125 — terminal desconectado
```

---

## Tabla `logs_cliente` — opcional para el PoC

Si se quiere visibilidad remota de errores del cliente sin acceso físico a la PC, el cliente puede enviar eventos críticos al servidor vía WebSocket:

```json
{
  "type": "log",
  "level": "error",
  "job_id": 124,
  "msg": "fallo de impresión: acceso denegado al spooler",
  "ts": "2026-06-08T14:32:01Z"
}
```

El servidor los almacena opcionalmente en:

```sql
CREATE TABLE logs_cliente (
  id           INT PRIMARY KEY AUTO_INCREMENT,
  agente_id    INT NOT NULL REFERENCES agentes_impresion(id),
  job_id       INT NULL,
  level        ENUM('info','warn','error') NOT NULL,
  msg          TEXT NOT NULL,
  ts           TIMESTAMP NOT NULL,
  created_at   TIMESTAMP DEFAULT NOW()
);
```

Esto permite al equipo técnico diagnosticar problemas remotamente sin tener acceso físico a la PC del restaurante.

---

# Referencia para comparar si el Try falla

Si la arquitectura Go + WebSocket + SQLite tiene bloqueos no previstos, la alternativa más madura es **QZ Tray**:

| | Go + WebSocket (este Try) | QZ Tray |
|---|---|---|
| Conexión | WebSocket persistente | WebSocket con cert autofirmado |
| Impresoras | Spooler / TCP / serie | USB, IP, puerto serie, OS |
| Persistencia | SQLite local | sin cola propia |
| Dependencias | ninguna (binario único) | Java instalado en cada terminal |
| Control | total | limitado a su API |
| Licencia | propio | open source |

QZ Tray resuelve el problema de Mixed Content (HTTPS → localhost) que afecta a la arquitectura HTTP-agent.

En la arquitectura WebSocket, ese problema no existe porque la conexión la inicia el cliente Go hacia el servidor, no el browser hacia localhost.

**No descartar QZ Tray hasta tener el Try de Go corriendo con impresión real confirmada.**

---

# Entregable esperado

Cada build debe generar:

```text
build/

usqay-print-server.exe
usqay-print-client.exe
```

Prueba exitosa:

```text
Servidor inicia
       ↓
Cliente conecta
       ↓
Servidor envía trabajo
       ↓
Cliente guarda en SQLite
       ↓
Cliente imprime
       ↓
Cliente confirma impresión
       ↓
Servidor recibe confirmación
```

Si este flujo funciona de extremo a extremo, queda demostrada la viabilidad técnica de una arquitectura basada en Go + WebSocket + SQLite local para impresión térmica distribuida.
