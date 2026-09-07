# Enviar previews con el binario cliente

> Cómo convertir un payload JSON en una imagen PNG del ticket, sin impresora y sin
> Windows. Sirve para probar rápido layout, tablas, QR, códigos de barras y logos.

El motor de render (`internal/ticketrender`) dibuja el ticket **primero como imagen**
y de ahí deriva los bytes ESC/POS. Esa imagen se puede volcar a disco, y eso es lo
que hace el binario de preview.

---

## 1. El binario: `preview_ticket`

Vive en [`usqay-print-client/cmd/preview_ticket`](../usqay-print-client/cmd/preview_ticket/main.go).
Es un CLI independiente del agente: no abre SQLite, no conecta al servidor, no
necesita `config.json`. Solo lee un JSON y escribe un PNG.

### Ejecutar sin compilar (lo más rápido)

```bash
cd usqay-print-client
go run ./cmd/preview_ticket <payload.json> [salida.png]
```

### Compilar un binario reutilizable

```bash
cd usqay-print-client
go build -o ../build/preview_ticket ./cmd/preview_ticket

# luego, desde cualquier lado:
./build/preview_ticket payload.json salida.png
```

### Argumentos

| Posición | Valor | Obligatorio | Nota |
|---|---|---|---|
| 1 | ruta al payload JSON | Sí | El JSON del trabajo de impresión (mismo esquema que envía el servidor). |
| 2 | ruta del PNG de salida | No | Si se omite, usa el nombre del JSON con sufijo `_preview.png` en la misma carpeta. |

El ancho de papel (58 mm / 80 mm), la escala y el padding se leen del propio
payload (`paper_properties`). No hay que pasarlos por flag.

---

## 2. Prueba en 30 segundos

Creá un payload mínimo:

```bash
cat > /tmp/ticket.json <<'JSON'
{
  "options": { "cut": true, "drawer": false },
  "paper_properties": { "width": 80.0, "scale": 1.0, "padding": [1.5, 1.5, 1.5, 1.5] },
  "body": [
    { "type": "text", "value": "USQAY RESTAURANT", "align": "center", "bold": true, "size": "double" },
    { "type": "text", "value": "Comanda de prueba", "align": "center" },
    { "type": "separator", "character": "-" },
    { "type": "table",
      "columns": [
        { "header": "CANT", "width": 0.15, "align": "left"  },
        { "header": "PRODUCTO", "width": 0.60, "align": "left"  },
        { "header": "TOTAL", "width": 0.25, "align": "right" }
      ],
      "rows": [
        { "cells": [{ "text": "2" }, { "text": "Lomo Saltado" }, { "text": "42.00" }] },
        { "cells": [{ "text": "1" }, { "text": "Inca Kola 500ml" }, { "text": "4.00" }] },
        { "merge": true, "cells": [{ "text": "Nota: sin cebolla" }] }
      ]
    },
    { "type": "separator", "character": "=" },
    { "type": "qr", "value": "https://usqay.com/pedido/123", "align": "center" }
  ]
}
JSON
```

Generá el preview:

```bash
cd usqay-print-client
go run ./cmd/preview_ticket /tmp/ticket.json /tmp/ticket.png
```

Salida esperada:

```text
==================================================
🎨 RENDERIZANDO PREVIEW A IMAGEN (PNG)...
📄 Entrada : /tmp/ticket.json
🖼️  Salida  : /tmp/ticket.png
==================================================
✅ PREVIEW GENERADO EXITOSAMENTE!
⏱️  Tiempo de renderizado : 28.40 ms
📐 Dimensiones imagen    : 552 x 1366 px
📦 Tamaño del archivo PNG : 77.4 KB
🚀 Archivo listo en       : /tmp/ticket.png
==================================================
```

(Si el ticket lleva QR, códigos de barras o fuentes escaladas, verás además
líneas de log del render antes del resumen — es normal.)

Abrir la imagen:

```bash
xdg-open /tmp/ticket.png     # visor del escritorio
# o: feh /tmp/ticket.png / eog /tmp/ticket.png
```

> El esquema completo de bloques (`text`, `table`, `qr`, `barcode`, `image`,
> `columns`, `separator`, `spacer`) está en
> [`docs/print_payload_schema.md`](print_payload_schema.md).

---

## 3. Lote: previsualizar payloads reales desde SQL

Si tenés un volcado de `INSERT INTO cola_impresion` de producción, el script
[`scripts/extract_sql_payloads.py`](../scripts/extract_sql_payloads.py) extrae cada
payload y genera su PNG:

```bash
python3 scripts/extract_sql_payloads.py ruta/a/inserts.sql --out dist/rest --preview
```

- `--out <dir>`: carpeta de salida (por defecto `dist/rest`).
- `--no-preview`: solo extrae los `.json`, sin generar imágenes.

---

## 4. ¿Y el agente (`usqay-print-client`) en sí?

El binario principal **no** produce PNG: su trabajo es imprimir. Lo que sí puede
volcar para inspección es el ESC/POS crudo (`.prn`) de cada trabajo, con
`"capture_prn": true` en `config.json`; los archivos quedan en `captured_prns/`
junto al ejecutable. Eso sirve para diffs de bytes, no para revisión visual — para
lo visual, usá `preview_ticket` con el mismo payload.

---

## 5. Cuándo pasar a Windows

El preview PNG cubre todo lo visual (geometría, tipografía, tablas, QR, barcode,
logos). Solo hace falta imprimir físicamente en la VM Windows para validar
hardware: corte de papel, pulso de gaveta, calor del cabezal y sensor de fin de
papel. Ver [`docs/entorno-pruebas-windows.md`](entorno-pruebas-windows.md).

Guía más amplia del flujo de validación visual: [`docs/PREVIEW.md`](PREVIEW.md).
