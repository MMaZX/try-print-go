# Bloque de Espaciador (`type: "spacer"`)

El bloque `spacer` introduce un espacio vertical en blanco en el ticket. Se utiliza para generar aire visual entre secciones o para alimentar papel al final del comprobante antes del corte físico.

---

## 1. Esquema del Bloque

```json
{
  "type": "spacer",
  "lines": 2
}
```

| Campo | Tipo | Requerido | Default | Descripción |
|---|---|---|---|---|
| `type` | `string` | Sí | `"spacer"` | Identificador del tipo de bloque. |
| `lines` | `integer` | No | `1` | Cantidad de saltos de línea (unidades `em`) de separación vertical. Si se envía `<= 0`, aplica `1`. |

---

## 2. Comportamiento

- Cada unidad de `lines` equivale a `1em` de altura en la escala tipográfica activa (aproximadamente `15px` a `18px` de espacio en blanco vertical).
- No imprime ningún carácter ni artefacto en el papel.
- Es más limpio y predecible que insertar strings vacíos `""` en bloques `text`.

---

## 3. Ejemplos de Uso

### Ejemplo 1: Salto Simple (1 línea)
```json
{
  "type": "spacer",
  "lines": 1
}
```

### Ejemplo 2: Separación Previa al Corte de Papel (2 a 3 líneas)
```json
{
  "type": "spacer",
  "lines": 2
}
```
