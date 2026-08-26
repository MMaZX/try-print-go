---
stepsCompleted: ['validate_prerequisites']
inputDocuments: ['_bmad-output/specs/spec-motor-impresion/SPEC.md', '_bmad-output/specs/spec-motor-impresion/roadmap.md', '_bmad-output/specs/spec-motor-impresion/device-profile.md', '_bmad-output/specs/spec-motor-impresion/backend-changes.md', '_bmad-output/specs/spec-motor-impresion/escpos-reference.md']
---

# try-print-go - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for try-print-go, decomposing the requirements from the printer redesign specification (`spec-motor-impresion`) and backend integration rules into implementable stories.

---

## Requirements Inventory

### Functional Requirements

- **FR-1:** La suite de pruebas golden debe capturar y validar los bytes exactos emitidos por el renderer actual en una matriz de combinaciones de `{58mm, 80mm, custom width} x {con/sin padding} x {texto, tabla con merge, columns, qr, barcode}`.
- **FR-2:** El cliente debe capturar los bytes exactos enviados a la impresora física en un archivo `.prn` si una bandera de depuración/configuración está activa.
- **FR-3:** El servidor debe persistir y gestionar perfiles de impresora declarativos (`DeviceProfile`) que incluyan: `width_dots`, `dpi`, `char_width_dots`, `supports_cut`, `supports_drawer`, `supports_qr_native`, `supports_print_area`, `supports_raster`.
- **FR-4:** El servidor WebSocket debe notificar a los clientes mediante el evento `printer_config_updated` cuando se cree, actualice o elimine un perfil de impresora en el sistema.
- **FR-5:** El cliente debe implementar un motor de cascada para resolver el perfil de impresora activo:
  1. Perfil recibido del servidor (persistido en SQLite).
  2. Perfil derivado por interpolación lineal a partir de anchos nominales (ej. 58mm/80mm).
  3. Perfil fallback por defecto (58mm completo).
- **FR-6:** El cliente debe aplicar perfiles en caliente sin requerir reinicio tras una actualización de WebSocket.
- **FR-7:** El motor de impresión del cliente debe medir y posicionar los bloques en una representación intermedia (IR) en dos etapas (`measure` y `arrange`) antes de emitir bytes ESC/POS.
- **FR-8:** El cliente debe centrar físicamente los tickets y aplicar márgenes usando comandos nativos ESC/POS (`GS L` y `GS W`) basados en las especificaciones del perfi, eliminando rellenos manuales por espacios en blanco.
- **FR-9:** El cliente debe soportar la impresión de imágenes monocromáticas selectivas mediante el comando `GS v 0` a partir de un nuevo bloque `type: "image"` en el payload JSON.
- **FR-10:** El servidor debe soportar el envío de imágenes en Base64 en el cuerpo del payload de impresión.

### Non-Functional Requirements

- **NFR-1:** **CGO-free**: Todo el pipeline y las dependencias (incluyendo el renderer raster y SQLite) en el cliente deben ser Go puro. No se permite introducir compilaciones CGO.
- **NFR-2:** **Tamaño del binario**: El tamaño del binario del cliente no debe crecer más de 3MB con las nuevas dependencias (`fogleman/gg` y `golang.org/x/image`).
- **NFR-3:** **Aislamiento por SO**: La lógica del spooler de impresión debe permanecer estrictamente aislada por build tags (`printer_windows.go` y `printer_linux.go`).
- **NFR-4:** **Compatibilidad del payload**: El contrato JSON `PrintPayload` sólo se extiende de forma retrocompatible. Todo payload preexistente debe seguir imprimiendo sin cambios en el servidor.
- **NFR-5:** **Emisión nativa por defecto**: Se debe utilizar texto nativo ESC/POS por defecto; el render rasterizado debe aplicarse exclusivamente a nivel de bloque (ej. bloques `type: "image"`). No se debe rasterizar el ticket completo.
- **NFR-6:** **Resiliencia de hardware**: Las características de hardware (corte, cajón, QR nativo, margen `GS L`) deben condicionarse a la disponibilidad declarada en el perfil, con fallbacks definidos para impresoras más sencillas.

### Additional Requirements

- **AD-1 (SQLite Caching):** El cliente debe almacenar en su SQLite local el último perfil configurado por impresora para recuperarlo rápidamente en el inicio sin depender de la conexión WebSocket.
- **AD-2 (Unidad de Medida ESC/POS):** La conversión de milímetros a dots debe usar la unidad de movimiento de 203 DPI (`GS P`) documentada por Epson para térmicas genéricas.

