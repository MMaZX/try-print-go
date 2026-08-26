# Roadmap de implementación — 5 fases

Orden estricto 0→3; cada fase se despliega y revierte por separado (compatible con el auto-update del cliente). La Fase 4 es opcional y está fuera del contrato actual (non-goal).

| Fase | Entrega | Criterio de salida |
|---|---|---|
| **0** | Suite golden de bytes sobre el renderer actual (CAP-1) | Matriz completa versionada; CI falla ante cualquier diff de bytes |
| **1** | `DeviceProfile` + `paperGeometry` degradado a derivador (CAP-2) + sync por dispatch WebSocket con el backend (CAP-7, ver `backend-changes.md` §1-2) | Equivalencia byte a byte con Fase 0; perfil editado en backend aplica al siguiente ticket sin reiniciar |
| **2** | IR measure→arrange + backend texto emitiendo desde la IR (CAP-3, CAP-4) | Sin desbordes en la matriz golden; padding en un único punto; encuadre vía `GS L`/`GS W`; diffs golden solo intencionales |
| **3** | Backend raster selectivo (CAP-5) + bloque `type:"image"` en el payload (ver `backend-changes.md` §3) + modo captura `.prn` (CAP-6) | Bloque raster/image fiel en escpresso; tickets sin raster no cambian ni un byte; binario ≤ +3MB; nuevas deps solo `gg` y `x/image` |
| **4** *(opcional, fuera de contrato)* | Transporte TCP 9100 bidireccional + `GS I` para impresoras de red | — |

## Coordinación con el backend

- Fase 1 requiere el modelo de perfil en BD y el dispatch `printer_config_updated` (`backend-changes.md` §1-2); el derivador y el fallback del cliente no dependen del backend y pueden avanzar en paralelo.
- Fase 3 requiere el bloque `type:"image"` en el schema del payload (`backend-changes.md` §3); la captura `.prn` y el motor raster no dependen del backend.
