# DeviceProfile — capacidades declarativas por impresora

Patrón adoptado de escpos-printer-db (base de perfiles compartida por python-escpos y escpos-php): la geometría y las features vienen del perfil, nunca de una fórmula global.

## Campos del perfil

| Campo | Tipo | Significado |
|---|---|---|
| `width_dots` | int | Ancho imprimible real en dots (p. ej. 384, 576) |
| `dpi` | int | Resolución del cabezal (default 203) |
| `char_width_dots` | int | Ancho de celda de carácter fuente A (default 12) |
| `supports_cut` | bool | Corte automático (`GS V`) |
| `supports_drawer` | bool | Apertura de cajón |
| `supports_qr_native` | bool | QR por comando nativo (`GS ( k`) |
| `supports_print_area` | bool | `GS L`/`GS W` respetados por el firmware |
| `supports_raster` | bool | `GS v 0` soportado |

Todo flag en `false` obliga al backend a usar su fallback (espacios para encuadre, imagen para QR, etc.). Es la mitigación contra firmware genérico con soporte parcial.

## Resolución (cascada, en este orden)

1. **Perfil del servidor** (BD, administrado desde el frontend, recibido por WebSocket y cacheado en SQLite) — gana siempre. Sin override local: la administración es 100% centralizada (ver `backend-changes.md`).
2. **Derivado del ancho declarado** en el payload: 58mm→384 dots y 80mm→576 dots como defaults exactos (valores Epson); interpolación lineal entre anclas solo para anchos custom (62, 70, 76mm…). Es el `paperGeometry` actual degradado a derivador, no a fórmula universal.
3. **Fallback**: perfil 58mm completo (el más conservador).

## Sincronización (CAP-7)

Cuando el backend crea/actualiza/elimina una impresora, emite `printer_config_updated` con el perfil completo por WebSocket; el cliente lo aplica a las siguientes impresiones sin reiniciar y actualiza su caché SQLite. Se integra al flujo de recarga en caliente ya existente.

## Regla de equivalencia (Fase 1)

Con los pasos 2-3 de la cascada, la salida debe ser byte a byte idéntica a la del renderer actual — el refactor a perfiles no cambia bytes, solo mueve la geometría a un tipo con nombre.

