# Cambios requeridos en el backend (usqay-print-server / BD)

El cliente no puede completar este contrato solo: estos cambios del lado servidor son parte del alcance y deben coordinarse con las fases del roadmap.

## 1. Modelo de datos: perfil de impresora (habilita CAP-2/CAP-7 — Fase 1)

Persistir por impresora los campos del perfil (ver `device-profile.md`): `width_dots`, `dpi`, `char_width_dots`, `supports_cut`, `supports_drawer`, `supports_qr_native`, `supports_print_area`, `supports_raster`. Administrables desde el frontend, como hoy `dimension_papel`.

## 2. Dispatch de actualización por WebSocket (habilita CAP-7 — Fase 1)

Cada vez que una impresora se crea, actualiza o elimina en el backend, el servidor emite al terminal correspondiente un mensaje de configuración (p. ej. `type: "printer_config_updated"`) con el perfil completo. El cliente lo aplica a las siguientes impresiones sin reiniciar y lo cachea en SQLite. El cliente ya tiene un flujo de recarga de configuración en caliente; el dispatch se integra a ese flujo en lugar de crear uno nuevo.

Regla de consistencia: el perfil viaja completo en cada dispatch (no diffs), para que el estado del cliente sea siempre el último mensaje recibido.

## 3. Extensión del schema del payload: bloque `type:"image"` (habilita CAP-5 — Fase 3)

Nuevo tipo de bloque para logos en el ticket. Forma mínima:

```json
{ "type": "image", "data": "<PNG en base64>", "align": "center", "width": 300 }
```

- `data` embebido en base64 (recomendado: PNG monocromo o escala de grises) — el cliente no debe depender de descargar URLs en el momento de imprimir.
- `width` en dots, opcional; sin él, la imagen se escala al ancho imprimible del perfil.
- El formato definitivo del campo se cierra al implementar la Fase 3; los payloads sin bloques `image` no cambian.

## 4. Documentación del contrato

Actualizar `docs/print_payload_schema.md` con el bloque `image` y documentar el mensaje `printer_config_updated` en la documentación del protocolo WebSocket cuando cada pieza se implemente.