---

### FR Coverage Map

- **Epic 1 (Suite Golden & Captura):** FR-1, FR-2, NFR-3
- **Epic 2 (Perfiles de Impresora):** FR-3, FR-4, FR-5, FR-6, NFR-4, AD-1
- **Epic 3 (Motor de Geometría & IR):** FR-7, FR-8, AD-2
- **Epic 4 (Renderizado Raster & Logos):** FR-9, FR-10, NFR-1, NFR-2, NFR-5, NFR-6

---

## Epic List

- **Epic 1:** Suite Golden & Captura de Bytes (Fase 0)
- **Epic 2:** Perfiles de Dispositivo & Sincronización en Caliente (Fase 1)
- **Epic 3:** Motor de Geometría & Representación Intermedia (Fase 2)
- **Epic 4:** Renderizado Raster & Bloque de Imagen (Fase 3)

---

## Epic 1: Suite Golden & Captura de Bytes (Fase 0)

Establecer las bases de pruebas automatizadas y asegurar que podamos comparar la salida de impresión byte a byte con el comportamiento original del renderer.

### Story 1.1: Captura de bytes crudos a archivos `.prn`
**Como** desarrollador de USQAY Print,  
**Quiero** que el cliente guarde opcionalmente una copia exacta de los bytes enviados al spooler a un archivo `.prn` local,  
**Para** verificar y comparar las salidas físicas generadas sin necesidad de contar con hardware de impresión térmica real.

**Criterios de Aceptación:**
- **Given** el flag `capture_prn` está configurado en `true` en el archivo `config.json` del cliente.
- **When** se procesa un trabajo de impresión a través del worker.
- **Then** el cliente crea un archivo `<job_id>.prn` en un directorio de depuración con el contenido de bytes idéntico a lo que se envía al driver físico.
- **And** si el flag está en `false` o no existe en la configuración, no se genera ningún archivo `.prn`.

### Story 1.2: Suite Golden de pruebas para validación regresiva
**Como** desarrollador de USQAY Print,  
**Quiero** una suite golden de pruebas automatizadas que valide la salida en bytes de diversos payloads del renderer actual,  
**Para** garantizar que cualquier refactorización del motor de geometría conserve la equivalencia byte a byte y evite regresiones imprevistas.

**Criterios de Aceptación:**
- **Given** una matriz de payloads de prueba que cubre `{58mm, 80mm, custom width} x {con/sin padding} x {texto, tabla con merge, columns, qr, barcode}`.
- **When** se ejecutan las pruebas golden mediante `go test ./...`.
- **Then** el sistema compara la salida actual contra un conjunto de archivos golden versionados en el repositorio.
- **And** el test falla explícitamente ante cualquier diferencia de bytes en esta primera fase.

---

## Epic 2: Perfiles de Dispositivo & Sincronización en Caliente (Fase 1)

Introducir perfiles declarativos en el backend y el cliente, con sincronización de perfiles en caliente vía WebSockets y almacenamiento en SQLite.

### Story 2.1: Modelo de datos y API para Perfiles de Impresora
**Como** administrador del restaurante,  
**Quiero** configurar las especificaciones y capacidades declarativas de cada impresora desde la base de datos del servidor,  
**Para** que el cliente adapte dinámicamente sus salidas a la capacidad de cada máquina sin código duro.

**Criterios de Aceptación:**
- **Given** el modelo de datos de la base de datos del servidor.
- **When** se crea o edita una impresora.
- **Then** se pueden definir los campos del `DeviceProfile`: `width_dots`, `dpi`, `char_width_dots`, `supports_cut`, `supports_drawer`, `supports_qr_native`, `supports_print_area`, `supports_raster`.
- **And** los perfiles por defecto para anchos de `58mm` y `80mm` se inicializan automáticamente con sus anclas Epson conocidas.

### Story 2.2: Evento WebSocket para actualización de perfiles
**Como** cliente de USQAY Print,  
**Quiero** recibir un mensaje de actualización de perfil desde el servidor cuando ocurra un cambio en el backend,  
**Para** refrescar inmediatamente la caché local sin necesidad de realizar polling manual.

**Criterios de Aceptación:**
- **Given** un cliente conectado por WebSocket.
- **When** una impresora cambia su configuración en el servidor.
- **Then** el servidor emite el mensaje `{ "type": "printer_config_updated", "printer_id": "...", "profile": {...} }`.
- **And** el cliente procesa el mensaje de actualización y actualiza el registro en la base de datos local SQLite.

