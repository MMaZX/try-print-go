# Guía Completa de Tablas (`type: "table"`)

El bloque `table` es el componente más versátil del motor de plantillas de **USQAY Print**. Permite estructurar desde comandas de cocina y cuentas detalladas hasta resúmenes financieros clave-valor, con soporte para columnas proporcionales, encabezados, fusión de celdas (`merge`), tamaños tipográficos y alineaciones individuales.

---

## 1. Estructura General del Bloque

Un bloque de tabla se compone de dos secciones principales:
1. **`columns`** (opcional): Define el número de columnas, anchos proporcionales, encabezados y alineaciones predeterminadas.
2. **`rows`** (requerido): Array de filas que contienen las celdas de datos con sus respectivos formatos y estilos.

```json
{
  "type": "table",
  "columns": [
    { "header": "CANT",     "width": 0.15, "align": "left"  },
    { "header": "PRODUCTO", "width": 0.60, "align": "left"  },
    { "header": "TOTAL",    "width": 0.25, "align": "right" }
  ],
  "rows": [
    {
      "cells": [
        { "text": "2" },
        { "text": "LOMO SALTADO" },
        { "text": "S/ 84.00" }
      ]
    }
  ]
}
```

---

## 2. Configuración de Columnas (`columns`)

El array `columns` configura la cuadrícula horizontal. Cada elemento admite los siguientes campos:

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `width` | `number` (float) | Sí | Proporción del ancho de la columna respecto al ancho total del ticket. Se puede expresar en decimales (`0.15`, `0.60`, `0.25`) o en porcentajes (`15`, `60`, `25`). La suma total debe dar `1.0` (o `100`). |
| `header` | `string` \| `null` | No | Texto que se imprimirá en la fila de encabezado en negrita. Si ninguna columna tiene `header` (o son `null`), la fila de encabezado se omite automáticamente. |
| `align` | `string` | No | Alineación predeterminada para las celdas de esta columna: `"left"` (default), `"center"` o `"right"`. |
| `key` | `string` | No | Solo para tablas dinámicas (`rows: "{{items}}"`). Enlaza esta columna a un campo de ítem del schema del documento (variable con `is_item_field: true`), p. ej. `cant`, `producto`. Cuando al menos una columna define `key`, el backend genera las celdas de cada ítem en el orden de `columns[]` leyendo ese campo; una columna sin `key` produce celda vacía. Si ninguna columna define `key`, se mantiene el volcado posicional histórico. `notas` no se admite como `key` (siempre se emite como fila `merge` con `↳` debajo del ítem). En tablas de filas fijas se ignora. |
| `bold` | `boolean` | No | Solo tablas dinámicas (`rows: "{{items}}"`). Si es `true`, cada celda generada para esa columna sale con `bold: true`. En filas fijas se ignora (ahí el estilo vive en `cells[]`). |
| `size` | `string` | No | Solo tablas dinámicas. Tamaño tipográfico de las celdas generadas para esa columna: `"normal"`, `"medium"` o `"double"`. Si se omite, el backend usa `"medium"` en documentos de cocina (COMANDA / COMANDA_ANULACION) y `normal` en el resto; un valor explícito pisa ese fallback. En filas fijas se ignora. |

> **`key`, `bold` y `size` de `columns` son metadato de build-time.** Los consume el backend de plantillas (Laravel `PrintDispatchService::buildTableRows()`) para armar las celdas de cada ítem. El motor de impresión Go nunca los ve: recibe `rows[].cells[colIdx]` ya resueltas y posicionales contra `columns[colIdx]`, con `bold` / `size` ya aplicados a cada celda.

### Modo Automático (`columns: null` o ausente)
Si no se define `columns`, el motor distribuye el espacio equitativamente entre las celdas de cada fila sin imprimir encabezados. Es ideal para desgloses clave-valor (ej. Subtotal, IGV, Total).

---

## 3. Configuración de Filas (`rows`)

Cada objeto del array `rows` representa una línea horizontal y admite:

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `cells` | `array` de objetos | Sí | Array posicional de celdas. La celda `[0]` corresponde a la columna `[0]`. |
| `bold` | `boolean` | No | Si es `true`, aplica negrita a **todas** las celdas de la fila. Por defecto `false`. |
| `merge` | `boolean` | No | Si es `true`, la fila ignora la cuadrícula de columnas y expande la **primera celda** (`cells[0]`) a lo largo de todo el ancho del ticket (`colspan` completo). |

---

## 4. Configuración de Celdas Individuales (`cells[]`)

Cada celda permite sobreescribir estilos a nivel puntual:

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `text` | `string` | Sí | Contenido textual de la celda. Si el texto excede el ancho de la columna, se envuelve automáticamente en varias líneas respetando palabras completas. |
| `bold` | `boolean` | No | Activa o desactiva negrita en esta celda específica. Sobreescribe el `bold` de la fila. |
| `size` | `string` | No | Tamaño tipográfico de la celda: `"normal"` (15px, default), `"medium"` (20px, bold), `"double"` (28px, extra-bold). |
| `align` | `string` | No | Alineación horizontal de la celda: `"left"`, `"center"`, `"right"`. Sobreescribe la alineación de la columna. |

