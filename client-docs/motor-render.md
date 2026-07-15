# Motor de Renderizado e Impresión

Este documento profundiza en la conversión de los datos de impresión desde el formato lógico JSON hasta el envío físico de bytes al hardware, explicando los tipos de impresoras, el cálculo geométrico del papel y la codificación de caracteres en español.

---

## 1. Modos de Impresión y Adaptadores de Hardware

La interfaz principal `Printer` define las capacidades mínimas de cualquier adaptador:
```go
type Printer interface {
    Print(data []byte) error
    Mode() string // Retorna "escpos" o "text"
}
```

El cliente implementa tres tipos de adaptadores físicos basados en la especificación provista por el servidor:

### A. Impresoras de Red (`NetworkPrinter`)
- **Tipo en Configuración:** `"RED"`
- **Conectividad:** Se comunica mediante sockets TCP crudos (normalmente al puerto `9100` del dispositivo).
- **Modo predeterminado:** Siempre funciona en modo `"escpos"`.
- **Implementación:** Abre una conexión TCP con un timeout de 5 segundos, escribe toda la ráfaga de bytes directamente y cierra la conexión inmediatamente para liberar el canal de la impresora.

### B. Impresoras del Sistema Operativo (`SystemPrinter`)
- **Tipo en Configuración:** `"USB"` o `"SERIE"`
- **Conectividad:** Pasa a través del subsistema de impresión nativo del sistema operativo (CUPS en Linux, Spooler de impresión en Windows).
- **Modos soportados:**
  - `"escpos"`: Envía los comandos binarios directamente a la impresora sin interferencia del driver de impresión.
  - `"text"`: Envía texto plano UTF-8 formateado para impresoras convencionales (láser o inyección de tinta).

#### Implementación en Linux (CUPS)
El cliente ejecuta llamadas al comando `lp` nativo del sistema:
* **Modo `escpos` (Raw)**: 
  ```bash
  lp -d <nombre_impresora> -o raw -
  ```
  Envía los bytes por `stdin` con el argumento `-o raw`. Esto indica a CUPS que omita toda su cadena de filtros (filtros raster, PPD, Ghostscript) y entregue los comandos ESC/POS binarios sin alterar directamente a la interfaz USB/Serie.
* **Modo `text` (Plain Text)**:
  Para evitar que el worker se congele temporalmente al imprimir en impresoras de inyección/láser, el cliente implementa una **optimización de liberación de procesos**:
  1. Escribe el contenido de texto plano en un archivo temporal (`/tmp/usqay-print-*.txt`).
  2. Ejecuta el comando redirigiendo tanto `stdout` como `stderr` a `/dev/null`:
     ```bash
     lp -d <nombre_impresora> /tmp/usqay-print-*.txt > /dev/null 2>&1
     ```
  3. Cierra y elimina el archivo temporal de inmediato.
  *¿Por qué es necesario esto?* CUPS genera múltiples procesos hijos (filtros del driver del fabricante) al procesar texto. Si Go utiliza tuberías estándar (`Stdin` o captura de salida `CombinedOutput`), el comando de Go espera a que *todos* los procesos hijos de CUPS cierren sus descriptores de archivo, lo que en impresoras convencionales tarda de 10 a 90 segundos. Redirigiendo a `/dev/null` y usando archivos físicos, el comando `lp` retorna en milisegundos apenas el spooler acepta el documento.

#### Implementación en Windows (WinSpool API)
Utiliza la API nativa de Windows `winspool.drv` mediante llamadas DLL (`alexbrainman/printer`):
* **Modo `escpos`**: Inicia el documento con el tipo de datos `"RAW"`. Los comandos ESC/POS fluyen directo al cabezal térmico.
* **Modo `text`**: Inicia el documento con el tipo de datos `"TEXT"`. El spooler del driver se encarga de rasterizar el texto plano en el papel utilizando las fuentes predeterminadas del sistema.

---

## 2. Detección Automática y Mode Hints

El cliente puede inspeccionar el sistema operativo para obtener las impresoras configuradas localmente (usado por el comando `-list` o en respuesta al mensaje `list_printers` del servidor). 
Para ayudar al usuario en la configuración, el cliente analiza el nombre y modelo de cada impresora del sistema y sugiere un `ModeHint` (`"escpos"` o `"text"`):

- Compara el nombre contra una lista de palabras clave térmicas comunes: `"tm-"`, `"tsp"`, `"star"`, `"thermal"`, `"receipt"`, `"pos-"`, `"zj-"`, `"xp-"`, `"bixolon"`, `"citizen"`, `"gprinter"`, `"rongta"`, `"escpos"`, `"58mm"`, `"80mm"`, etc.
- Aplica una expresión regular `pos[ _-]?\d` para capturar modelos genéricos clones como "POS-80" o "POS58".
- Si coincide con alguna regla, retorna `"escpos"`; de lo contrario, asume que es una impresora común de oficina y retorna `"text"`.

---

## 3. Geometría del Papel y Layout Dinámico

En impresoras térmicas, calcular el ancho exacto del papel en caracteres es vital para evitar que el texto se corte o que las columnas de las tablas se desalineen. El archivo `renderer.go` realiza un cálculo automático dinámico:

### Cálculo Geométrico Continuo (`paperGeometry`):
El cliente no encasilla el ancho en dos tamaños rígidos (58mm y 80mm). En su lugar, aplica una **interpolación lineal** a partir de dos "anclas físicas" basadas en estándares del fabricante (Epson TM):
1. **Ancla 1 (58 mm)**: Área de impresión real de **384 dots (puntos)**.
2. **Ancla 2 (80 mm)**: Área de impresión real de **576 dots (puntos)**.

