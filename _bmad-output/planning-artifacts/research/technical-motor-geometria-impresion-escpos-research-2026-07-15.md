---
stepsCompleted: [1, 2, 3, 4, 5, 6]
inputDocuments: []
workflowType: 'research'
lastStep: 1
research_type: 'technical'
research_topic: 'Rediseño del motor de geometría/impresión térmica ESC/POS del cliente USQAY Print: de anclas fijas 58mm/80mm a box model + capacidades de dispositivo (Go, Windows/Linux)'
research_goals: 'Validar contra el código actual los 7 enfoques identificados (box model de layout, escalado DPI/vectorial, DEVMODE/XFORM/DeviceCapabilities en Windows, CUPS/lpr en Linux, abstracción de capacidades de dispositivo, simulación RAW/LPD para depuración, texto vs dibujo vectorial) y producir un informe que alimente una spec con bmad-spec, respetando las reglas del proyecto (build tags por SO, modernc.org/sqlite, alexbrainman/printer solo Windows)'
user_name: 'Fulanito'
date: '2026-07-15'
web_research_enabled: true
source_verification: true
---

# Research Report: technical

**Date:** 2026-07-15
**Author:** Fulanito
**Research Type:** technical

---

## Research Overview

Esta investigación evalúa el rediseño del motor de geometría/impresión del cliente USQAY Print (Go, térmicas ESC/POS 58/80mm, Windows/Linux), partiendo de una investigación previa del usuario que proponía siete enfoques tomados de repositorios de referencia. El renderer actual calcula el ancho por interpolación lineal entre anclas fijas (58mm→384 dots, 80mm→576 dots) y resta paddings a mano, lo que produce desbordes de contenido y encuadre manual.

La metodología combinó verificación web multi-fuente (13 búsquedas, fuentes oficiales de Epson/Microsoft y proyectos de referencia de la industria) con contraste permanente contra el código real del cliente. Hallazgos rectores: DEVMODE/XFORM no aplican al flujo RAW de Windows (pista falsa de la investigación previa); HTML/CSS y PDF intermedio quedan descartados; el patrón convergente de la industria es JSON → box model local de dos pasadas → bytes RAW, con perfiles de dispositivo declarativos (patrón escpos-printer-db) en lugar de anclas.

El resultado son cuatro decisiones cerradas con recomendación única y un roadmap incremental de 4 fases + fase 0 de seguridad. El resumen ejecutivo completo está en la sección "Research Synthesis" al final del documento.

---

<!-- Content will be appended sequentially through research workflow steps -->

## Technical Research Scope Confirmation

**Research Topic:** Rediseño del motor de geometría/impresión térmica ESC/POS del cliente USQAY Print: de anclas fijas 58mm/80mm a box model + capacidades de dispositivo (Go, Windows/Linux)
**Research Goals:** Producir decisiones fuertes, funcionales y escalables (una recomendación única por decisión, no un menú) que alimenten una spec con bmad-spec, validando los enfoques identificados contra el código actual (`renderer.go`, `builder.go`, `printer_linux.go`).

**Premisa fija:** el payload JSON del servidor es la fuente de cada impresión. La investigación decide cómo renderizarlo con precisión física, no lo reemplaza por plantillas.

**Technical Research Scope:**

- Decisión 1 — Motor de layout: box model propio en Go vs raster/PDF intermedio con escalado por DPI (go-pdfium, fogleman/gg) vs HTML/CSS (candidato a descartar con argumentos; wkhtmltopdf descontinuado desde 2023)
- Decisión 2 — Vía de salida en Windows: RAW ESC/POS vs GDI con geometría nativa (DEVMODE, XFORM, DeviceCapabilities, AddCustomPaperSize)
- Decisión 3 — Vía de salida en Linux: RAW a dispositivo vs delegar a CUPS/lp con filtros y escalado del spooler
- Decisión 4 — Abstracción de capacidades de dispositivo: dejar anclas 58/80mm y consultar capacidades reales (patrón OpenPrinting/go-mfp), interfaz Go única con build tags por SO
- Verificación y depuración: simulador RAW/LPD (sim-printer) para validar bytes reales antes del hardware

**Research Methodology:**

- Current web data with rigorous source verification
- Multi-source validation for critical technical claims
- Confidence level framework for uncertain information
- Contraste permanente con el código actual del cliente USQAY Print
- Cada decisión cierra con una recomendación única y justificada

**Scope Confirmed:** 2026-07-15

---

## Technology Stack Analysis

### Estado actual del código (línea base)

