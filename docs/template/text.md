# Bloque de Texto (`type: "text"`)

El bloque `text` representa una línea o párrafo de texto plano con opciones de alineación horizontal, énfasis en negrita y tamaños tipográficos estándar.

Si el texto supera el ancho disponible del papel, el motor aplica ajuste de línea automático (**word-wrap**) respetando palabras completas sin cortarlas a la mitad.

---

## 1. Esquema del Bloque

```json
{
  "type": "text",
  "value": "RESTAURANTE USQAY",
  "align": "center",
  "bold": true,
  "size": "medium"
}
```

| Campo | Tipo | Requerido | Default | Descripción |
|---|---|---|---|---|
| `type` | `string` | Sí | `"text"` | Identificador del tipo de bloque. |
| `value` | `string` | Sí | `""` | Contenido textual a imprimir. Admite saltos de línea explícitos (`\n`), caracteres acentuados (`á`, `é`, `ñ`), signos de puntuación y emojis monocromáticos. |
| `align` | `string` | No | `"left"` | Alineación horizontal: `"left"` (o `"izquierda"`), `"center"` (o `"centro"`), `"right"` (o `"derecha"` / `"der"`). |
| `bold` | `boolean` | No | `false` | Activa negrita tipográfica de alto contraste (peso 800). |
| `size` | `string` | No | `"normal"` | Tamaño tipográfico del texto: `"normal"`, `"medium"`, `"double"`. |

---

## 2. Tamaños Tipográficos Disponibles (`size`)

El motor utiliza una escala tipográfica basada en la fuente monoespaciada de alta legibilidad **CaskaydiaCove**:

| Valor de `size` | Tamaño en Píxeles | Altura de Línea | Peso | Uso Recomendado |
|---|---|---|---|---|
| `"normal"` | 15 px | 1.25 | Regular / 600 | Cuerpo del ticket, datos fiscales, items secundarios, notas. |
| `"medium"` | 20 px | 1.25 | Bold / 800 | Nombre del restaurante, número de mesa/pedido, subtotales. |
| `"double"` | 28 px | 1.20 | Extra Bold / 900 | Título principal de comanda ("COMANDA ANULADA", "N° PEDIDO: 42"), total final a pagar. |

---

## 3. Comportamiento y Características Especiales

1. **Ajuste de Línea Inteligente (Word-Wrap):**
   - El texto que no quepa en el ancho del papel se envuelve en la siguiente línea respetando los límites de palabra (`overflow-wrap: break-word; word-break: normal;`).
   - Jamás fragmenta palabras cortas (como `CANT` o `S/ 50.00`) a menos que sea una cadena continua sin espacios más ancha que todo el papel.
2. **Espacios y Saltos de Línea Múltiples:**
   - Conserva los espacios exactos y saltos de línea explícitos (`\n`) gracias a `white-space: pre-wrap`.
3. **Caracteres Especiales y Tildes:**
   - Totalmente compatible con caracteres del idioma español (`Á, É, Í, Ó, Ú, ñ, Ñ, ¿, ¡, °`).

---

## 4. Catálogo de Ejemplos

### Ejemplo 1: Encabezado de Negocio con Tamaño Grande
```json
{
  "type": "text",
  "value": "LA TRANQUERA RESTOBAR",
  "align": "center",
  "bold": true,
  "size": "medium"
}
```

### Ejemplo 2: Título de Comanda Destacado (`size: "double"`)
```json
{
  "type": "text",
  "value": "COMANDA #105",
  "align": "center",
  "bold": true,
  "size": "double"
}
```

### Ejemplo 3: Párrafo de Políticas o Pie Legal
```json
{
  "type": "text",
  "value": "Gracias por su preferencia.\nConserve su comprobante para cualquier reclamo o cambio.\nEmitido por USQAY POS.",
  "align": "center",
  "bold": false,
  "size": "normal"
}
```

### Ejemplo 4: Datos Fiscales a la Izquierda
```json
{
  "type": "text",
  "value": "RUC: 20601234567\nDirección: Av. Larco 123 - Miraflores\nTeléfono: (01) 445-6789",
  "align": "left",
  "bold": false,
  "size": "normal"
}
```
