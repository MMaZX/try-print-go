# Motor de Plantillas y Bloques de Impresión (USQAY Print)

Esta carpeta contiene la documentación detallada de cada bloque primitivo admitido por el motor de renderizado HTML/ESC-POS de **USQAY Print**.

El diseño del comprobante se describe mediante un objeto JSON desacoplado del hardware, compuesto por opciones de papel, hardware y un array ordenado de bloques en `body`.

---

## Índice de Documentación por Bloque

| Documento | Bloque / Sección | Descripción |
|---|---|---|
| [`paper-properties.md`](paper-properties.md) | `options` y `paper_properties` | Ancho de papel (58/80 mm), escala de lienzo, márgenes (padding de 4 lados), corte de papel y apertura de gaveta. |
| [`text.md`](text.md) | `type: "text"` | Párrafos y líneas de texto con alineación, negrita, tamaños tipográficos y ajuste de línea automático. |
| [`table.md`](table.md) | `type: "table"` | Tablas con columnas porcentuales, encabezados, fusión de celdas (`merge`), modificadores y totales clave-valor. |
| [`columns.md`](columns.md) | `type: "columns"` | Fila única de celdas paralelas lado a lado (ej. `Mesa` a la izquierda y `Mozo` a la derecha). |
| [`separator.md`](separator.md) | `type: "separator"` | Líneas divisorias horizontales continuas (punteadas, dobles o sólidas). |
| [`spacer.md`](spacer.md) | `type: "spacer"` | Espaciado vertical y avance de líneas en blanco. |
| [`image.md`](image.md) | `type: "image"` | Logotipos e imágenes en Base64 con control de ancho, alineación y dithering. |
| [`qr.md`](qr.md) | `type: "qr"` | Códigos QR nítidos generados por software con control de tamaño y alineación. |
| [`barcode.md`](barcode.md) | `type: "barcode"` | Códigos de barras 1D (Code128) con texto HRI legible superior/inferior. |

---

## Estructura General del Payload JSON

```json
{
  "options": {
    "cut": true,
    "drawer": false
  },
  "paper_properties": {
    "width": 80.0,
    "scale": 1.0,
    "padding": [1.5, 1.5, 1.5, 1.5]
  },
  "body": [
    { "type": "image", "data": "iVBORw0KGgo...", "align": "center", "width": 180 },
    { "type": "text", "value": "RESTAURANTE USQAY", "align": "center", "bold": true, "size": "medium" },
    { "type": "separator", "character": "=" },
    { "type": "columns", "columns": [
      { "text": "Mesa: 04", "width": 0.5, "align": "left" },
      { "text": "Mozo: Carlos", "width": 0.5, "align": "right" }
    ]},
    { "type": "separator", "character": "-" },
    { "type": "table", "columns": [
      { "header": "CANT", "width": 0.15, "align": "left" },
      { "header": "PRODUCTO", "width": 0.60, "align": "left" },
      { "header": "TOTAL", "width": 0.25, "align": "right" }
    ], "rows": [
      { "cells": [{ "text": "2" }, { "text": "LOMO SALTADO" }, { "text": "84.00" }] },
      { "merge": true, "cells": [{ "text": "↳ Término medio" }] }
    ]},
    { "type": "separator", "character": "=" },
    { "type": "qr", "value": "https://app.usqay.com/c/abc123xyz", "align": "center", "size": 128 },
    { "type": "spacer", "lines": 2 }
  ]
}
```