El renderer actual (`usqay-print-client/internal/queue/renderer.go`) convierte mm→dots con interpolación lineal entre dos anclas documentadas por fabricantes: 58mm→384 dots y 80mm→576 dots a 203dpi (`paperGeometry`, renderer.go:533-558). El padding se resta manualmente (`padding × 8 dots/mm`) y el ancho en caracteres se deriva de `printWidthDots / 12` (fuente A). En Linux, `printer_linux.go` envía bytes RAW vía `lp -d <cola> -o raw` para térmicas, o delega a los filtros CUPS para inkjet/láser. Todo el layout es "texto en celdas de carácter": la alineación, columnas y tablas se resuelven con conteo de caracteres, que es la raíz de los desbordes y del encuadre manual.

### Lenguaje y runtime

Go puro sin CGO es una restricción dura del proyecto (binario único en Windows sin GCC/MinGW, regla de CLAUDE.md con `modernc.org/sqlite` como precedente). Cualquier candidato que exija CGO o binarios externos parte con desventaja estructural.

_Confianza: Alta (restricción propia del proyecto, verificada en código)._

### Candidatos de motor de layout

**HTML/CSS (go-wkhtmltopdf) — DESCARTADO.** wkhtmltopdf fue archivado en GitHub en enero de 2023; su último release (0.12.6) es de 2020; arrastra CVE-2022-35583 (SSRF, CVSS 9.8) sin parche; y su motor es un fork de Qt WebKit congelado ~2014 sin CSS Grid ni Flexbox moderno. Las alternativas vivas (Playwright/Puppeteer/Gotenberg) requieren un Chromium completo — inaceptable para un agente ligero de restaurante. Además el payload ya es JSON estructurado; introducir HTML como formato intermedio agrega una capa de traducción sin eliminar el problema de geometría física.
_Source: https://pdf4.dev/blog/wkhtmltopdf-alternatives-2026, https://www.htmltopdfconverter.com.au/blog/wkhtmltopdf-is-dead-alternatives-2026_
_Confianza: Alta (multi-fuente coincidente)._

**PDF intermedio (klippa-app/go-pdfium) — viable pero sobredimensionado.** go-pdfium tiene modo WebAssembly vía wazero (Go puro, sin CGO, PDFium embebido con go:embed, ~2x más lento que CGO nativo). Cumple la restricción de binario único, pero introduce un runtime WASM y un motor PDF completo solo para rasterizar tickets de texto — decenas de MB y complejidad para un problema que no requiere PDF.
_Source: https://github.com/klippa-app/go-pdfium_
_Confianza: Alta._

**Raster propio en Go puro (fogleman/gg + image/draw) — candidato fuerte para bloques gráficos.** `gg` es 2D puro Go sobre freetype/raster, con `DrawStringWrapped`, `MeasureString`, anclas de alineación y salida a `image.RGBA`. Un canvas de exactamente `printWidthDots` px de ancho elimina el desborde por construcción: lo que no cabe se ajusta o recorta en píxeles, no en caracteres. La imagen 1-bit resultante se envía con el comando raster ESC/POS `GS v 0` (universalmente soportado por térmicas; Epson lo marca "obsolete" a favor de `GS ( L`, pero sigue siendo el más compatible). Este es el patrón estándar de la industria para impresión térmica de alta fidelidad (python-escpos, react-native-thermal, etc. rasterizan igual).
_Source: https://github.com/fogleman/gg, https://download4.epson.biz/sec_pubs/pos/reference_en/escpos/gs_lv_0.html_
_Confianza: Alta._

**Box model propio sobre texto ESC/POS — candidato fuerte para el caso común.** Formalizar contenedores con padding/margin/width como propiedades (en mm, resueltas contra el ancho real del dispositivo) manteniendo la salida en texto nativo ESC/POS. Máxima velocidad de impresión y mínimo tráfico de bytes; la limitación intrínseca es la granularidad de la celda de carácter (12 dots).
_Confianza: Alta (análisis propio contra el código)._

### Librerías ESC/POS en Go

El ecosistema Go de ESC/POS es de librerías pequeñas y poco mantenidas (hennedo/escpos, kenshaw/escpos con paquete `raster`, seer-robotics/escpos). Ninguna es un estándar dominante; nuestro `internal/escpos/builder.go` propio ya cubre texto, QR, barcode y área de impresión. La pieza faltante no es una librería nueva sino la función imagen→`GS v 0` (~100 líneas, patrón conocido, kenshaw/escpos/raster.go sirve de referencia).
_Source: https://github.com/hennedo/escpos, https://github.com/kenshaw/escpos/blob/master/raster.go_
_Confianza: Alta._

### Plataforma Windows

