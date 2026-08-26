---
id: SPEC-motor-impresion
companions:
  - architecture-diagrams.md
  - backend-changes.md
  - device-profile.md
  - escpos-reference.md
  - roadmap.md
sources:
  - ../../planning-artifacts/research/technical-motor-geometria-impresion-escpos-research-2026-07-15.md
---

> **Canonical contract.** This SPEC and the files in `companions:` are the complete, preservation-validated contract for what to build, test, and validate. Source documents listed in frontmatter are for traceability only — consult them only if you need narrative rationale or prose color this contract intentionally omits.

# Rediseño del motor de geometría/impresión del cliente USQAY Print

## Why

Dolor a resolver: los tickets térmicos sufren desbordes de contenido, paddings imprecisos y encuadre manual con espacios porque el renderer calcula la geometría con anclas fijas (58mm→384 dots, 80mm→576 dots interpoladas linealmente) y resta el padding a mano en dos puntos distintos del código. La investigación técnica del 2026-07-15 verificó que la causa no es la vía de salida (los spoolers RAW de ambos SO son correctos) sino que el contenido nunca se mide antes de emitir bytes, y cerró cuatro decisiones: box model propio de dos pasadas, RAW sin cambios en Windows, `lp -o raw` sin cambios en Linux, y perfiles de dispositivo declarativos centralizados en el servidor. Este contrato implementa ese rediseño manteniendo intactos la Autoridad de Impresión Local, el contrato JSON del payload (que solo se extiende, nunca se rompe) y el flujo SQLite/worker.

## Capabilities

- **CAP-1**
  - **intent:** El equipo puede detectar cualquier cambio en los bytes ESC/POS emitidos por el renderer actual antes de iniciar el refactor.
  - **success:** Suite golden versionada que cubre la matriz {58mm, 80mm, ancho custom} × {con/sin padding} × {texto, tablas con merge, columns, qr, barcode} y falla ante cualquier diff de bytes no intencional.

- **CAP-2**
  - **intent:** El cliente resuelve geometría y features de cada impresora desde un perfil de dispositivo declarativo, en lugar de una fórmula global de anclas.
  - **success:** Con perfil recibido del servidor, sus valores gobiernan la salida; sin perfil, anchos 58/80mm producen exactamente 384/576 dots — byte a byte igual que el renderer actual (validado por CAP-1).

- **CAP-3**
  - **intent:** El renderer mide y posiciona todos los bloques (pasada measure→arrange con restricción `maxWidthDots`) antes de emitir un solo byte.
  - **success:** Ningún payload de la matriz golden produce contenido que exceda el ancho imprimible; el padding se calcula en un único punto del código.

- **CAP-4**
  - **intent:** El encuadre físico del ticket (margen izquierdo, ancho de área) se emite con comandos nativos de geometría desde la IR, sin espacios en blanco manuales.
  - **success:** El `.prn` capturado contiene `GS L`/`GS W` con los valores del perfil; un ticket configurado más angosto que el papel sale centrado, verificable en escpresso/esc2html.

- **CAP-5**
  - **intent:** Los bloques que exceden la celda de carácter (logos vía `type:"image"`, precisión sub-carácter) se emiten como raster monocromo selectivo, manteniendo texto nativo para el resto.
  - **success:** Un bloque raster genera `GS v 0` con empaquetado correcto (`ceil(widthDots/8)` bytes/línea, MSB-first) que renderiza fiel en escpresso; los tickets sin bloques raster no cambian ni un byte respecto a CAP-4.

- **CAP-6**
  - **intent:** Un operador puede capturar a archivo los bytes exactos que el cliente envía al spooler, para verificar sin hardware.
  - **success:** Con el flag de config activo, cada trabajo produce un `.prn` idéntico a lo entregado a `Print()`; sin el flag, no se escribe ningún archivo.

