# Esquema de Payload de Impresión Genérico (Print Payload Schema)

Este documento especifica la estructura JSON genérica y estrictamente tipada utilizada para representar los trabajos de impresión en la plataforma USQAY. 

El objetivo de este formato es desacoplar el agente local (`usqay-print-client`) de la lógica de negocio del backend. El backend describe el ticket utilizando bloques de diseño primitivos (texto, tablas, códigos QR, etc.), y el agente simplemente se encarga de interpretarlos y traducirlos a comandos físicos (ESC/POS o texto plano).

---

## Estructura General del JSON

El JSON de impresión consta de tres secciones principales:
1. `options`: Parámetros de control físico del ticket.
2. `margins`: Parámetros de dimensión física del papel.
3. `body`: Un array ordenado de bloques de diseño que componen el ticket.

```json
{
  "options": {
    "cut": true,
    "drawer": false
  },
  "margins": {
    "ancho_dimension": 80.0,
    "altura_dimension": 0.0
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

## 2. Márgenes (`margins`)

Define el tamaño físico del papel en milímetros.

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `ancho_dimension` | `number` (float) | Sí | Ancho del papel en mm (ej. `80.0` para papel estándar de 80mm, `58.0` para 58mm). Determina el ancho de caracteres virtuales en el agente (48 caracteres para 80mm, 32 caracteres para 58mm). |
| `altura_dimension` | `number` (float) | Sí | Altura del papel. `0.0` representa papel continuo (rollo térmico). |

---

## 3. Bloques del Cuerpo (`body`)

El cuerpo es un array de objetos JSON, donde cada objeto representa una instrucción de diseño. Todos los bloques contienen el campo `type`, que define su estructura y comportamiento.

### 3.1. Bloque de Texto (`type: "text"`)

Representa una línea de texto plano con opciones de formato.

```json
{
  "type": "text",
  "value": "TICKET DE PRUEBA",
  "align": "center",
  "bold": true,
  "size": "double"
}
```

* **`value`** (`string`, Requerido): El contenido textual a imprimir.
* **`align`** (`string`, Opcional): Alineación horizontal. Valores soportados: `"left"` (por defecto), `"center"`, `"right"`.
* **`bold`** (`boolean`, Opcional): Si es `true`, activa el modo negrita. Por defecto `false`.
* **`size`** (`string`, Opcional): Tamaño del texto. Valores soportados: 
  * `"normal"` (por defecto): Tamaño estándar.
  * `"medium"`: Altura doble.
  * `"double"`: Altura doble + Ancho doble.

---

### 3.2. Bloque de Separador (`type: "separator"`)

Dibuja una línea horizontal repetitiva a lo largo del ancho disponible del papel.

```json
{
  "type": "separator",
  "character": "-"
}
```

* **`character`** (`string`, Opcional): El caracter a repetir (ej. `"-"`, `"="`, `"*"`). Si no se especifica, por defecto es `"-"`.

---

### 3.3. Bloque de Espaciador (`type: "spacer"`)

Introduce líneas en blanco verticales para alimentar el papel.

```json
{
  "type": "spacer",
  "lines": 2
}
```

* **`lines`** (`integer`, Opcional): Número de saltos de línea a generar. Por defecto es `1`.

---

### 3.4. Bloque de Tabla Genérica (`type: "table"`)

Representa una tabla estructurada con columnas ajustables y alineaciones individuales.

```json
{
  "type": "table",
  "headers": ["CANT", "PRODUCTO", "TOTAL"],
  "widths": [0.1, 0.65, 0.25],
  "aligns": ["left", "left", "right"],
  "rows": [
    {
      "cant": 2,
      "producto": "LOMO SALTADO",
      "total": "S/ 42.00"
    },
    {
      "cant": 1,
      "producto": "COCA COLA ZERO",
      "total": "S/ 5.00"
    }
  ]
}
```

* **`headers`** (`array` of `string`, Opcional): Etiquetas para las cabeceras de columnas. Si no se especifican, se omitirá la línea de cabeceras.
* **`widths`** (`array` of `number` / floats, Requerido): Array de proporciones de ancho para cada columna. La suma debe aproximarse a `1.0` (ej: `[0.1, 0.65, 0.25]`).
* **`aligns`** (`array` of `string`, Requerido): Alineación para cada columna. Valores: `"left"`, `"center"`, `"right"`.
* **`rows`** (`array` de objetos, Requerido): Los datos de cada fila. Para resolver qué campo de cada objeto va en qué columna, el agente buscará llaves basadas en el nombre de la cabecera (mapeo semántico de campos como `cant`, `cantidad`, `producto`, `descripcion`, `total`, `precio`, etc.).

---

### 3.5. Bloque de Tabla de Comanda (`type: "table_comanda"`)

Especializado en tickets de cocina (comandas). No incluye precios y soporta observaciones/notas de preparación por plato.

```json
{
  "type": "table_comanda",
  "rows": [
    {
      "cant": 2,
      "producto": "ARROZ CHAUFA",
      "notas": "Bien tostado, sin cebollita china",
      "categoria_id": 1
    },
    {
      "cant": 1,
      "producto": "LOMO SALTADO",
      "notas": null,
      "categoria_id": 1
    }
  ]
}
```

* **`rows`** (`array` de objetos, Requerido):
  * **`cant`** (`integer`, Requerido): Cantidad del plato.
  * **`producto`** (`string`, Requerido): Nombre del plato.
  * **`notas`** (`string` o `null`, Opcional): Indicaciones especiales de preparación.
  * **`categoria_id`** (`integer`, Opcional): ID de la categoría (utilizado para el enrutamiento interno).

---

### 3.6. Bloque de Código QR (`type: "qr"`)

Imprime un código QR. En impresoras térmicas ESC/POS se renderiza nativamente, y en impresoras de texto se imprime la URL como texto encerrado entre corchetes.

```json
{
  "type": "qr",
  "value": "https://app.usqay.com/c/abc123xyz",
  "align": "center",
  "size": 6
}
```

* **`value`** (`string`, Requerido): La URL o texto a codificar en el QR.
* **`align`** (`string`, Opcional): Alineación horizontal del código QR (`"left"`, `"center"`, `"right"`).
* **`size`** (`integer`, Opcional): Tamaño del módulo del código QR en píxeles (entre `1` y `16`, por defecto es `6`).