Hallazgo crítico: **cuando el trabajo se envía con datatype RAW, el spooler y el driver ignoran DEVMODE** — dmPaperWidth/dmPaperLength/dmScale y XFORM solo tienen efecto en la vía GDI (donde el driver renderiza). Por tanto, la propuesta de la investigación previa de "usar DEVMODE/XFORM para corregir encuadre" **no aplica al flujo RAW ESC/POS actual**: son mundos excluyentes. `AddCustomPaperSize` (godoes/printers, wrapper de AddForm/winspool) registra formularios de papel útiles solo para la vía con driver (inkjet/láser o térmicas usadas vía driver Windows). alexbrainman/printer (regla del proyecto) cubre el envío RAW; godoes/printers puede complementar para consulta de formularios/DEVMODE si se implementa la vía driver.
_Source: https://forums.dotnetfoundation.org/t/windows-print-spooler-api-print-raw-devmode-printer-settings-ignored/4374, https://learn.microsoft.com/en-us/windows-hardware/drivers/print/raw-data-type, https://github.com/godoes/printers/blob/main/custom_page_size.go, https://qz.io/docs/what-is-raw-printing_
_Confianza: Alta para la separación RAW/GDI (multi-fuente + doc oficial de Microsoft); Media en detalles finos de drivers Epson TM específicos._

### Plataforma Linux

El flujo actual (`lp -o raw`) es el correcto para térmicas: los filtros CUPS no entienden ESC/POS y cualquier reescalado del spooler corrompería los bytes. La delegación a CUPS con filtros (propuesta de la investigación previa vía jeffotoni/printserver) aplica solo a impresoras con driver/PPD (inkjet/láser), vía que ya existe en `printText`. Conclusión: en Linux el problema no es la vía de salida sino el contenido de los bytes.
_Confianza: Alta (verificado contra código actual + comportamiento documentado de CUPS raw)._

### Abstracción de capacidades de dispositivo

OpenPrinting/go-mfp es una base de simulador IPP/eSCL para multifunción de red — su paquete `abstract` modela capacidades IPP, no aplica a térmicas ESC/POS conectadas por USB/serie que no hablan IPP. El patrón sí es rescatable como diseño: una interfaz Go `Capabilities()` (ancho imprimible en dots, DPI, corte, cajón) implementada por perfil de dispositivo, en lugar de anclas hardcodeadas. Para térmicas reales la fuente de capacidades es configuración local (config.json/BD) o comandos ESC/POS de status (GS I) — no un protocolo de descubrimiento.
_Source: https://github.com/OpenPrinting/go-mfp_
_Confianza: Alta en el alcance de go-mfp; Media en soporte de GS I entre marcas chinas genéricas._

### Tendencias de adopción

La industria POS converge en: payload estructurado (JSON) → render local raster o texto → RAW al dispositivo (patrón de qz.io, ESCPOS-NET, python-escpos, react-native printers). Los motores HTML/navegador quedaron para PDF de oficina, no para tickets térmicos. El comando raster `GS v 0` sigue siendo el mínimo común denominador multi-marca.
_Source: https://qz.io/docs/raw, https://lib.rs/crates/escpos_
_Confianza: Media-Alta._

---

## Integration Patterns Analysis

### Contrato JSON servidor↔cliente (capa 1)

El contrato actual (`PrintPayload` en renderer.go) ya transporta geometría física en mm (`dimension_papel`, `padding`) y bloques tipados (`text`, `table`, `columns`, `qr`, `barcode`, `separator`, `spacer`). Es la interfaz correcta y se mantiene como premisa fija. La evolución necesaria no es cambiar el formato sino **enriquecer la semántica de caja**: hoy `padding` es un número global que el renderer resta a mano; en un box model pasa a ser propiedad resuelta por el motor de layout contra el ancho real del dispositivo, y los bloques pueden declarar propiedades de caja (width %, align, padding) sin que cada tipo de bloque reimplemente la aritmética. Patrón de la industria: los payloads de qz.io y ESCPOS-NET son igualmente estructurados y el layout se resuelve 100% en el cliente.
_Source: https://qz.io/docs/raw_
_Confianza: Alta._

### Comandos ESC/POS de geometría (capa 2 — cliente↔impresora)

La geometría en ESC/POS estándar se controla con tres comandos que ya usa parcialmente nuestro `builder.go`:

