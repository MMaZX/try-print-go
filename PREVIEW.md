# Guía de Previsualización Local (PREVIEW.md)

> **Validación visual rápida de tickets en Linux antes de enviar a Windows por SSH.**

Consulta el documento completo en:
👉 [`docs/PREVIEW.md`](file:///home/fulanito/development/try-print-go/docs/PREVIEW.md)

---

## Resumen Rápido

Para previsualizar cualquier ticket térmico en formato PNG sin requerir hardware físico ni la máquina virtual Windows:

```bash
cd usqay-print-client
go run ./cmd/preview_ticket <ruta_al_payload.json> [ruta_de_salida.png]
```

### Ejemplo:
```bash
cd usqay-print-client
go run ./cmd/preview_ticket dist/test_full_payload.json dist/test_preview.png
xdg-open dist/test_preview.png
```

### Extraer y Previsualizar desde SQL (`cola_impresion`):
Si tienes sentencias SQL `INSERT` de la base de datos de producción:
```bash
python3 scripts/extract_sql_payloads.py dump.sql --out dist/rest --preview
```
Esto creará automáticamente cada `cola_XX.json` formateado y su imagen `cola_XX.png`.

Para más detalles sobre el checklist de diseño, tests automatizados y cuándo migrar la prueba a la VM Windows física, revisa [`docs/PREVIEW.md`](file:///home/fulanito/development/try-print-go/docs/PREVIEW.md).