Con el valor de `dimension_papel` provisto en el JSON (o fallback a `ancho_dimension`), se calcula la fracción:
$$\text{fracción} = \frac{\text{anchoMM} - 58.0}{80.0 - 58.0}$$
$$\text{dots} = 384.0 + \text{fracción} \times (576.0 - 384.0)$$

Este cálculo permite que papeles de tamaños alternativos (por ejemplo, 62mm, 76mm, 90mm) se autocalibren y aprovechen su espacio disponible de forma fluida.

### Ajuste de Caracteres:
Considerando que el tamaño estándar de la fuente A en ESC/POS es de **12 dots de ancho por carácter**, el número de columnas de texto disponibles es:
$$\text{ancho en caracteres} = \text{dots} / 12$$
- Para 80mm, el área suele ser de **48 caracteres** de ancho.
- Para 58mm, el área suele ser de **32 caracteres** de ancho.

*Margen Físico:* Si el payload define un valor de `padding` (margen físico en milímetros), este se convierte a dots ($1\text{ mm} \approx 8\text{ dots}$) y se restan simétricamente del área imprimible, ajustando el ancho útil de caracteres de manera proporcional.

---

## 4. Estructura del Renderizador (`renderer.go`)

El renderizador procesa el arreglo `body` del payload JSON secuencialmente. Admite los siguientes bloques:

| Bloque `type` | Descripción y Parámetros | Comportamiento en ESC/POS | Comportamiento en Texto Plano |
| :--- | :--- | :--- | :--- |
| **`text`** | Texto lineal. Parámetros: `value`, `align` ("center", "right"), `bold`, `size` ("normal", "medium", "double"). | Emite comandos de tamaño y alineación ESC/POS, transcodifica el texto a CP850 y escribe. Restablece el tamaño después. | Alinea agregando espacios en blanco a la izquierda del string y escribe en UTF-8. Omite estilos físicos. |
| **`separator`** | Línea horizontal divisora. Parámetros: `character` (default `"-"`). | Repite el carácter hasta rellenar el ancho de caracteres calculado. | Igual. |
| **`spacer`** | Espacio vertical. Parámetros: `lines` (default `1`). | Envía comandos de salto de línea (`\n`). | Envía saltos de línea (`\n`). |
| **`table`** | Tabla estructurada. Parámetros: `columns` (anchos porcentuales, encabezados), `rows` (celdas, bold, `merge` de fila). | **Complejo**: Si la celda requiere estilo individual (bold/tamaño), activa comandos ESC/POS inline. Si la fila tiene `merge: true`, imprime la primera celda sobre todo el ancho de la línea. | Mide caracteres y rellena con espacios según la alineación de cada columna. Remueve marcas binarias. |
| **`columns`** | Columnas simples una al lado de la otra. Parámetros: `columns` (texto, ancho porcentual, estilos). | Similar a la tabla pero simplificado en un solo elemento de línea. | Rellena espacios para cada columna. |
| **`qr`** | Código QR nativo. Parámetros: `value`, `align`, `size` (1-16 dots de módulo). | Emite la secuencia nativa ESC/POS modelo 2 de 5 pasos para generación de QR en hardware. | Imprime una etiqueta de reemplazo en texto: `[QR: <valor>]`. |
| **`barcode`** | Código de barras 1D. Parámetros: `symbology` (CODE128, EAN13, etc.), `value`, `height`, `width`, `hri` (posición del texto legible). | Emite el comando nativo `GS k` (nuevo formato) con las dimensiones y simbología indicadas. | Imprime una etiqueta de reemplazo en texto: `[BARCODE <simbología>: <valor>]`. |

---

## 5. Codificación de Texto y Transcodificación CP850 (`encoding.go`)

Las impresoras térmicas ESC/POS no interpretan UTF-8 nativo de forma generalizada. Si se envía texto codificado en UTF-8, los caracteres especiales como tildes (`á`, `é`), eñes (`ñ`, `Ñ`) y signos de puntuación de apertura (`¿`, `¡`) se imprimen como símbolos extraños o caracteres rotos.

Para solucionar esto, el cliente realiza una transcodificación por software a **CP850 (PC850 Multilingual)**:

1. **Inicialización**: Al crear un buffer de comandos (`escpos.New()`), el cliente envía el comando de inicialización de la impresora (`ESC @`) seguido de la selección de tabla de caracteres `ESC t 2`, la cual activa la página de códigos **PC850** en el firmware del hardware.
2. **Traducción de Runas**: Todos los textos enviados a través de `Builder.Text()` o `Builder.Line()` pasan por la función `encodeCP850()`.
3. **Mapeo**:
   - Todo carácter ASCII estándar (código Unicode < 128) se pasa intacto como un byte directo.
   - Todo carácter acentuado o símbolo español (ej. `'á'`, `'ñ'`, `'¿'`) se busca en una tabla estática en memoria (`cp850`) y se reemplaza por su equivalente exacto de 1 byte correspondiente a la página de códigos PC850 (ej. `'ñ'` se convierte al byte `0xA4`).
   - Los caracteres que no tengan representación en CP850 son sustituidos por un signo de interrogación `'?'` para evitar enviar bytes de control inválidos al hardware.

*¿Por qué se eligió CP850 sobre CP437?*
La página estándar estadounidense (CP437) carece de las mayúsculas acentuadas utilizadas en español (como `Á`, `É`, `Í`, `Ó`, `Ú`). La tabla CP850 cuenta con el repertorio completo de caracteres latinos occidentales, garantizando una presentación ortográfica impecable en los tickets.
