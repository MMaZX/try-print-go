# POC — Impresión Térmica (Try de Guillermo)

**Asignado a:** Guillermo  
**Tipo:** Proof of Concept / Investigación técnica  
**Fecha:** 2026-06-05  
**Estado:** backlog → in-progress al iniciar

---

## Contexto del problema

Usqay Web es una aplicación Next.js que corre en web, móvil y desktop. Necesita imprimir a impresoras térmicas (USB y por IP) los siguientes documentos:

| Documento | Destino | Frecuencia |
|-----------|---------|------------|
| Comanda | Impresora cocina/barra | Alta (por cada pedido) |
| Precuenta / ticket | Impresora caja | Media |
| Comprobante (boleta/factura) | Impresora caja | Alta |
| Cierre de caja | Impresora caja | Baja (1 por turno) |
| Reportes | Impresora caja o PDF | Baja |

---

## El problema central: web no puede imprimir a térmica directamente

Un browser web **no puede acceder a puertos USB ni a TCP:9100** (puerto ESC/POS estándar) por restricciones de seguridad. Hay dos familias de solución:

### Familia A — Agente local (recomendada para USB + IP)

Un proceso pequeño corre en la máquina del POS. La app web le habla por WebSocket o HTTP a `localhost`.

```
[App Web / Next.js]
        |
        | HTTP POST / WebSocket
        v
[Agente local en la máquina POS]
        |              |
        v              v
  [USB printer]  [IP printer :9100]
```

### Familia B — Directo a impresora IP desde backend

Para impresoras de red, el servidor Next.js (o un microservicio) conecta directamente al IP:9100 de la impresora y envía ESC/POS. No requiere agente. **No sirve para USB.**

---

## Problema adicional: HTTPS → HTTP = Mixed Content

Si la app está en HTTPS (producción), el browser **bloquea** llamadas a `http://localhost`. Soluciones:

1. El agente expone HTTPS en localhost con certificado autofirmado (el usuario acepta una vez)
2. El agente usa WebSocket (`wss://localhost`) con certificado autofirmado
3. En desktop (Electron) esto no aplica — Electron tiene acceso nativo

---

## Por qué Go es buena opción para el agente

- Binario único, sin dependencias (sin JVM, sin Node, sin Python)
- Fácil de distribuir: un `.exe` en Windows, binario en Linux/Mac
- Bajo consumo de memoria (20-40 MB)
- Manejo nativo de TCP/IP para impresoras de red
- Puerto serie (USB como COM/ttyUSB) manejable con librería `go.bug.st/serial`
- Alternativa principal a QZ Tray sin depender de Java

---

## Corrección arquitectónica — el backend genera el job, no el frontend

El frontend **no llama** al agente Go directamente. El flujo correcto es:

```
[Frontend] → POST /api/print  → [Backend Next.js / Laravel]
                                         |
                                  crea job en BD
                                  (cola_impresion)
                                         |
                              [Agente Go en PC cliente]
                              ← conecta al backend y recibe job
                                         |
                                  [Impresora USB o IP]
```

El backend está en la nube. El agente está en la PC del restaurante.
El backend **no puede** hacer `POST http://localhost:8765` — ese localhost es la nube, no el cliente.
La solución: **el agente invierte la conexión** — él conecta al backend, no al revés.

---

## Dos mecanismos para el Try — Guillermo debe evaluar ambos

### Mecanismo A — Cola en BD + Polling (más simple, recomendado para empezar)

```
[Backend] → INSERT cola_impresion (estado=pendiente)
[Agente]  → GET /api/v1/print-agent/jobs/pending  (cada 1-2 segundos)
[Agente]  → imprime
[Agente]  → PATCH /api/v1/print-agent/jobs/{id}  (estado=impreso)
```

Ventajas: simple, sin dependencias extra, jobs persisten aunque el agente se desconecte momentáneamente
Desventaja: latencia de hasta 2 segundos entre acción y impresión

