# Bloque de Código QR (`type: "qr"`)

El bloque `qr` genera códigos QR vectoriales de alta precisión y lectura instantánea para facturación electrónica (SUNAT, SAT, DIAN), URLs de pago digital, comprobantes digitales o links de propina.

A diferencia de los comandos nativos de algunas impresoras clon que distorsionan o recortan el QR, el motor de **USQAY Print** procesa el QR con matrices exactas y márgenes de silencio perimetrales automáticos.

---

## 1. Esquema del Bloque

```json
{
  "type": "qr",
  "value": "https://app.usqay.com/c/f01-00042918",
  "align": "center",
  "size": 128
}
```

| Campo | Tipo | Requerido | Default | Descripción |
|---|---|---|---|---|
| `type` | `string` | Sí | `"qr"` | Identificador del tipo de bloque. |
| `value` | `string` | Sí | `""` | Contenido o URL a codificar en el código QR. (También admite `data` como alias). |
| `align` | `string` | No | `"center"` | Alineación horizontal del QR en el papel: `"center"`, `"left"`, `"right"`. |
| `size` | `integer` | No | `128` | Ancho del código QR en píxeles. (También admite `pixel_width`). Mínimo recomendado: `96px` a `120px`. |

---

## 2. Tamaños Recomendados (`size` en píxeles)

| Valor de `size` | Tamaño en Papel (80 mm) | Uso Recomendado |
|---|---|---|
| `120` | ~15 mm | Facturas electrónicas estándar y tickets compactos. |
| `160` | ~20 mm | Comprobantes para escanear con celulares de clientes (links de menú o pago). |
| `200` | ~25 mm | Códigos QR destacados para pago con billeteras digitales (Yape / Plin). |

---

## 3. Ejemplos de Uso

### Ejemplo 1: QR Estándar para Factura Electrónica
```json
{
  "type": "qr",
  "value": "20601234567|01|F001|000042|18.00|118.00|12/09/2026|6|20100000001|",
  "align": "center",
  "size": 128
}
```

### Ejemplo 2: QR Destacado para Pagos Digitales o Calificación
```json
{
  "type": "qr",
  "value": "https://pagos.usqay.com/pedido/9842",
  "align": "center",
  "size": 180
}
```
