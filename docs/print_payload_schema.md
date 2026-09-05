# Esquema de Payload de Impresión Genérico (Print Payload Schema)

Este documento especifica la estructura JSON genérica y estrictamente tipada utilizada para representar los trabajos de impresión en la plataforma USQAY.

El objetivo de este formato es desacoplar el agente local (`usqay-print-client`) de la lógica de negocio del backend. El backend describe el ticket utilizando bloques de diseño primitivos (texto, tablas, códigos QR, etc.), y el agente simplemente se encarga de interpretarlos y traducirlos a comandos físicos (ESC/POS o texto plano).

---

## Estructura General del JSON

El JSON de impresión consta de tres secciones principales:
1. `options`: Parámetros de control físico del ticket.
2. `paper_properties`: Parámetros de dimensión física del papel, escala y márgenes de 4 lados.
3. `body`: Un array ordenado de bloques de diseño que componen el ticket.

```json
{
  "options": {
    "cut": true,
    "drawer": false
  },
  "paper_properties": {
    "width": 80.0,
    "scale": 1.0,
    "padding": [
      1.5,
      1.5,
      1.5,
      1.5
    ]
  },
  "body": [
    // Array de bloques...
  ]
}
```

---

## 1. Opciones (`options`)

Define el comportamiento del hardware antes o después de procesar el cuerpo del ticket.

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `cut` | `boolean` | Sí | Si es `true`, realiza un corte de papel parcial al terminar la impresión. |
| `drawer` | `boolean` | Sí | Si es `true`, envía un pulso para abrir la gaveta de dinero antes de imprimir. |

---

## 2. Propiedades de Papel (`paper_properties`)

Define el tamaño físico del papel, el factor de escala y los márgenes en milímetros.

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `width` | `number` (float) | Sí | Ancho físico del papel en mm (ej. `80.0`, `58.0`, `70.0`). |
| `scale` | `number` (float) | No | Factor multiplicador de escala sobre la base visual por defecto (1.4x). Por defecto `1.0` (aplica 1.4x). Si se envía `1.5`, aplica `1.4 × 1.5 = 2.1x`; si se envía `2.0`, aplica `1.4 × 2.0 = 2.8x`. |
| `padding` | `array` de 4 floats | No | Array con 4 márgenes en mm en formato CSS `[top, right, bottom, left]` (arriba, derecha, abajo, izquierda). Por defecto `[0.0, 0.0, 0.0, 0.0]`. |

---

## 3. Bloques del Cuerpo (`body`)

El cuerpo es un array de objetos JSON, donde cada objeto representa una instrucción de diseño. Todos los bloques contienen el campo `type`, que define su estructura y comportamiento.

### Referencia rápida de bloques

| Tipo | Descripción |
|---|---|
| `text` | Línea de texto con formato |
| `separator` | Línea horizontal repetitiva |
| `spacer` | Líneas en blanco |
| `table` | Tabla con columnas, filas y celdas configurables |
| `qr` | Código QR |
| `columns` | Fila de celdas independientes lado a lado |
| `barcode` | Código de barras 1D |
| `image` | Imagen o logo en Base64 |

---

### 3.1. Bloque de Texto (`type: "text"`)

Representa un párrafo de texto plano con opciones de formato. Si `value` no entra en el ancho del papel
(típico con `size: "medium"`/`"double"`, donde cada carácter ocupa más espacio) se envuelve automáticamente
en tantas líneas como haga falta — no se corta en el borde del papel ni afecta al resto del ticket.