### Mecanismo B — WebSocket persistente (más rápido, más complejo)

```
[Agente]  → conecta wss://app.usqay.com/ws/print-agent (auth con token)
[Backend] → push { tipo: "comanda", payload: {...} }  cuando ocurre evento
[Agente]  → recibe, imprime, responde { ok: true }
```

Ventajas: near-instant (< 200ms), ideal para comandas
Desventaja: requiere manejar reconexión, heartbeat, estado del socket

### Recomendación para el Try

**Empezar con Mecanismo A (cola + polling).** Si la latencia de 1-2 segundos es aceptable para el negocio (muy probable), no se necesita WebSocket. Si en producción se detecta que la latencia molesta en comandas, se agrega WebSocket como capa encima de la cola.

---

## Arquitectura del repositorio Go

```
usqay-print-agent/
├── main.go
├── agent/
│   ├── poller.go           ← polling de jobs pendientes cada N segundos
│   └── worker.go           ← procesa cada job: construye ESC/POS → imprime
├── printer/
│   ├── usb.go              ← USB vía puerto serie
│   ├── network.go          ← TCP directo a IP:9100
│   └── escpos/
│       └── builder.go      ← construcción de comandos ESC/POS
├── api/
│   └── client.go           ← HTTP client que habla con el backend Usqay
└── config/
    └── config.go           ← token, URL del backend, cache de config
```

El agente **no expone ningún puerto HTTP**. Solo consume la API del backend.

---

## Librerías Go a evaluar en el Try

| Librería | Para qué | Repo |
|----------|----------|------|
| `kenshaw/escpos` | Comandos ESC/POS (texto, corte, QR, logo) | github.com/kenshaw/escpos |
| `go.bug.st/serial` | Puerto serie (USB como COM/ttyUSB) | go.bug.st/serial |
| `net` stdlib | TCP directo a IP:9100 | stdlib de Go |
| `rs/cors` | CORS para el servidor HTTP | github.com/rs/cors |

---

## Scope del Try (PoC)

El objetivo **no** es producción. Es responder estas preguntas con código real:

### Criterios de éxito del PoC

- [ ] **1. Imprimir texto a impresora IP** — Go conecta a `192.168.x.x:9100` y envía ESC/POS con texto simple
- [ ] **2. Imprimir texto a impresora USB** — Go detecta el puerto (COM3 en Windows / /dev/ttyUSB0 en Linux) y envía ESC/POS
- [ ] **3. API HTTP funciona desde el browser** — `fetch('http://localhost:8765/print', ...)` dispara impresión real sin CORS error
- [ ] **4. Probar al menos un template real** — comanda con: número de mesa, items, cantidades, hora
- [ ] **5. Documenta los problemas encontrados** — Mixed Content, detección de puertos, permisos OS

### Fuera del scope del Try

- Templates finales de todos los documentos
- Autenticación del agente
- Instalación/autostart como servicio
- Firma digital en comprobantes (eso es SUNAT, no impresión)
- Mobile (evaluar en sprint siguiente si el agente resuelve web+desktop primero)

---

## Preguntas que el Try debe responder

1. ¿El browser permite `fetch` a `http://localhost` desde app en HTTPS sin configuración extra?
2. ¿Podemos detectar impresoras USB automáticamente o el usuario configura manualmente el puerto?
3. ¿La latencia Go → impresora es aceptable para comandas (objetivo: < 500ms)?
4. ¿Necesitamos certificado SSL en el agente para entornos HTTPS?
5. ¿Una sola librería ESC/POS cubre lo que necesitamos (logo, QR, corte, negrita, alineación)?

---

## Entregable esperado

1. Repositorio `usqay-print-agent` con el PoC funcionando
2. Un `.md` con hallazgos: qué funcionó, qué no, qué decisiones tomar
3. Decisión sobre si seguir con Go + agente propio o evaluar alternativa (QZ Tray)
4. Schema JSON definitivo del endpoint `/print` para que el frontend lo adopte

---

## Base de datos — qué tenemos y qué falta

