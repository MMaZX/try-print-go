---
stepsCompleted: ['validate_prerequisites', 'design_epics', 'create_stories', 'final_validation']
inputDocuments: ['/home/fulanito/Downloads/poc-impresion-termica.md', 'usqay-print-client/internal/ws/connection.go', 'usqay-print-client/internal/queue/worker.go', 'usqay-print-client/internal/queue/repository.go', 'usqay-print-client/internal/queue/sqlite.go', 'usqay-print-server/internal/ws/hub.go', 'usqay-print-server/internal/ws/client.go']
---

# try-print-go - Epic Breakdown (Resiliencia Offline)

## Overview

Este documento cubre la parte del POC original (`poc-impresion-termica.md`) referida a operación sin conexión que aún NO está cubierta por el código actual. No es un rediseño: el flujo base de reconexión, heartbeat, kick de conexión duplicada, cola SQLite, `sync` al reconectar y expiración de jobs por `expira_en` ya están implementados en `usqay-print-client/internal/ws`, `usqay-print-client/internal/queue` y `usqay-print-server/internal/ws`. Este breakdown cierra las tres brechas identificadas contra el POC, para ejecutar en 3 días.

## Requirements Inventory

### Functional Requirements

- **FR-1:** El cliente debe persistir en SQLite local la configuración de impresoras y rutas recibida del servidor (mensaje `type: "config"`), para poder resolver qué impresora usar al arrancar sin conexión a internet.
- **FR-2:** Al arrancar sin WebSocket disponible, el cliente debe cargar la última configuración cacheada desde SQLite y operar con ella hasta que la conexión se restablezca.
- **FR-3:** El worker debe llevar un contador `intentos` por trabajo en la tabla `print_jobs`, incrementándolo en cada fallo de impresión (ej. impresora sin papel).
- **FR-4:** Al superar un máximo de reintentos configurable, el trabajo debe marcarse como `ERROR` definitivo y notificarse al servidor, sin bloquear el procesamiento de los demás trabajos en cola.
- **FR-5:** Debe existir una prueba automatizada de extremo a extremo que cubra: servidor envía job → cliente guarda `PENDING` en SQLite → se corta la conexión WebSocket → worker imprime localmente y pasa a `PRINTED` → la conexión vuelve → el cliente sincroniza (`type: "sync"`) → el servidor refleja el estado real en `cola_impresion`.

### Non-Functional Requirements

- **NFR-1:** La persistencia de configuración debe reutilizar `modernc.org/sqlite` (CGO-free), sin introducir nuevas dependencias de almacenamiento.
- **NFR-2:** El mecanismo de reintentos no debe bloquear el procesamiento FIFO de otros trabajos en la cola local.
- **NFR-3:** El número máximo de reintentos debe ser configurable (vía `config.json` o constante nombrada), no un valor mágico disperso en el código.

### Additional Requirements

- **AD-1:** Requiere una migración de esquema en `print_jobs` (columna `intentos`) y una tabla o fila nueva para la config cacheada (`agent_config` o similar) en `usqay-print-client/internal/queue/sqlite.go`.
- **AD-2:** Debe reutilizar el protocolo `sync` / `HandleSync` ya existente en `usqay-print-server/internal/ws/hub.go` — no crear un mensaje nuevo para esto.

### UX Design Requirements

N/A — no hay superficie de UI en este alcance (agente headless + servidor).

### FR Coverage Map

- **Epic 5 (Resiliencia Offline del Agente):** FR-1, FR-2, FR-3, FR-4, FR-5, NFR-1, NFR-2, NFR-3, AD-1, AD-2
  - Story 5.1 → FR-1, FR-2, NFR-1, AD-1
  - Story 5.2 → FR-3, FR-4, NFR-2, NFR-3, AD-1
  - Story 5.3 → FR-5, AD-2

---

## Epic List

### Epic 5: Resiliencia Offline del Agente
El agente sigue operando (resuelve impresoras, imprime y no pierde trabajos) cuando no hay conexión a internet, y recupera el estado real de forma confiable al reconectar.
**FRs covered:** FR-1, FR-2, FR-3, FR-4, FR-5

