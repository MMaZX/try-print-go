# Modo offline real: cómo enrutan los trabajos de impresión cuando no hay internet

## El problema, en una frase

Hoy, cuando hay internet, el flujo es:

```
Electron/Next (rest_web_react)  →  Laravel  →  usqay-print-server (nube)  →  WebSocket  →  usqay-print-client (POS)  →  impresora
```

Cuando **no hay internet**, Laravel deja de estar disponible, y el frontend (`lib/print-dispatch.ts`,
`electron/sync/print-retry.js`) ya asume que debe hablarle **directo** a un agente local:

```ts
const PRINT_AGENT_URL = 'http://localhost:9100/print'
```

Ese supuesto funciona perfecto si el POS que genera el pedido y la impresora están en **la misma máquina**.
Se rompe en cuanto hay **N estaciones de impresión en N máquinas distintas de la LAN** y Electron corre como
orquestador en una sola de ellas: `localhost:9100` nunca va a alcanzar la impresora de la caja de al lado.

Ese es el dilema real que hay que resolver, y las tres propuestas de abajo son tres formas distintas de
resolverlo — no son incrementales entre sí, son **alternativas**: se elige una.

## Lo que ya existe y no hay que rehacer

Investigar antes de proponer encontró más piezas ya construidas de las que parecía:

| Pieza | Dónde vive | Qué hace |
|---|---|---|
| Resiliencia offline del agente (épica 5) | `usqay-print-client` | Config cacheada en SQLite, reintentos con backoff, impresión sin conexión activa (`docs/entorno-local-sin-servidor-real.md`) |
| Outbox de pedidos/cobros/inventario | `rest_web_react/electron/sync/sync-engine.js` | Cola local en SQLite (`local_pedidos`, `local_cobros`, ...), sync contra `/tenant/sync/offline` con `idempotency_key`, ya maneja conflictos 409/422 |
| Outbox de impresión | `rest_web_react/lib/print-dispatch.ts` + `electron/sync/print-retry.js` | Cola local (`local_trabajos_impresion`), contrato `PrintJob` ya tipado, reintento cada 30s hasta 10 intentos |
| Contrato `PrintJob` | `rest_web_react/lib/print-dispatch.ts` | `job_id`, `terminal_id`, `impresora_name_id`, `documento_slug`, `payload.{options,paper_properties,body}` — compatible con lo que espera `renderer.go` del cliente Go |
| Servidor de impresión con protocolo probado | `usqay-print-server` | Register/Config/PrintJob/Sync por WebSocket, ya validado en LAN pura (ver `docs/laboratorio-offline.html`) |

La pieza que **no existe todavía** es cómo un trabajo de impresión llega desde la máquina de Electron hasta
la máquina correcta de entre las N estaciones, sin internet. Eso es lo que resuelve cada propuesta.

> Versión interactiva (resumen + las 3 propuestas por pestañas): [`tres-caminos-offline.html`](./tres-caminos-offline.html).

## Las tres propuestas

| # | Propuesta | Idea central | Esfuerzo | Cuándo conviene |
|---|---|---|---|---|
| [1](./propuesta-1-agente-http-y-registro-lan.md) | Agente HTTP + registro de IPs | Cada `usqay-print-client` abre `:9100/print` (el contrato que Electron ya asume); una tabla local mapea `terminal_id → IP` | Bajo | Locales chicos (1-3 estaciones), salir rápido a producción |
| [2](./propuesta-2-descubrimiento-mdns.md) | Descubrimiento automático (mDNS) | Cada estación se anuncia sola en la LAN (`_usqayprint._tcp`); nadie configura IPs a mano | Medio | Locales con varias estaciones, IPs por DHCP, rotación de equipos |
| [3](./propuesta-3-hub-embebido-electron.md) | Hub local embebido en Electron | Electron levanta el propio `usqay-print-server` como sidecar en la LAN; los `usqay-print-client` reconectan ahí en vez de a la nube | Medio-alto | Escala real a futuro, un solo protocolo online/offline, menos superficie de red expuesta |

## Recomendación

Para no bloquear el punto que depende de esto hoy: **Propuesta 1** como primer escalón (reutiliza el
contrato que el frontend ya tiene escrito, cambio quirúrgico en el cliente Go), con **Propuesta 3** como
destino final si el número de estaciones por local crece — comparten la mitad del trabajo (el agente Go
necesita HTTP entrante en ambas), así que empezar por la 1 no es tiempo perdido si después se migra a la 3.
La 2 se puede sumar encima de cualquiera de las otras dos el día que administrar IPs a mano se vuelva un
problema real, sin tocar el contrato de ninguna.

Cada documento incluye: diseño detallado, diagrama de flujo, qué cambia en cada repositorio, riesgos y
esfuerzo estimado.