### Lo que ya existe en el backend

El frontend ya consume dos tablas relacionadas con impresión:

**`impresoras`** (existe, Epic 11-3 Arturo)
```
id | descripcion | tipo (USB|Wifi|Otros) | ip | observaciones | estado
```

**`terminales`** (existe, Epic 11-3 Arturo)
```
id | descripcion | sucursal_id | estado
```

### Lo que falta para que el agente Go funcione

El agente necesita leer su configuración desde Usqay Web. Con lo que hay hoy **no alcanza**. Faltan estos campos y tablas:

---

#### Campos a agregar en `impresoras`

```sql
-- Falta para USB
puerto_serie    VARCHAR(30)   NULL   -- "COM3" en Windows, "/dev/ttyUSB0" en Linux

-- Falta para IP
puerto_tcp      INT           DEFAULT 9100   -- casi siempre 9100

-- Falta para ESC/POS
ancho_papel     ENUM('58','80')  DEFAULT '80'  -- mm, determina el ancho de impresión

-- Falta para trazabilidad del agente
terminal_id     INT           NULL  FK → terminales(id)
```

---

#### Tabla nueva: `impresora_documentos`

Mapea qué impresora imprime qué tipo de documento por sucursal.

```sql
CREATE TABLE impresora_documentos (
  id              INT PRIMARY KEY AUTO_INCREMENT,
  sucursal_id     INT NOT NULL,
  tipo_documento  ENUM('comanda','precuenta','comprobante','cierre','reporte') NOT NULL,
  impresora_id    INT NOT NULL FK → impresoras(id),
  copias          TINYINT DEFAULT 1,
  activo          BOOLEAN DEFAULT true,
  created_at      TIMESTAMP,
  updated_at      TIMESTAMP,
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

#### Tabla nueva: `agentes_impresion`

Registra cada binario Go instalado en cada PC del restaurante.

```sql
CREATE TABLE agentes_impresion (
  id              INT PRIMARY KEY AUTO_INCREMENT,
  terminal_id     INT NOT NULL FK → terminales(id),
  token           VARCHAR(64) NOT NULL UNIQUE,  -- el agente se autentica con esto
  version         VARCHAR(20),                  -- versión del binario instalado
  ultimo_ping     TIMESTAMP NULL,               -- heartbeat del agente
  activo          BOOLEAN DEFAULT true,
  created_at      TIMESTAMP,
  updated_at      TIMESTAMP
);
```

---

#### Tabla nueva: `cola_impresion`

El corazón del sistema. El backend inserta aquí, el agente consume de aquí.

```sql
CREATE TABLE cola_impresion (
  id              INT PRIMARY KEY AUTO_INCREMENT,
  agente_id       INT NOT NULL FK → agentes_impresion(id),
  tipo_documento  ENUM('comanda','precuenta','comprobante','cierre','reporte') NOT NULL,
  payload         JSON NOT NULL,       -- datos del documento a imprimir
  estado          ENUM('pendiente','procesando','impreso','error') DEFAULT 'pendiente',
  intentos        TINYINT DEFAULT 0,
  error_msg       TEXT NULL,
  created_at      TIMESTAMP DEFAULT NOW(),
  procesado_at    TIMESTAMP NULL
);
```

---

### Endpoint que el agente Go consume al arrancar

```
GET /api/v1/print-agent/config
Authorization: Bearer {token}   ← token del agente en agentes_impresion