- **`GS L` (left margin):** margen izquierdo = `(nL + nH×256) × unidad de movimiento horizontal` desde el borde del área imprimible.
- **`GS W` (print area width):** ancho de área de impresión desde el margen izquierdo.
- **Clamping automático del firmware:** si `margen izquierdo + ancho > área imprimible`, la impresora recorta el ancho a `área imprimible − margen izquierdo`; si el ancho queda menor que un carácter, lo extiende. Es decir, **la impresora ya implementa la defensa contra desborde de área** — el desborde que sufrimos es de contenido (texto que excede el ancho en caracteres), no de área.
- **`GS P`** fija la unidad de movimiento (por defecto 1/203" ≈ 0.125mm en térmicas de 203dpi), que es la base real de la conversión mm→dots.

Implicación de diseño: el "encuadre en medio" no necesita espacios en blanco manuales — `GS L` desplaza el origen físicamente. El firmware es la última línea de defensa, pero el contenido debe medirse antes en el cliente.
_Source: https://download4.epson.biz/sec_pubs/pos/reference_en/escpos/gs_cl.html, https://download4.epson.biz/sec_pubs/pos/reference_en/escpos/gs_cw.html, https://escpos.readthedocs.io/en/latest/layout.html_
_Confianza: Alta (documentación oficial Epson)._

### Consulta de capacidades del dispositivo (GS I) y su límite práctico

`GS I` transmite el printer ID, incluyendo un byte de "paper width and resolution". Sin embargo, es un protocolo **bidireccional con handshake estricto** (no enviar más datos hasta recibir respuesta, buffer de envío de 99 bytes en algunas interfaces). Nuestro flujo actual es **unidireccional**: en Linux los bytes van por `lp` al spooler CUPS y en Windows por `WritePrinter` al spooler — en ninguno de los dos casos hay canal de lectura desde la impresora. Solo un transporte directo (TCP 9100, que es bidireccional, o USB/serie crudo sin spooler) permitiría leer la respuesta.

Implicación de diseño (crítica para la Decisión 4): **la fuente de capacidades del dispositivo debe ser configuración declarada** (perfil de impresora en config/BD: ancho de papel, DPI, dots imprimibles, corte, cajón), no autodescubrimiento. `GS I` queda como mejora opcional futura solo para impresoras de red 9100.
_Source: https://download4.epson.biz/sec_pubs/pos/reference_en/escpos/gs_ci.html, https://www.b4x.com/android/forum/threads/how-to-find-out-paper-width-for-an-esc-pos-printer.123231/_
_Confianza: Alta en el mecanismo; Alta en la restricción de unidireccionalidad vía spooler._

### Transportes hacia el dispositivo (capa 3)

| Transporte | Bidireccional | Estado en el proyecto |
|---|---|---|
| Spooler Windows (winspool RAW, alexbrainman/printer) | No (práctico) | Regla del proyecto, vía actual |
| Spooler CUPS (`lp -o raw`) | No | Vía actual en Linux |
| TCP 9100 (JetDirect/AppSocket) | Sí | No implementado; candidato futuro para impresoras de red |
| USB/serie crudo | Sí (según interfaz) | No implementado; evita el spooler pero pierde cola/reintentos del SO |

El puerto 9100 es transporte crudo estándar multi-fabricante (no un protocolo de impresión): entrega los bytes tal cual y permite leer respuestas. Mantener el spooler como vía primaria conserva la cola, reintentos y visibilidad del SO — coherente con la Autoridad de Impresión Local del proyecto.
_Source: https://sslinsights.com/what-is-port-9100/, https://support.hp.com/us-en/document/c02480766_
_Confianza: Alta._

### Integración de depuración: impresora simulada

Para verificar los bytes reales sin hardware existen dos patrones complementarios: (a) un emulador TCP 9100 que parsea ESC/POS y renderiza el ticket visualmente (p. ej. PDVPrinterSim, C++/SFML), y (b) una cola CUPS apuntada a archivo o un servidor LPD/RAW que captura `.prn` (patrón sim-printer). Para nuestro caso, la opción de menor fricción es un **modo de captura en el propio cliente** (volcar los bytes que van a `Print()` a un archivo `.prn` con un flag de config) más un visor ESC/POS externo — no requiere componente nuevo en producción.
_Source: https://github.com/vhpontes/PDVPrinterSim_
_Confianza: Alta en el patrón; Media en herramientas concretas de visualización._

---

## Architectural Patterns and Design

### Patrón central: pipeline de tres etapas con representación intermedia

El patrón dominante en sistemas de impresión de tickets maduros es: **entrada estructurada → representación intermedia (IR) de layout → múltiples backends de salida**. Ejemplos verificados: el ecosistema receipt-print-hq convierte plantillas a una representación de comandos que emite HTML o bytes ESC/POS; EscPosEncoder y esc-pos-encoder (NielsLeenheer) separan composición de emisión; zachzurn/thermal renderiza la misma IR a imagen o HTML. Aplicado a nuestro cliente:

```
PrintPayload (JSON)
    → árbol de layout (bloques con propiedades de caja resueltas en dots)
    → primitivas medidas (líneas de texto posicionadas, imágenes raster)
    → backend de emisión:
        • escpos-text  (builder.go actual: máxima velocidad, caso común)
        • escpos-raster (gg → GS v 0: fidelidad de píxel para casos que el texto no cubre)
        • driver-SO    (texto plano a CUPS / GDI futuro para inkjet/láser)
```

La clave arquitectónica: **el layout se resuelve una sola vez, en dots, antes de elegir backend**. Los backends solo traducen primitivas ya medidas; ninguno vuelve a hacer aritmética de geometría. Esto elimina la duplicación actual (renderer.go repite el cálculo de padding en `renderESCPOS` y en el fallback de texto, renderer.go:152-170 y 401-414).
_Source: https://github.com/NielsLeenheer/EscPosEncoder, https://github.com/zachzurn/thermal, https://medium.com/till-engineering/receipt-printing-with-esc-pos-a-javascript-cross-platform-library-7110d7f7a1db_
_Confianza: Alta (patrón convergente multi-proyecto)._

### Layout de dos pasadas (measure → arrange)

Los motores de layout probados (WPF, Avalonia, Jetpack Compose) usan dos pasadas: *measure* (el padre comunica restricciones de tamaño al hijo; el hijo responde con su tamaño deseado) y *arrange* (el padre posiciona a los hijos con los tamaños ya conocidos). Para tickets térmicos —flujo vertical, ancho fijo— es suficiente una versión simplificada: la restricción es un único `maxWidthDots` que baja del contenedor raíz (papel − márgenes) a cada bloque, y cada bloque responde su altura. El desborde se vuelve imposible por construcción: ningún bloque recibe más ancho del que existe, y el word-wrap/truncado se decide en la pasada de medición, no al emitir bytes.
_Source: https://learn.microsoft.com/en-us/dotnet/desktop/wpf/advanced/layout, https://docs.flutter.dev/ui/layout/constraints, https://doveletter.dev/articles/compose-complex-layouts_
_Confianza: Alta._

### Perfiles de capacidades de dispositivo (patrón escpos-printer-db)

La solución de la industria al problema de las anclas fijas existe y está estandarizada: **escpos-printer-db** (receipt-print-hq), la base de datos JSON de perfiles que comparten python-escpos y escpos-php. Cada perfil declara `media.width` en píxeles y mm, columnas por fuente, y features soportadas (corte, cajón, QR nativo, code pages). python-escpos advierte explícitamente que centrar imágenes "no tiene efecto" sin un perfil con `media.width.pixels` — confirmación directa de que la geometría debe venir del perfil, no de interpolación.

Adaptación a nuestro cliente: un tipo `DeviceProfile` en Go (`WidthDots`, `DPI`, `CharWidthDots`, `SupportsCut`, `SupportsDrawer`, `SupportsQR`...) resuelto en este orden: (1) perfil explícito en configuración de la impresora, (2) perfil derivado del ancho de papel declarado (los actuales 58→384 / 80→576 quedan como *defaults* de perfil, no como fórmula universal), (3) fallback conservador 58mm. La interpolación lineal actual queda solo como derivación de perfil para anchos no estándar.
_Source: https://github.com/receipt-print-hq/escpos-printer-db/blob/master/dist/capabilities.json, https://python-escpos.readthedocs.io/en/latest/user/usage.html_
_Confianza: Alta._

### Separación por plataforma (se mantiene y se refuerza)

La arquitectura actual ya cumple el patrón correcto: interfaz `Printer` única con implementaciones por build tag (`printer_windows.go` / `printer_linux.go`). El rediseño no toca esta frontera — el pipeline de layout es 100% portable (Go puro, sin syscalls) y solo las últimas ~50 líneas (entrega de bytes al spooler) difieren por SO. Los perfiles de dispositivo viven encima de esa frontera, no debajo: el mismo `DeviceProfile` alimenta al renderer sin importar el SO.
_Confianza: Alta (verificado contra estructura actual + regla de CLAUDE.md)._

### Escalabilidad y rendimiento

Para el volumen de un restaurante (decenas-cientos de tickets/hora) el renderizado no es cuello de botella; el criterio arquitectónico dominante es **robustez y tamaño de binario**, no throughput. El backend texto emite ~1-4KB por ticket; el raster `GS v 0` a 576 dots de ancho emite ~72 bytes/línea de píxeles (~50-100KB por ticket típico) — relevante solo en impresoras serie a 9600bps, irrelevante en USB/red. Regla de diseño resultante: texto nativo como vía por defecto, raster como vía selectiva (logos, layouts que exceden la celda de carácter), nunca rasterizar por defecto lo que el texto resuelve.
_Confianza: Alta en órdenes de magnitud (aritmética verificable); no requiere fuente externa._

---

## Implementation Approaches and Technology Adoption

### Estrategia de adopción: incremental, sin big bang

El rediseño no reescribe el cliente: refactoriza el corazón del renderer manteniendo contrato JSON, interfaz `Printer` y flujo SQLite/worker intactos. Orden de adopción con menor riesgo:

1. **Perfiles de dispositivo** — introducir `DeviceProfile` y mover `paperGeometry` a "derivador de perfil". Cero cambio de bytes emitidos: los tests existentes validan la equivalencia.
2. **Box model / IR** — pasada measure→arrange sobre los bloques existentes, emitiendo por el backend texto actual. Los golden tests detectan cualquier regresión de bytes.
3. **Backend raster selectivo** — `gg` + `GS v 0` para logos/casos que el texto no cubre, activado por bloque (`type:"image"`) o por flag de perfil.
4. **(Opcional futuro)** transporte TCP 9100 bidireccional y `GS I`.

Cada fase es desplegable y reversible por separado — compatible con el auto-update existente del cliente.
_Confianza: Alta (estrategia estándar de migración incremental aplicada al código real)._

### Testing y aseguramiento de calidad

- **Golden files de bytes ESC/POS:** el patrón validado para salidas binarias complejas — generar bytes, comparar contra referencia versionada, regenerar solo en cambios intencionales. Ya existe la base en `builder_test.go`/`renderer_test.go`; el rediseño exige elevarlo a suite golden por payload representativo (58mm, 80mm, ancho custom, con/sin padding, tablas con merge).
- **Modo captura `.prn`:** flag de config que vuelca los bytes de `Print()` a archivo — cero dependencias nuevas.
- **Emulador visual:** emuladores ESC/POS por TCP con preview GUI (escpresso, EscPosEmulator) permiten validación visual sin hardware; útiles en desarrollo, no en CI.
_Source: https://github.com/franiglesias/golden, https://github.com/jflaflamme/escpresso, https://github.com/roydejong/EscPosEmulator_
_Confianza: Alta._

### Detalles de implementación del backend raster

- **Fuente embebida:** `golang.org/x/image/font/opentype` parsea TTF/OTF embebidos con `go:embed`; para tickets, una monoespaciada embebida (Go Mono o Inconsolata, ambas con licencia libre y disponibles como paquete Go) mantiene la métrica predecible. `font.Face` con `Size` y `DPI: 203` alinea la tipografía con la resolución física del cabezal.
- **Binarización 1-bit:** regla de la industria: **threshold (~50%) para logos/texto** (bordes nítidos), **Floyd-Steinberg para fotos** (difusión de error, implementable con buffer de una línea). Para USQAY: threshold como default (los tickets llevan logos, no fotos).
- **Empaquetado `GS v 0`:** ancho en bytes = `ceil(widthDots/8)`, MSB primero, raster top-down. ~100 líneas de Go con `image.Gray` como entrada.
_Source: https://pkg.go.dev/golang.org/x/image/font/opentype, https://blog.golang.org/go-fonts, https://label.live/guides/optimizing-thermal-print-quality, https://en.wikipedia.org/wiki/Floyd%E2%80%93Steinberg_dithering_
_Confianza: Alta._

### Riesgos y mitigaciones

| Riesgo | Impacto | Mitigación |
|---|---|---|
| Firmware de térmicas genéricas (chinas) con soporte parcial de `GS L`/`GS W`/`GS v 0` | Encuadre incorrecto en hardware barato | Perfil de dispositivo con flags de features (patrón escpos-printer-db); fallback a espacios/texto si el perfil marca no-soportado |
| Regresión de bytes durante el refactor del renderer | Tickets mal impresos en producción | Suite golden previa al refactor (fase 0); comparación byte a byte |
| Granularidad de celda de carácter (12 dots) en backend texto | Alineaciones imperfectas ±6 dots | Documentar como límite del backend texto; raster selectivo cuando la precisión importe |
| Codificación de caracteres en raster vs texto | Acentos/ñ correctos en un backend y no en otro | El raster elimina el problema de code pages (dibuja glifos); el backend texto ya tiene `encoding.go` |
| Peso del ticket raster en impresoras serie lentas | Impresión perceptiblemente lenta | Raster selectivo por bloque, nunca ticket completo por defecto |

_Confianza: Alta en los riesgos identificados; Media en la prevalencia real de firmware genérico defectuoso (anecdótico multi-fuente)._

## Technical Research Recommendations

### Decisiones finales (una recomendación por decisión)

**Decisión 1 — Motor de layout: box model propio en Go con IR de dos pasadas, híbrido texto+raster.**
Pipeline JSON → árbol de layout con restricción `maxWidthDots` → primitivas medidas → backend. Texto ESC/POS como emisión por defecto; raster (`fogleman/gg` + `GS v 0`) selectivo para logos y precisión sub-carácter. Se descartan: HTML/CSS (wkhtmltopdf muerto, Chromium inviable) y PDF intermedio (go-pdfium sobredimensionado).

**Decisión 2 — Windows: mantener RAW vía alexbrainman/printer.**
DEVMODE/XFORM/AddCustomPaperSize documentados como NO aplicables al flujo RAW (Windows los ignora); quedan reservados para una futura vía GDI de inkjet/láser si se necesita. El encuadre se resuelve en los bytes (`GS L`/`GS W` + contenido medido), idéntico en ambos SO.

**Decisión 3 — Linux: mantener `lp -o raw` para térmicas.**
La vía actual es correcta; no delegar térmicas a filtros CUPS. La vía con driver (`printText`) sigue para inkjet/láser.

**Decisión 4 — Capacidades: `DeviceProfile` declarativo (patrón escpos-printer-db).**
Resolución: perfil explícito en config → derivado del ancho declarado (anclas actuales como defaults de perfil) → fallback 58mm. Sin autodescubrimiento (`GS I` inviable vía spooler unidireccional).

### Roadmap de implementación

- **Fase 0:** suite golden de bytes sobre el renderer actual (red de seguridad).
- **Fase 1:** `DeviceProfile` + refactor de `paperGeometry` (equivalencia byte a byte).
- **Fase 2:** IR measure→arrange, backend texto; elimina la doble aritmética de padding y el desborde de contenido.
- **Fase 3:** backend raster selectivo (`gg`, fuente embebida, threshold, `GS v 0`) + modo captura `.prn`.
- **Fase 4 (opcional):** transporte TCP 9100 + `GS I` para impresoras de red.

### Stack recomendado

Go puro sin CGO en todo el pipeline. Dependencias nuevas: `fogleman/gg` y `golang.org/x/image` (ambas puras Go) — solo desde Fase 3. Ninguna dependencia nueva en Fases 0-2.

### Métricas de éxito

- 0 desbordes de contenido en la matriz de payloads golden (58/80/custom × padding × tablas).
- Encuadre centrado verificable en `.prn` sin espacios manuales (uso de `GS L`).
- Equivalencia byte a byte en Fase 1; diffs golden solo intencionales en Fase 2+.
- Binario del cliente sin CGO y sin crecimiento >3MB hasta Fase 3.

---

# Research Synthesis — De anclas fijas a box model con perfiles de dispositivo

## Executive Summary

El problema de desbordes, paddings y encuadre del cliente USQAY Print no se resuelve cambiando la vía de salida al spooler ni con las APIs de geometría de Windows que sugería la investigación inicial: se resuelve en el contenido de los bytes ESC/POS, midiendo el layout en el cliente antes de emitir. La investigación validó cada enfoque propuesto contra fuentes primarias y contra el código real, y produjo tres correcciones de rumbo determinantes:

1. **DEVMODE, XFORM y AddCustomPaperSize no aplican al flujo RAW.** Windows ignora la configuración de DEVMODE cuando el datatype es RAW (doc. oficial Microsoft + multi-fuente). Eran la pista más costosa de la investigación previa: pertenecen a la vía GDI con driver, un mundo excluyente del nuestro.
2. **HTML/CSS y PDF intermedio quedan descartados con argumentos duros.** wkhtmltopdf está archivado desde enero 2023 con CVE-2022-35583 (CVSS 9.8) sin parche; las alternativas vivas exigen Chromium completo; go-pdfium (viable sin CGO vía WebAssembly) es un motor PDF entero para rasterizar tickets de texto. El payload JSON existente ya es el contrato correcto.
3. **La solución tiene tres piezas probadas por la industria:** un box model de dos pasadas (measure→arrange, patrón WPF/Flutter/Compose) que hace el desborde imposible por construcción; perfiles de dispositivo declarativos (patrón escpos-printer-db, compartido por python-escpos y escpos-php) que reemplazan las anclas 58/80 por capacidades por impresora; y los comandos nativos `GS L`/`GS W` para encuadre físico sin espacios manuales, con clamping automático del firmware como última defensa.

**Key Technical Findings:**

- El desborde actual es de contenido (texto que excede el ancho medido en caracteres), no de área: la impresora ya protege el área vía firmware; el cliente debe medir el contenido.
- El flujo vía spooler es unidireccional en ambos SO → autodescubrimiento (`GS I`) inviable → las capacidades deben ser configuración declarada.
- El pipeline correcto es: `PrintPayload` (JSON) → árbol de layout resuelto en dots → primitivas medidas → backend de emisión (texto ESC/POS por defecto; raster `gg`+`GS v 0` selectivo).
- La frontera por build tags actual (`printer_windows.go`/`printer_linux.go`) se mantiene intacta: el pipeline de layout es Go puro portable.
- renderer.go duplica hoy la aritmética de padding en dos sitios (líneas 152-170 y 401-414); la IR la unifica.

**Technical Recommendations (decisiones cerradas):**

| # | Decisión | Recomendación única |
|---|---|---|
| 1 | Motor de layout | Box model propio en Go, IR de dos pasadas, híbrido texto+raster selectivo |
| 2 | Salida Windows | Mantener RAW vía alexbrainman/printer; DEVMODE/XFORM reservados a futura vía GDI |
| 3 | Salida Linux | Mantener `lp -o raw` para térmicas; filtros CUPS solo para inkjet/láser |
| 4 | Capacidades | `DeviceProfile` declarativo (config → derivado del ancho → fallback 58mm); sin autodescubrimiento |

## Table of Contents

1. Research Overview (inicio del documento)
2. Technical Research Scope Confirmation
3. Technology Stack Analysis — línea base del código, candidatos de motor, plataformas
4. Integration Patterns Analysis — contrato JSON, comandos de geometría ESC/POS, transportes, depuración
5. Architectural Patterns and Design — pipeline de 3 etapas, layout de dos pasadas, perfiles, escalabilidad
6. Implementation Approaches and Technology Adoption — adopción incremental, testing, raster, riesgos
7. Technical Research Recommendations — decisiones finales, roadmap, stack, métricas
8. Research Synthesis (este capítulo) — resumen ejecutivo, metodología, conclusiones

## Metodología y verificación de fuentes

- **13 búsquedas web** ejecutadas sobre: estado de wkhtmltopdf, go-pdfium, godoes/printers, comandos ESC/POS (`GS v 0`, `GS L`, `GS W`, `GS I`), OpenPrinting/go-mfp, ecosistema escpos en Go, RAW vs GDI en Windows, transportes 9100/USB/serie, escpos-printer-db, motores de layout de dos pasadas, pipelines de render de tickets, golden testing, dithering 1-bit y fuentes embebidas en Go.
- **Fuentes primarias privilegiadas:** referencia oficial ESC/POS de Epson (download4.epson.biz), documentación de drivers de Microsoft Learn, documentación oficial de python-escpos, repositorios de los proyectos evaluados.
- **Contraste con código real:** `renderer.go` (paperGeometry, doble aritmética de padding), `builder.go`, `printer_linux.go` (vía `lp -o raw` ya correcta).
- **Niveles de confianza declarados por sección**; el único punto con confianza Media relevante es la prevalencia de firmware genérico con soporte parcial de comandos — mitigado por los flags de features del perfil.
- **Limitación conocida:** no se probó hardware físico durante la investigación; la validación práctica pertenece a la Fase 0/3 del roadmap (golden tests + modo captura `.prn`).

## Conclusión

**Impacto estratégico:** el rediseño no es una reescritura sino un refactor del corazón del renderer que conserva contrato JSON, interfaz `Printer`, flujo SQLite/worker y reglas del proyecto (Go puro sin CGO, build tags por SO). Las piezas nuevas son pequeñas y de riesgo acotado: un tipo `DeviceProfile`, una pasada measure→arrange, y ~100 líneas de raster `GS v 0` con `fogleman/gg` + fuente embebida (únicas dependencias nuevas, ambas Go puro, recién en Fase 3).

**Roadmap:** Fase 0 golden tests → Fase 1 perfiles (equivalencia byte a byte) → Fase 2 box model/IR → Fase 3 raster selectivo + captura `.prn` → Fase 4 opcional TCP 9100 + `GS I`.

**Próximo paso:** destilar estas cuatro decisiones y el roadmap en una spec formal (bmad-spec) que sirva de contrato de implementación.

---

**Technical Research Completion Date:** 2026-07-15
**Source Verification:** Todas las afirmaciones técnicas citadas con fuentes vigentes; multi-fuente en las críticas
**Technical Confidence Level:** Alta en las 4 decisiones; Media solo en prevalencia de firmware genérico defectuoso