### Story 2.3: Cascada de resolución del perfil de dispositivo
**Como** motor de renderizado de USQAY Print,  
**Quiero** resolver las propiedades geométricas del dispositivo utilizando una estructura en cascada,  
**Para** garantizar compatibilidad retroactiva y fallbacks seguros si no hay comunicación con el servidor.

**Criterios de Aceptación:**
- **Given** la necesidad de imprimir un ticket.
- **When** se calcula la geometría de impresión.
- **Then** se aplica el siguiente orden de resolución:
  1. Perfil del servidor almacenado en SQLite.
  2. Perfil derivado mediante interpolación lineal de anchos nominales provenientes del payload (58mm/80mm).
  3. Fallback por defecto correspondiente a un perfil conservador de 58mm.
- **And** en esta fase la equivalencia en bytes para anchos estándar de 58mm y 80mm permanece 100% idéntica al renderer anterior (verificable con la suite Golden).

---

## Epic 3: Motor de Geometría & Representación Intermedia (Fase 2)

Refactorizar el motor de layout para usar una representación intermedia con un paso de medición y posicionamiento antes de imprimir, y aplicar encuadre nativo.

### Story 3.1: Motor de Layout de dos pasadas (Measure -> Arrange)
**Como** motor de renderizado de USQAY Print,  
**Quiero** calcular la geometría de los bloques en dos fases diferenciadas antes de generar comandos ESC/POS,  
**Para** evitar desbordes y calcular paddings de forma precisa y centralizada.

**Criterios de Aceptación:**
- **Given** un payload de impresión estructurado.
- **When** se ejecuta el renderizador.
- **Then** el motor calcula primero el tamaño de cada bloque (`measure`) restringido por el ancho máximo (`maxWidthDots`), y luego los posiciona (`arrange`).
- **And** el padding se calcula en un único punto del motor de layout, eliminando offsets manuales dispersos.

### Story 3.2: Encuadre nativo mediante comandos `GS L` y `GS W`
**Como** cliente de USQAY Print,  
**Quiero** configurar el margen y el ancho del área de impresión usando comandos nativos de la impresora térmica,  
**Para** centrar el contenido físicamente sin rellenar líneas con caracteres de espacio manuales.

**Criterios de Aceptación:**
- **Given** un ticket que tiene configurado un ancho menor al papel físico en el perfil.
- **When** se compilan los bytes ESC/POS.
- **Then** los bytes resultantes contienen los comandos `GS L nL nH` (para el margen izquierdo) y `GS W nL nH` (para el ancho del área), calculados de forma dinámica.
- **And** el ticket se imprime centrado en el visualizador ESC/POS, sin espacios manuales.

---

## Epic 4: Renderizado Raster & Bloque de Imagen (Fase 3)

Implementar el soporte de bloques de imagen de tipo rasterizado monocromo selectivo utilizando gg y x/image.

### Story 4.1: Bloque `type: "image"` en el payload JSON
**Como** servidor de USQAY Print,  
**Quiero** enviar un bloque de imagen codificado en Base64 dentro del cuerpo de la comanda/ticket,  
**Para** incluir logotipos u otros gráficos directamente en el ticket sin que el cliente requiera descargar recursos externos.

**Criterios de Aceptación:**
- **Given** un ticket con logotipo.
- **When** el servidor construye el payload.
- **Then** incluye un bloque con el formato: `{ "type": "image", "data": "<base64_png>", "align": "center", "width": 300 }`.
- **And** el cliente puede deserializar correctamente el payload extendido.

### Story 4.2: Renderer Raster monocromático selectivo con `GS v 0`
**Como** cliente de USQAY Print,  
**Quiero** renderizar las imágenes y bloques complejos que excedan la celda normal de caracteres a un formato monocromo binario,  
**Para** enviar el logotipo al cabezal térmico utilizando el comando `GS v 0` y mantener el resto del ticket en texto nativo.

**Criterios de Aceptación:**
- **Given** un bloque `type: "image"` en el payload.
- **When** el renderer procesa la imagen.
- **Then** la escala al ancho configurado, la binariza con un threshold de ~50% (sin dithering ruidoso) y emite un comando `GS v 0 m xL xH yL yH d1...dk` empaquetado correctamente (MSB-first).
- **And** los tickets que no contienen imágenes siguen imprimiéndose exactamente igual que antes, sin cambios en sus bytes de texto nativo.
- **And** el binario no incrementa su tamaño total en disco más de 3MB.
