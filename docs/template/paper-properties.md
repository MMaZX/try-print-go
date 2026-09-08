# Propiedades de Papel y Hardware (`options` y `paper_properties`)

Esta sección define las características físicas del soporte de impresión (ancho de papel, escala de lienzo, márgenes perimetrales) y las instrucciones electromecánicas para la impresora (corte de papel y apertura de gaveta de dinero).

---

## 1. Opciones de Hardware (`options`)

El objeto `options` controla las acciones electromecánicas que se ejecutan al iniciar o finalizar el trabajo de impresión.

```json
{
  "options": {
    "cut": true,
    "drawer": false
  }
}
```

| Campo | Tipo | Requerido | Default | Descripción |
|---|---|---|---|---|
| `cut` | `boolean` | Sí | `true` | Si es `true`, ejecuta un corte de papel parcial al terminar la impresión. Si es `false`, realiza un avance de líneas para dejar el papel visible. |
| `drawer` | `boolean` | Sí | `false` | Si es `true`, envía un pulso eléctrico para abrir la gaveta/cajón de dinero antes de imprimir. |

---

## 2. Propiedades de Papel (`paper_properties`)

El objeto `paper_properties` gobierna la geometría del lienzo de impresión en milímetros y la escala óptica.

```json
{
  "paper_properties": {
    "width": 80.0,
    "scale": 1.0,
    "padding": [1.5, 1.5, 1.5, 1.5]
  }
}
```

| Campo | Tipo | Requerido | Default | Descripción |
|---|---|---|---|---|
| `width` | `number` (float) | Sí | `80.0` | Ancho físico del papel en milímetros. Estándares comunes: `80.0` (576 dots útiles) o `58.0` (384 dots útiles). Admite tamaños intermedios (ej. `70.0`, `76.0`). |
| `scale` | `number` (float) | No | `1.0` | Factor de escala visual/zoom sobre el lienzo completo (estilo viewport). Por ejemplo: `1.0` (escala normal 100%), `1.5` (zoom 150%). |
| `padding` | `array` de 4 floats | No | `[0.0, 0.0, 0.0, 0.0]` | Márgenes perimetrales en milímetros según el estándar CSS: `[top, right, bottom, left]` (arriba, derecha, abajo, izquierda). |

---

## 3. Comportamiento de la Geometría y Escala

### Conversión de Milímetros a Píxeles Térmicos (Dots)
A una resolución estándar de **203 DPI** (impresoras térmicas ESC/POS comunes):
$$1\text{ mm} \approx 8\text{ dots (píxeles)}$$
- **Papel 80 mm:** 576 dots de ancho imprimible.
- **Papel 58 mm:** 384 dots de ancho imprimible.
- **Padding:** Un margen de `1.5 mm` equivale a `1.5 × 8 = 12 px` de padding en el borde del ticket.

### Zoom de Lienzo (`scale`)
El parámetro `scale` amplía o reduce uniformemente todo el ticket sin alterar las proporciones internas de las columnas ni comprimir el ancho:
- Si se diseña un ticket para papel de **58 mm** (`width: 58.0`) y se envía con `scale: 1.5`, la salida resultante ocupará exactamente **576 px** (el ancho completo de un rollo de 80 mm) manteniendo la tipografía grande y las columnas sin desfasarse.
- Si se envía `scale: 1.0` (o se omite), el ticket se renderiza en su tamaño tipográfico nativo estándar.
- Valores `<= 0` aplican `1.0` por seguridad. El valor máximo está acotado a `5.0`.

---

## 4. Ejemplos de Configuración

### Ejemplo 1: Rollo Estándar de 80 mm con Margen Perimetral
```json
{
  "options": {
    "cut": true,
    "drawer": false
  },
  "paper_properties": {
    "width": 80.0,
    "scale": 1.0,
    "padding": [2.0, 1.5, 2.0, 1.5]
  },
  "body": []
}
```

### Ejemplo 2: Rollo Estrecho de 58 mm
```json
{
  "options": {
    "cut": true,
    "drawer": false
  },
  "paper_properties": {
    "width": 58.0,
    "scale": 1.0,
    "padding": [0.0, 0.0, 0.0, 0.0]
  },
  "body": []
}
```

### Ejemplo 3: Apertura de Gaveta y Corte (Cobro en Caja)
```json
{
  "options": {
    "cut": true,
    "drawer": true
  },
  "paper_properties": {
    "width": 80.0,
    "scale": 1.0,
    "padding": [1.0, 1.0, 1.0, 1.0]
  },
  "body": []
}
```
