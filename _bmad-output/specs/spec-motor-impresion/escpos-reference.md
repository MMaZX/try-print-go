# Referencia ESC/POS de geometría y raster

Material HOW citado por el kernel. Fuentes primarias: referencia oficial ESC/POS de Epson (download4.epson.biz).

## Comandos de geometría (backend texto, CAP-4)

| Comando | Efecto | Nota |
|---|---|---|
| `GS L nL nH` | Margen izquierdo = `(nL + nH×256) × unidad horizontal` desde el borde del área imprimible | Mecanismo del encuadre: desplaza el origen físico, sin espacios manuales |
| `GS W nL nH` | Ancho del área de impresión desde el margen izquierdo | |
| `GS P x y` | Unidad de movimiento; default 1/203" ≈ 0.125mm a 203dpi | Base real de la conversión mm→dots |

Clamping del firmware (última defensa, no sustituye la medición en cliente): si `margen + ancho > área imprimible`, la impresora recorta el ancho; si queda menor que un carácter, lo extiende.

## Raster `GS v 0` (backend raster, CAP-5)

- Formato: `GS v 0 m xL xH yL yH d1...dk` con `x = ceil(widthDots/8)` bytes por línea, MSB primero, raster top-down.
- Entrada: `image.Gray` → binarización threshold ~50% (logos: bordes nítidos; Floyd-Steinberg descartado salvo fotos).
- Epson marca `GS v 0` como "obsolete" a favor de `GS ( L`, pero sigue siendo el mínimo común denominador multi-marca — es la elección correcta para hardware genérico.
- Estimación de peso: ~72 bytes/línea a 576 dots; solo relevante en enlaces serie lentos — refuerza la regla de raster selectivo.

## Tipografía del raster

- `fogleman/gg` (2D puro Go: `DrawStringWrapped`, `MeasureString`, anclas) sobre canvas de exactamente `maxWidthDots` px.
- Fuente monoespaciada TTF embebida con `go:embed` + `golang.org/x/image/font/opentype`; `font.Face` con `DPI: 203` para alinear tipografía y cabezal. Candidatas con licencia libre: Go Mono, Inconsolata.
- Como referencia de empaquetado existe `kenshaw/escpos/raster.go`; la implementación propia ronda ~100 líneas.

## Límite documentado del backend texto

Granularidad de celda de carácter: 12 dots (fuente A). Alineaciones con error máximo ±6 dots son inherentes al backend texto; cuando la precisión importe, el bloque se emite por el backend raster.

## Codificación

El backend texto sigue dependiendo de code pages (`encoding.go` existente). El backend raster elimina el problema por construcción: dibuja glifos, no envía códigos.
