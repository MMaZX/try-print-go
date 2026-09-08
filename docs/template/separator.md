# Bloque de Separador (`type: "separator"`)

El bloque `separator` dibuja una línea horizontal continua a lo largo de todo el ancho imprimible del papel. Es fundamental para delimitar secciones visuales (encabezados, items de la comanda, totales y pie de página).

---

## 1. Esquema del Bloque

```json
{
  "type": "separator",
  "character": "-"
}
```

| Campo | Tipo | Requerido | Default | Descripción |
|---|---|---|---|---|
| `type` | `string` | Sí | `"separator"` | Identificador del tipo de bloque. |
| `character` | `string` | No | `"-"` | Carácter que define el estilo de la línea divisoria. |

---

## 2. Estilos de Línea Soportados

El motor interpreta el valor de `character` para generar líneas vectoriales nítidas mediante CSS:

| Valor de `character` | Estilo Visual | Regla CSS Generada | Uso Recomendado |
|---|---|---|---|
| `"-"` o `""` (default) | **Línea Discontinua / Punteada** | `border-top: 1.5px dashed #000000;` | Separar items individuales, cabeceras de tabla o datos de mozo/mesa. |
| `"="` | **Línea Doble** | `border-top: 3px double #000000;` | Cierre del encabezado principal o inicio del total final a pagar. |
| `"_"` | **Línea Sólida Continua** | `border-top: 2px solid #000000;` | Delimitador fuerte entre el cuerpo y el pie del ticket. |

---

## 3. Ventajas frente al texto plano tradicional

A diferencia de las impresoras de texto plano que rellenan con caracteres `--------------------------------` (donde un carácter de más desborda a la siguiente línea), el bloque `separator` del motor HTML:
1. **Ajuste de ancho perfecto (100%):** Llena con precisión matemática el área imprimible del papel (tanto en 58 mm como en 80 mm) sin desbordes.
2. **Espaciado vertical balanceado:** Incluye un margen vertical predeterminado de `5px` arriba y abajo para mantener una lectura limpia y ordenada.

---

## 4. Ejemplos de Uso

### Ejemplo 1: Separador Punteado Estándar (Default)
```json
{
  "type": "separator",
  "character": "-"
}
```

### Ejemplo 2: Separador Doble para Totales
```json
{
  "type": "separator",
  "character": "="
}
```

### Ejemplo 3: Separador Sólido Continuo
```json
{
  "type": "separator",
  "character": "_"
}
```