---

## 5. Catálogo de Ejemplos Prácticos

### Ejemplo 1: Comanda de Cocina con Modificadores y Notas (`merge: true`)
Usa `merge: true` para que las notas de preparación o mensajes de anulación ocupen todo el ancho sin descuadrar las columnas de cantidad y producto.

```json
{
  "type": "table",
  "columns": [
    { "header": "CANT", "width": 0.15, "align": "left" },
    { "header": "PRODUCTO", "width": 0.85, "align": "left" }
  ],
  "rows": [
    {
      "cells": [
        { "text": "2", "bold": true, "size": "medium" },
        { "text": "HAMBURGUESA ROYAL", "bold": true, "size": "medium" }
      ]
    },
    {
      "merge": true,
      "cells": [
        { "text": "  ↳ Término 3/4, sin cebolla" }
      ]
    },
    {
      "merge": true,
      "cells": [
        { "text": "  ↳ Papas rústicas extras" }
      ]
    },
    {
      "cells": [
        { "text": "1" },
        { "text": "CHICHA MORADA 1L" }
      ]
    }
  ]
}
```

---

### Ejemplo 2: Cuenta / Precuenta Detallada con Encabezados y Alineaciones
Columnas alineadas estratégicamente (texto a la izquierda, números y precios a la derecha).

```json
{
  "type": "table",
  "columns": [
    { "header": "CANT", "width": 0.12, "align": "left" },
    { "header": "DESCRIPCIÓN", "width": 0.50, "align": "left" },
    { "header": "P.UNIT", "width": 0.18, "align": "right" },
    { "header": "TOTAL", "width": 0.20, "align": "right" }
  ],
  "rows": [
    {
      "cells": [
        { "text": "1" },
        { "text": "CEVICHE CLÁSICO" },
        { "text": "38.00" },
        { "text": "38.00" }
      ]
    },
    {
      "cells": [
        { "text": "2" },
        { "text": "CERVEZA CUSQUEÑA" },
        { "text": "12.00" },
        { "text": "24.00" }
      ]
    }
  ]
}
```

---

### Ejemplo 3: Totales y Liquidación Clave-Valor (`columns: null`)
Al omitir `columns`, el layout divide el espacio automáticamente entre las celdas (ideal para 2 columnas como Etiqueta y Monto), soportando tamaños destacados (`size: "double"`).

```json
{
  "type": "table",
  "columns": null,
  "rows": [
    {
      "cells": [
        { "text": "Op. Gravada:" },
        { "text": "S/ 52.54", "align": "right" }
      ]
    },
    {
      "cells": [
        { "text": "IGV (18%):" },
        { "text": "S/ 9.46", "align": "right" }
      ]
    },
    {
      "bold": true,
      "cells": [
        { "text": "TOTAL A PAGAR:", "size": "medium" },
        { "text": "S/ 62.00", "size": "double", "align": "right" }
      ]
    }
  ]
}
```

---

### Ejemplo 4: División por Secciones o Áreas dentro de la Misma Tabla
Combinar `merge: true` con negrita para separar grupos de platos (Entradas, Fondos, Bebidas) en una sola estructura:

```json
{
  "type": "table",
  "columns": [
    { "header": "CANT", "width": 0.15, "align": "left" },
    { "header": "PLATO", "width": 0.85, "align": "left" }
  ],
  "rows": [
    {
      "merge": true,
      "bold": true,
      "cells": [
        { "text": "[ COCINA - ENTRADAS ]", "align": "center" }
      ]
    },
    {
      "cells": [
        { "text": "1" },
        { "text": "TEQUEÑOS DE QUESO (x6)" }
      ]
    },
    {
      "merge": true,
      "bold": true,
      "cells": [
        { "text": "[ COCINA - SEGUNDOS ]", "align": "center" }
      ]
    },
    {
      "cells": [
        { "text": "1" },
        { "text": "ARROZ CON MARISCOS" }
      ]
    }
  ]
}
```

---

## 6. Buenas Prácticas y Recomendaciones

1. **Suma de anchos (`width`):**
   - Asegúrate de que la suma de `width` de todas las columnas sea igual a `1.0` (o `100.0`).
2. **Columnas en papel de 80 mm vs 58 mm:**
   - **80 mm (576 dots):** Permite cómodamente tablas de 3 a 4 columnas (`CANT`, `DESCRIPCIÓN`, `P.UNIT`, `TOTAL`).
   - **58 mm (384 dots):** Se recomienda un máximo de 2 a 3 columnas (ej. `CANT 0.18`, `PRODUCTO 0.54`, `TOTAL 0.28`) para evitar líneas excesivamente envueltas.
3. **Columnas de Cantidad (`CANT`):**
   - Asigna un ancho mínimo de `0.12` a `0.15` (12% a 15%) a la columna de cantidad para dar espacio a números de 2-3 dígitos o decimales (`1.5`, `10`, `100`) junto con el padding.
4. **Fusión de Celdas (`merge: true`):**
   - En una fila con `merge: true`, solo se toma el contenido del primer elemento (`cells[0]`). No es necesario incluir más celdas en esa fila.
