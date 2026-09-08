# Bloque de Código de Barras (`type: "barcode"`)

El bloque `barcode` genera códigos de barras 1D lineales con renderizado vectorial nítido (`crispEdges`) y texto legible opcional (**HRI - Human Readable Interpretation**). Se utiliza para números de ticket, guías de despacho, órdenes de compra y trazabilidad logística.

---

## 1. Esquema del Bloque

```json
{
  "type": "barcode",
  "symbology": "CODE128",
  "value": "TKT-2026-0042",
  "align": "center",
  "height": 60,
  "width": 2,
  "hri": "below"
}
```

| Campo | Tipo | Requerido | Default | Descripción |
|---|---|---|---|---|
| `type` | `string` | Sí | `"barcode"` | Identificador del tipo de bloque. |
| `symbology` | `string` | No | `"CODE128"` | Simbología del código de barras. Soporta: `"CODE128"`, `"CODE39"`, `"EAN13"`, `"EAN8"`, `"UPCA"`. |
| `value` | `string` | Sí | `""` | Cadena de texto o dígitos a codificar. |
| `align` | `string` | No | `"center"` | Alineación horizontal en el ticket: `"center"`, `"left"`, `"right"`. |
| `height` | `integer` | No | `60` | Altura de las barras en píxeles (dots). Rango sugerido: `40` a `80`. |
| `width` | `integer` | No | `2` | Grosor del módulo de barra en píxeles. Rango sugerido: `1` a `3`. |
| `hri` | `string` | No | `"below"` | Posición del texto legible: `"below"` (abajo de las barras, default), `"above"` (arriba de las barras), `"none"` (ocultar texto). |

---

## 2. Recomendaciones de Escaneabilidad

1. **Grosor del módulo (`width`):**
   - El valor `width: 2` es el estándar óptimo para escáneres ópticos térmicos de 203 DPI.
   - En cadenas muy largas (más de 20 caracteres), usa `width: 1` o `width: 2` para evitar que el código exceda el ancho físico del papel.
2. **Altura (`height`):**
   - Una altura de `50px` a `70px` permite que pistolas lectoras y escáneres omnidireccionales lean el código al primer intento sin necesidad de apuntar con precisión milimétrica.

---

## 3. Catálogo de Ejemplos

### Ejemplo 1: Código de Barras con Texto Inferior (Estándar)
```json
{
  "type": "barcode",
  "symbology": "CODE128",
  "value": "ORD-0009842",
  "align": "center",
  "height": 60,
  "width": 2,
  "hri": "below"
}
```

### Ejemplo 2: Código de Barras con Texto Superior
```json
{
  "type": "barcode",
  "symbology": "CODE128",
  "value": "7751234567890",
  "align": "center",
  "height": 50,
  "width": 2,
  "hri": "above"
}
```

### Ejemplo 3: Solo Barras sin Texto (`hri: "none"`)
```json
{
  "type": "barcode",
  "symbology": "CODE128",
  "value": "SEC-88231",
  "align": "center",
  "height": 45,
  "width": 2,
  "hri": "none"
}
```
