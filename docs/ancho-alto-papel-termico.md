# Ancho y alto de papel térmico (`ancho_dimension` / `altura_dimension`)

Este documento explica cómo el agente (`usqay-print-client`) interpreta los campos `margins.ancho_dimension` y `margins.altura_dimension` del [payload de impresión](./print_payload_schema.md), por qué se implementó así, y una limitación conocida frente a cómo lo resuelve el resto de la industria.

## Historial del problema

Originalmente `ancho_dimension` solo se usaba para elegir entre dos anchos de texto fijos (32 u 80 caracteres) mediante un umbral arbitrario (`> 60mm`), y nunca se enviaba ningún comando físico a la impresora — el hardware siempre imprimía usando el ancho completo de su cabezal, sin importar el valor configurado. Además, no existía forma de que valores intermedios personalizados (ej. `70mm`) tuvieran algún efecto.

## Solución actual

### 1. Comando físico real: `GS L` / `GS W`

`usqay-print-client/internal/escpos/builder.go` expone `PrintAreaWidth(dots int)`, que emite:

- `GS L 0 0` — margen izquierdo = 0.
- `GS W nL nH` — ancho del área imprimible en dots, medido desde ese margen.

Esto es lo único que realmente le dice a la impresora "usa solo N dots de tu cabezal". Sin este comando, el "ancho" configurado en el JSON es puramente cosmético (solo afecta el padding de texto en software).

### 2. Cálculo por interpolación, no por buckets fijos

`usqay-print-client/internal/queue/renderer.go` (`paperGeometry`) usa dos anclas de calibración oficiales (documentadas por fabricantes ESC/POS, fuente A a 203dpi):

| Ancho nominal | Dots imprimibles | Caracteres |
|---|---|---|
| 58mm | 384 | 32 |
| 80mm | 576 | 48 |

Para cualquier valor recibido, se interpola (o extrapola, fuera de ese rango) linealmente entre esos dos puntos:

```
frac  = (ancho_mm - 58) / (80 - 58)
dots  = 384 + frac × (576 - 384)
chars = dots / 12
```

Así, `ancho_dimension: 70` produce ~489 dots / 40 caracteres — un valor real y proporcional, no uno de los dos buckets originales. Si el resultado excede el área física real del cabezal, la propia impresora lo recorta automáticamente al recibir `GS W` (comportamiento definido en el spec ESC/POS), así que extrapolar fuera de 58–80mm es seguro, aunque pierde precisión mientras más lejos del rango conocido esté el valor.

Si `ancho_dimension` es `<= 0` (ausente o inválido), se usa como fallback seguro el ancho térmico más angosto conocido (58mm) — preferible a arriesgar imprimir más ancho de lo que el papel físico real permite.

### 3. `altura_dimension` no se usa

Para impresoras térmicas de rollo continuo el concepto de "alto de página fijo" no aplica de la misma forma que en una hoja A4 — no hay corte automático a una altura determinada en el flujo actual. El campo se mantiene en el schema (probablemente por compatibilidad con impresoras no térmicas a futuro) pero el agente lo ignora deliberadamente.

### 4. Impresoras no térmicas (A4, láser/inkjet)

Este mecanismo (`PrintAreaWidth`, interpolación) solo aplica al camino ESC/POS (`render`/`renderStructured`, impresoras térmicas). Los tickets destinados a impresoras no térmicas usan `renderText`/`renderStructuredText`, un camino separado que no emite comandos físicos ESC/POS. En Linux, además, ese camino fuerza `-o media=A4` sin importar el JSON — una limitación preexistente, fuera del alcance de este trabajo.

## Observación / limitación conocida

Investigando cómo resuelve esto el resto del ecosistema (librerías maduras como `python-escpos` + [`escpos-printer-db`](https://github.com/receipt-print-hq/escpos-printer-db), y sistemas POS comerciales como Oliver POS o Square), el patrón dominante **no es una fórmula continua** como la implementada aquí, sino un **catálogo de perfiles por ancho/modelo conocido**: cada ancho estándar (58mm, 76mm, 80mm, 112mm...) tiene sus columnas-por-fuente documentadas directamente por el fabricante, y el software típicamente restringe la entrada del usuario a un dropdown de esas opciones — no a un campo numérico libre en mm.

La interpolación entre 58 y 80mm que usamos es una aproximación razonable mientras no se conozcan con certeza los modelos físicos de impresora en uso, pero **no es tan precisa como un perfil documentado** para anchos que sí son estándares de catálogo reales (por ejemplo 76mm o 112mm, comunes en impresoras de kiosco/etiqueta ancha) — para esos casos, un valor intermedio calculado por interpolación puede no coincidir con las columnas reales que el fabricante documenta para ese modelo exacto.

**Recomendación a futuro:** si se confirma que el fleet real incluye impresoras físicas de 76mm y/o 112mm (no solo 58/80mm con anchos personalizados desde el frontend), conviene agregar esos anchos como anclas de calibración adicionales en `paperGeometry` en vez de depender de la interpolación entre solo dos puntos.