```json
{
  "type": "text",
  "value": "TICKET DE PRUEBA",
  "align": "center",
  "bold": true,
  "size": "double"
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `value` | `string` | Sí | El contenido textual a imprimir. Se envuelve (word-wrap) automáticamente si no entra en una línea. |
| `align` | `string` | No | Alineación horizontal: `"left"` (por defecto), `"center"`, `"right"`. |
| `bold` | `boolean` | No | Activa negrita. Por defecto `false`. |
| `size` | `string` | No | `"normal"` (por defecto), `"medium"` (altura doble), `"double"` (altura + ancho doble). |

> **Nota (2026-09-05):** antes de esta fecha `value` se imprimía en una sola línea y el texto que no entraba
> en el ancho del papel se cortaba en el borde. Ahora se envuelve en líneas adicionales.

---

### 3.2. Bloque de Separador (`type: "separator"`)

Dibuja una línea horizontal repetitiva a lo largo del ancho disponible del papel.

```json
{
  "type": "separator",
  "character": "-"
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `character` | `string` | No | Carácter a repetir (ej. `"-"`, `"="`, `"*"`). Por defecto `"-"`. |

---

### 3.3. Bloque de Espaciador (`type: "spacer"`)

Introduce líneas en blanco verticales para alimentar el papel.

```json
{
  "type": "spacer",
  "lines": 2
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `lines` | `integer` | No | Número de saltos de línea a generar. Por defecto `1`. |

---

### 3.4. Bloque de Tabla (`type: "table"`)

Tabla completamente configurable con columnas, filas y celdas. Reemplaza tanto la tabla genérica anterior como la tabla de comanda. Soporta formato a nivel de fila y a nivel de celda individual.

#### Estructura general

```json
{
  "type": "table",
  "columns": [
    { "header": "CANT",     "width": 0.10, "align": "left"  },
    { "header": "PRODUCTO", "width": 0.65, "align": "left"  },
    { "header": "TOTAL",    "width": 0.25, "align": "right" }
  ],
  "rows": [
    {
      "cells": [
        { "text": "2" },
        { "text": "LOMO SALTADO" },
        { "text": "S/ 42.00" }
      ]
    },
    {
      "cells": [
        { "text": "1" },
        { "text": "COCA COLA ZERO" },
        { "text": "S/ 5.00" }
      ]
    },
    {
      "bold": true,
      "cells": [
        { "text": "" },
        { "text": "Total a pagar:" },
        { "text": "S/ 49.56", "bold": true, "size": "double" }
      ]
    }
  ]
}
```

#### Campo `columns` (Opcional)

Si es `null` o se omite, el agente distribuye el ancho en partes iguales entre las celdas, sin imprimir encabezados y con alineación `"left"` por defecto en todas las columnas.

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `columns` | `array` \| `null` | No | Definición de columnas. `null` activa el modo automático. |
| `columns[].header` | `string` | No | Texto del encabezado. Si se omite en todas las columnas, no se imprime fila de encabezado. |
| `columns[].width` | `float` | Sí | Proporción del ancho de la columna. La suma de todos los valores debe aproximarse a `1.0`. |
| `columns[].align` | `string` | No | Alineación por defecto para las celdas de esta columna: `"left"` (por defecto), `"center"`, `"right"`. |

#### Campo `rows` (Requerido)

Array de filas. Cada fila contiene sus celdas y propiedades opcionales de formato.

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `rows[].cells` | `array` | Sí | Array de celdas posicional. La posición `[0]` corresponde a la columna `[0]`. |
| `rows[].bold` | `boolean` | No | Aplica negrita a todas las celdas de la fila. Por defecto `false`. Una celda puede sobreescribir este valor con su propio `bold`. |
| `rows[].merge` | `boolean` | No | Si es `true`, ignora la estructura de columnas e imprime la fila como una línea única de ancho completo usando solo la primera celda. Por defecto `false`. |

#### Propiedades de celda (`cells[]`)

Cada celda es siempre un objeto. Los valores de formato de celda sobreescriben los valores de fila cuando ambos están definidos.

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `text` | `string` | Sí | Contenido textual de la celda. |
| `bold` | `boolean` | No | Negrita para esta celda. Sobreescribe el `bold` de la fila. |
| `size` | `string` | No | `"normal"` (por defecto), `"medium"`, `"double"`. |
| `align` | `string` | No | Sobreescribe la alineación definida en la columna para esta celda específica. |

#### Ejemplos de uso

**Sin columnas (modo automático) — reemplaza el patrón clave-valor:**

```json
{
  "type": "table",
  "columns": null,
  "rows": [
    { "cells": [{ "text": "Subtotal:" },  { "text": "S/ 42.00" }] },
    { "cells": [{ "text": "IGV (18%):" }, { "text": "S/ 7.56"  }] },
    {
      "bold": true,
      "cells": [{ "text": "Total:" }, { "text": "S/ 49.56" }]
    }
  ]
}
```

**Con `merge: true` — fila de ancho completo dentro de la tabla (notas, secciones):**

```json
{
  "type": "table",
  "columns": [
    { "header": "CANT",     "width": 0.15, "align": "left" },
    { "header": "PRODUCTO", "width": 0.85, "align": "left" }
  ],
  "rows": [
    {
      "cells": [
        { "text": "2" },
        { "text": "ARROZ CHAUFA" }
      ]
    },
    {
      "merge": true,
      "cells": [
        { "text": "↳ Bien tostado, sin cebollita china" }
      ]
    },
    {
      "cells": [
        { "text": "1" },
        { "text": "LOMO SALTADO" }
      ]
    }
  ]
}
```

---

### 3.5. Bloque de Código QR (`type: "qr"`)

Imprime un código QR. En impresoras térmicas ESC/POS se renderiza nativamente; en impresoras de texto se imprime la URL entre corchetes.

```json
{
  "type": "qr",
  "value": "https://app.usqay.com/c/abc123xyz",
  "align": "center",
  "size": 6
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `value` | `string` | Sí | La URL o texto a codificar en el QR. |
| `align` | `string` | No | Alineación horizontal: `"left"`, `"center"` (por defecto), `"right"`. |
| `size` | `integer` | No | Ancho final del QR en píxeles (ej. `120`, `200`, `300`). Se recorta automáticamente al ancho imprimible del papel si lo excede. Por defecto `128`. |

> **Nota de migración (2026-09-05):** antes de esta fecha `size` representaba el tamaño de módulo en píxeles (1-16), no el ancho final del QR. Se cambió porque con el motor de render anterior ese valor terminaba siempre recortado al ancho máximo del papel para cualquier número por encima de ~16, haciendo el campo inútil para controlar el tamaño real impreso. Si tu integración todavía manda valores en el rango 1-16 esperando el comportamiento viejo, actualízala para mandar el ancho en píxeles directamente.

---

### 3.6. Bloque de Columnas (`type: "columns"`)

Imprime una fila única con N celdas independientes lado a lado. A diferencia de `table`, no tiene múltiples filas de datos — es un primitivo de layout para una sola línea. Ideal para encabezados de ticket, datos de caja/mozo/mesa, o cualquier información que requiera texto simultáneo en distintas posiciones horizontales.

```json
{
  "type": "columns",
  "columns": [
    { "text": "Caja #1",          "width": 0.5, "align": "left"  },
    { "text": "12/06/2026 14:30", "width": 0.5, "align": "right" }
  ]
}
```

```json
{
  "type": "columns",
  "columns": [
    { "text": "Mozo:",      "width": 0.3, "align": "left" },
    { "text": "Juan Pérez", "width": 0.7, "align": "left", "bold": true }
  ]
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `columns` | `array` | Sí | Array de celdas. La suma de `width` debe aproximarse a `1.0`. |
| `columns[].text` | `string` | Sí | Contenido textual de la celda. |
| `columns[].width` | `float` | Sí | Proporción del ancho de la celda respecto al ancho total del papel. |
| `columns[].align` | `string` | No | `"left"` (por defecto), `"center"`, `"right"`. |
| `columns[].bold` | `boolean` | No | Negrita. Por defecto `false`. |
| `columns[].size` | `string` | No | `"normal"` (por defecto), `"medium"`, `"double"`. |

---

### 3.7. Bloque de Código de Barras (`type: "barcode"`)

Imprime un código de barras 1D. Complementa al bloque `qr` para casos donde se requiere un código de barras lineal (facturación, trazabilidad de productos).

```json
{
  "type": "barcode",
  "symbology": "CODE128",
  "value": "B001-00000042",
  "align": "center",
  "height": 80,
  "hri": "below"
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `symbology` | `string` | Sí | Simbología del código: `"CODE128"`, `"CODE39"`, `"EAN13"`, `"EAN8"`, `"UPCA"`, `"UPCE"`, `"ITF"`. |
| `value` | `string` | Sí | Dato a codificar. |
| `align` | `string` | No | `"center"` (por defecto), `"left"`, `"right"`. |
| `height` | `integer` | No | Altura de las barras en dots. Por defecto `80`. |
| `width` | `integer` | No | Grosor del módulo entre `1` y `6`. Por defecto `2`. |
| `hri` | `string` | No | Texto legible (Human Readable Interpretation): `"below"` (por defecto), `"above"`, `"both"`, `"none"`. |

---

### 3.8. Bloque de Imagen (`type: "image"`)

Imprime una imagen o logo del negocio. El agente convierte la imagen a bitmap monocromático antes de enviarla a la impresora mediante el comando ESC/POS `GS v S`.

```json
{
  "type": "image",
  "data": "iVBORw0KGgoAAAANSUh...",
  "align": "center",
  "width": 200
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `data` | `string` | Sí | Imagen codificada en Base64. Formatos recomendados: PNG o BMP. |
| `align` | `string` | No | `"center"` (por defecto), `"left"`, `"right"`. |
| `width` | `integer` | No | Ancho máximo en píxeles al que se escala la imagen. Si se omite, usa el ancho completo del papel. |
| `dither` | `boolean` | No | Si es `true`, aplica dithering al convertir a monocromático (recomendado para fotografías). Por defecto `false` (umbral simple, mejor para logos). |
