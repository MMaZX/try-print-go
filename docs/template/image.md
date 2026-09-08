# Bloque de Imagen / Logo (`type: "image"`)

El bloque `image` permite estampar logotipos, gráficos o firmas comerciales en el ticket. El motor binariza la imagen a un mapa de bits monocromático (blanco y negro puro) de alta definición y la despacha al cabezal térmico.

---

## 1. Esquema del Bloque

```json
{
  "type": "image",
  "data": "iVBORw0KGgoAAAANSUhEUgAAAMgAAADICAYAAACtWK6eAAA...",
  "align": "center",
  "width": 200,
  "dither": false
}
```

| Campo | Tipo | Requerido | Default | Descripción |
|---|---|---|---|---|
| `type` | `string` | Sí | `"image"` | Identificador del tipo de bloque. |
| `data` | `string` | Sí | `""` | Imagen codificada en Base64. Admite strings con o sin prefijo `data:image/png;base64,`. Formatos recomendados: PNG o JPG de alto contraste. |
| `align` | `string` | No | `"center"` | Alineación horizontal de la imagen en el ticket: `"left"`, `"center"`, `"right"`. |
| `width` | `integer` | No | Ancho nativo | Ancho en píxeles al que se escalará la imagen. Si se omite o es `0`, usa el ancho natural de la imagen (acotado al ancho imprimible del papel). |
| `dither` | `boolean` | No | `false` | Algoritmo de binarización: `false` (umbral fijo por defecto, ideal para logotipos vectoriales nítidos) o `true` (dithering de difusión de error, recomendado para fotografías o imágenes con degradados). |

---

## 2. Recomendaciones de Preparación de Imágenes

1. **Resolución y Tamaño:**
   - Para papel de **80 mm**, un ancho de logotipo entre **`180px` y `320px`** ofrece un encuadre visual óptimo y balanceado.
   - Para papel de **58 mm**, se recomienda un ancho de **`120px` a `220px`**.
2. **Fondo Blanco Puro:**
   - Asegúrate de que el fondo del logo sea blanco (`#FFFFFF`) o transparente. Los fondos grises claros pueden convertirse en puntos negros indeseados en el papel térmico.
3. **Alto Contraste:**
   - Las impresoras térmicas solo imprimen en blanco y negro (sin escala de grises real). Los logotipos en color negro sólido sobre fondo blanco ofrecen la máxima nitidez.

---

## 3. Ejemplos de Uso

### Ejemplo 1: Logotipo Centrado con Ancho Controlado
```json
{
  "type": "image",
  "data": "iVBORw0KGgoAAAANSUhEUgAAAI...",
  "align": "center",
  "width": 180
}
```

### Ejemplo 2: Gráfico / Foto con Dithering Activado
```json
{
  "type": "image",
  "data": "data:image/png;base64,iVBORw0KGgo...",
  "align": "center",
  "width": 250,
  "dither": true
}
```
