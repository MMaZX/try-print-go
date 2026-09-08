# Bloque de Columnas en Fila Única (`type: "columns"`)

El bloque `columns` permite ubicar dos o más celdas de texto independientes en una **sola línea horizontal** (lado a lado).

A diferencia de `table` (que está pensada para múltiples filas de productos y encabezados), `columns` es un primitivo de layout ligero y directo, ideal para parejas de datos como `Mesa / Mozo`, `Fecha / Hora` o `Caja / Cajero`.

---

## 1. Esquema del Bloque

```json
{
  "type": "columns",
  "columns": [
    {
      "text": "Mesa: 05",
      "width": 0.50,
      "align": "left",
      "bold": false,
      "size": "normal"
    },
    {
      "text": "Mozo: Carlos P.",
      "width": 0.50,
      "align": "right",
      "bold": true,
      "size": "normal"
    }
  ]
}
```

---

## 2. Propiedades de las Celdas (`columns[]`)

El array `columns` contiene cada una de las columnas horizontales:

| Campo | Tipo | Requerido | Default | Descripción |
|---|---|---|---|---|
| `text` | `string` | Sí | `""` | Texto a imprimir dentro de la columna. Si no cabe, se envuelve automáticamente. |
| `width` | `number` (float) | Sí | `0` (flex: 1) | Proporción del ancho de la columna respecto al total del ticket (`0.50` = 50%, `50` = 50%). Si se omite o es `0`, se distribuye el espacio equitativamente (`flex: 1`). |
| `align` | `string` | No | `"left"` | Alineación horizontal del texto dentro de la celda: `"left"`, `"center"`, `"right"`. |
| `bold` | `boolean` | No | `false` | Activa negrita en esta celda. |
| `size` | `string` | No | `"normal"` | Tamaño tipográfico de la celda: `"normal"` (15px), `"medium"` (20px), `"double"` (28px). |

---

## 3. ¿Cuándo usar `columns` vs `table`?

| Caso de Uso | Usar `columns` | Usar `table` |
|---|:---:|:---:|
| Fila simple de datos de cabecera (`Mesa`, `Mozo`, `Fecha`, `Hora`) | ✅ Recomendado | Excesivo |
| Lista de N items de comandas o productos con cantidades y precios | No | ✅ Recomendado |
| Filas con encabezados fijos (`CANT`, `PRODUCTO`, `TOTAL`) | No | ✅ Recomendado |
| Notas o modificadores con ancho completo (`merge: true`) | No | ✅ Recomendado |
| Distribución rápida de 2 o 3 textos en una sola línea | ✅ Recomendado | Posible |

---

## 4. Catálogo de Ejemplos Prácticos

### Ejemplo 1: Mesa y Mozo (50% / 50%)
```json
{
  "type": "columns",
  "columns": [
    { "text": "Mesa: TERRAZA-02", "width": 0.50, "align": "left" },
    { "text": "Mozo: Roberto",     "width": 0.50, "align": "right" }
  ]
}
```

### Ejemplo 2: Fecha y Hora (60% / 40%)
```json
{
  "type": "columns",
  "columns": [
    { "text": "Fecha: 12/09/2026", "width": 0.60, "align": "left" },
    { "text": "Hora: 14:35",       "width": 0.40, "align": "right" }
  ]
}
```

### Ejemplo 3: Tres Columnas (Caja, Turno y Ticket)
```json
{
  "type": "columns",
  "columns": [
    { "text": "Caja: 01",     "width": 0.33, "align": "left" },
    { "text": "Turno: Tarde", "width": 0.33, "align": "center" },
    { "text": "Tkt: #0045",   "width": 0.34, "align": "right", "bold": true }
  ]
}
```

### Ejemplo 4: Clave-Valor con Tamaño Destacado
```json
{
  "type": "columns",
  "columns": [
    { "text": "PEDIDO:", "width": 0.30, "align": "left", "size": "medium", "bold": true },
    { "text": "# 88",    "width": 0.70, "align": "right", "size": "double", "bold": true }
  ]
}
```