- **CAP-7**
  - **intent:** El cliente aplica automáticamente el perfil actualizado cuando el servidor notifica por WebSocket que la configuración de una impresora cambió.
  - **success:** Tras un dispatch de actualización desde el backend, el siguiente trabajo de esa impresora se imprime con el nuevo perfil sin reiniciar el cliente, y el cambio queda registrado en el log.

## Constraints

- Go puro sin CGO en todo el pipeline; únicas dependencias nuevas permitidas: `fogleman/gg` y `golang.org/x/image` (ambas Go puro), y solo a partir de la Fase 3.
- La lógica de spooler permanece aislada por build tags (`printer_windows.go`/`printer_linux.go`); el pipeline de layout es portable y no puede contener syscalls ni imports específicos de SO.
- El contrato JSON `PrintPayload` solo se extiende (nuevo bloque `type:"image"` en Fase 3): todo payload que hoy imprime debe seguir imprimiendo sin cambios en el servidor.
- Los perfiles de impresora se administran exclusivamente en el servidor/BD; prohibido introducir overrides locales de perfil en `config.json`.
- Prohibido usar DEVMODE/XFORM/AddCustomPaperSize en la vía RAW: Windows los ignora con datatype RAW; cualquier encuadre debe resolverse en los bytes ESC/POS.
- Las capacidades del dispositivo son exclusivamente declarativas (perfil); nada de autodescubrimiento `GS I` — el spooler es unidireccional en ambos SO.
- Texto nativo ESC/POS es la emisión por defecto; el raster es selectivo por bloque — nunca se rasteriza un ticket completo por defecto (50-100KB vs 1-4KB).
- Toda feature de hardware (corte, cajón, QR nativo, `GS L`/`GS W`) se condiciona al flag correspondiente del perfil, con fallback definido (espacios/texto) para firmware que no la soporte.
- La Fase 1 exige equivalencia byte a byte con la salida actual; desde la Fase 2 los diffs golden solo pueden ser intencionales y revisados. Las fases se ejecutan en orden 0→3, cada una desplegable y reversible por separado.
- El binario del cliente no crece más de 3MB hasta la Fase 3 inclusive.

## Non-goals

- Vía GDI/driver de Windows para térmicas (DEVMODE, XFORM, AddCustomPaperSize): reservada a una futura necesidad de inkjet/láser, fuera de este contrato.
- Autodescubrimiento de capacidades (`GS I`) y transporte TCP 9100: Fase 4 opcional, explícitamente fuera de este contrato.
- Motores HTML/CSS (wkhtmltopdf y sucesores) o PDF intermedio (go-pdfium): descartados por la investigación.
- Cambios al flujo servidor → SQLite → ACK → worker → PRINTED, o rupturas del payload JSON existente.
- Override local de perfiles en el terminal: la administración es 100% centralizada.
- Reemplazar el backend texto por raster o rasterizar tickets completos.
- Dithering Floyd-Steinberg: threshold basta para logos; solo se reconsidera si el producto exige fotos.

## Success signal

Un ticket de la matriz golden impreso con perfil 58mm sobre papel de 80mm sale centrado, sin desbordes de contenido y sin espacios de relleno manuales — verificable en el `.prn` (presencia de `GS L`/`GS W`, ausencia de padding por espacios) con escpresso/esc2html. Al editar el perfil de una impresora en el backend, el siguiente ticket de ese terminal sale con la nueva geometría sin reiniciar el cliente. La Fase 1 reproduce byte a byte la salida del renderer actual y `go test ./...` permanece verde en todas las fases.

## Assumptions

- Threshold ~50% como binarización por defecto del raster: los tickets llevan logos, no fotos.
- 203 DPI como resolución de referencia para la conversión mm→dots (unidad de movimiento por defecto de `GS P` en térmicas comunes).
- Los defaults de perfil 58mm→384 dots y 80mm→576 dots (valores documentados por Epson) son válidos para las marcas genéricas desplegadas.
- El cliente cachea el último perfil conocido (SQLite) para trabajos que lleguen antes de un refresh; el dispatch de CAP-7 se integra al flujo de recarga de configuración en caliente ya existente en el cliente.