Response:
{
  "sucursal": {
    "id": 1,
    "nombre": "Restaurante Centro",
    "ruc": "20123456789",
    "direccion": "Av. Lima 123",
    "logo_base64": "..."          ← para imprimir logo en header
  },
  "impresoras": [
    {
      "id": 1,
      "nombre": "Caja",
      "tipo": "USB",
      "puerto_serie": "COM3",
      "ancho_papel": "80"
    },
    {
      "id": 2,
      "nombre": "Cocina",
      "tipo": "Wifi",
      "ip": "192.168.1.100",
      "puerto_tcp": 9100,
      "ancho_papel": "58"
    }
  ],
  "ruteo": {
    "comanda":     { "impresora_id": 2, "copias": 1 },
    "precuenta":   { "impresora_id": 1, "copias": 1 },
    "comprobante": { "impresora_id": 1, "copias": 2 },
    "cierre":      { "impresora_id": 1, "copias": 1 },
    "reporte":     { "impresora_id": 1, "copias": 1 }
  }
}
```

El agente cachea esta config en `config.json` local para sobrevivir sin internet momentáneamente.

---

## Flujo completo del sistema de impresión

```
[Administrador configura en Usqay Web]
  → Registra impresoras (tipo, IP o puerto USB, ancho papel)
  → Asigna qué impresora imprime cada documento (impresora_documentos)
  → Registra el terminal → sistema genera token del agente (agentes_impresion)

[Técnico instala el agente en la PC del restaurante]
  → Descarga usqay-print-agent.exe
  → Ejecuta: usqay-print-agent.exe --token=ABC123 --url=https://app.usqay.com
  → El agente llama GET /api/v1/print-agent/config
  → Descarga config (impresoras, ruteo, datos empresa) → guarda en cache local
  → Inicia ciclo de polling

[Flujo en tiempo real — cajero cobra un pedido]
  → Browser hace POST /api/print al backend Next.js
    { tipo: "comprobante", terminal_id: 3, payload: { mesa: 5, items: [...], total: 45.00 } }
  → Backend resuelve: terminal_id 3 → agente_id 1
  → Backend inserta en cola_impresion { agente_id: 1, tipo: "comprobante", payload: {...}, estado: "pendiente" }
  → Backend responde { ok: true } al browser inmediatamente (no espera la impresión)

  → Agente Go (en la PC) hace GET /api/v1/print-agent/jobs/pending (cada 1s)
  → Recibe el job pendiente
  → Marca estado = "procesando"
  → Busca en config local: comprobante → impresora_id 1 → USB → COM3
  → Construye ESC/POS con el payload
  → Envía bytes a COM3
  → Impresora imprime
  → Agente hace PATCH /api/v1/print-agent/jobs/{id} { estado: "impreso" }

[Flujo comanda — mozo envía pedido a cocina]
  → Backend inserta en cola_impresion { tipo: "comanda", ... }
  → Agente recibe en siguiente poll
  → Resuelve: comanda → impresora_id 2 → Wifi → 192.168.1.100:9100
  → Abre TCP socket → envía ESC/POS → impresora cocina imprime
  → Marca job como "impreso"
```

---

## Qué hace el agente Go — ciclo de vida

```
1. ARRANQUE
   ├── Lee token del argumento CLI o variable de entorno
   ├── Llama GET /api/v1/print-agent/config
   ├── Guarda config en cache local (config.json)
   └── Inicia HTTP server en localhost:8765

2. EN OPERACIÓN
   ├── Recibe POST /print
   ├── Valida tipo de documento
   ├── Resuelve impresora por ruteo
   ├── Construye template ESC/POS
   ├── Envía a impresora (USB o TCP)
   └── Responde { ok: true/false }

3. HEARTBEAT (cada 60s)
   └── POST /api/v1/print-agent/ping → actualiza ultimo_ping en BD

4. HOT-RELOAD DE CONFIG (cada 5 min o manual)
   └── GET /api/v1/print-agent/config → actualiza cache local
       (permite cambiar impresoras sin reinstalar el agente)
```

---

## Referencia para comparar si el Try falla

Si Go + agente propio tiene bloqueos no previstos, la alternativa más madura es **QZ Tray**:
- Usa WebSocket con certificado autofirmado (resuelve Mixed Content)
- Soporta USB, IP, puerto serie, impresoras del OS
- Gratis para uso básico, open source
- Contras: requiere Java instalado en cada terminal

**No descartar QZ Tray hasta no tener el Try de Go corriendo.**