---

## Epic 5: Resiliencia Offline del Agente

El agente sigue operando (resuelve impresoras, imprime y no pierde trabajos) cuando no hay conexión a internet, y recupera el estado real de forma confiable al reconectar.

### Story 5.1: Configuración cacheada para arranque offline

Como agente de impresión (usqay-print-client),
Quiero persistir en SQLite local la última configuración de impresoras y rutas recibida del servidor, y cargarla al arrancar si no hay conexión,
Para que el restaurante pueda seguir imprimiendo aunque el agente arranque sin internet.

**Criterios de Aceptación:**

**Given** el cliente recibe un mensaje `type: "config"` válido del servidor
**When** `applyConfig()` procesa el mensaje
**Then** la lista de impresoras y su ruteo se persiste en SQLite (nueva tabla/registro, ej. `agent_config`), reemplazando la versión anterior

**Given** el cliente arranca sin poder establecer conexión WebSocket
**When** se inicializa el worker
**Then** el cliente carga la última configuración persistida en SQLite y la usa para resolver impresoras
**And** la persistencia usa `modernc.org/sqlite` (sin CGO), sin nuevas dependencias (NFR-1)

**Given** no existe ninguna configuración previa persistida (primer arranque, nunca llegó a conectar)
**When** el cliente intenta arrancar sin conexión
**Then** registra un error explícito en el log indicando que no hay configuración disponible y no puede operar, consistente con la regla del proyecto de no usar valores por defecto silenciosos

### Story 5.2: Reintentos controlados en fallos de impresión

Como worker local de impresión,
Quiero reintentar un trabajo fallido un número máximo de veces antes de marcarlo como ERROR definitivo,
Para que errores transitorios (papel atascado, impresora ocupada) no pierdan el trabajo a la primera, pero tampoco bloqueen la cola indefinidamente.

**Criterios de Aceptación:**

**Given** un job falla al imprimir (ej. error de spooler o timeout)
**When** el worker captura el error
**Then** incrementa la columna `intentos` en `print_jobs` (migración de esquema) y deja el job en un estado reintentable

**Given** un job alcanza el máximo de reintentos configurado (constante nombrada o campo en `config.json`, NFR-3)
**When** el worker evalúa el siguiente intento
**Then** marca el job como `ERROR` definitivo, registra `error_msg`, y notifica `{ type: "error", job_id, msg }` al servidor

**Given** un job en estado reintentable
**When** el worker vuelve a procesarlo
**Then** aplica un backoff entre reintentos, evitando un loop apretado que sature CPU o golpee repetidamente una impresora atascada
**And** los demás jobs `PENDING` de la cola se siguen procesando con normalidad mientras un job reintenta (NFR-2)

### Story 5.3: Test end-to-end del escenario caída/reconexión

Como desarrollador de USQAY Print,
Quiero una prueba automatizada que reproduzca el flujo completo de caída y recuperación de conexión,
Para demostrar que el sistema no pierde trabajos y sincroniza el estado real, tal como exige el POC.

**Criterios de Aceptación:**

**Given** un servidor y un cliente conectados (o mockeados a nivel de WebSocket)
**When** el servidor envía un job de impresión y luego se simula la caída de la conexión
**Then** el cliente ya guardó el job en SQLite como `PENDING` antes del corte

**Given** la conexión está caída
**When** el worker local procesa el job
**Then** el job pasa a `PRINTED` en SQLite local sin necesidad de conexión activa

**Given** la conexión se restablece
**When** el cliente reconecta y envía `type: "sync"` (protocolo existente, AD-2)
**Then** el servidor actualiza `cola_impresion` reflejando el estado `impreso` real, verificado por el test

**And** el test corre de forma aislada vía `go test ./...`, sin depender de hardware físico de impresión — usa un registry de impresoras fake/mock, consistente con la regla del proyecto de que toda prueba con impresión física se hace en Windows, nunca en Linux

