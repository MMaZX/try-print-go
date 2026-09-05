# Recarga de configuración de impresoras en caliente

Este documento explica cómo el binario `usqay-print-client` detecta que su pila de
impresoras cambió en el servidor y cómo recarga la configuración sin reiniciarse
**ni cortar su conexión WebSocket**.

---

## El problema

El agente arranca y recibe del servidor la lista de impresoras asignadas a ese terminal
(`TypeConfig`). Esa lista queda en memoria en el `Registry`. Si un administrador cambia
la configuración en el panel (agrega una impresora, cambia la IP, reasigna terminales,
edita una regla de ruteo), el agente no se entera hasta que se reinicie manualmente.

La solución es que el servidor empuje la config nueva al agente en tiempo real, sobre
la misma conexión WebSocket ya abierta — sin pedirle que se desconecte.

> **Nota histórica:** la primera versión de este mecanismo hacía que el agente cerrara
> su WebSocket y volviera a conectarse para recargar la config. En la práctica, Laravel
> dispara un refresh por cada mutación de negocio (crear/editar/borrar una regla de
> ruteo, un filtro, o el estado de una impresora) — no agrupados — así que un operador
> configurando varias reglas seguidas para el mismo terminal producía varias
> reconexiones en pocos minutos, cada una con su propio backoff. Se corrigió para que
> el refresh sea **no disruptivo**: nunca toca el socket.

---

## Flujo completo

```
Admin panel
    │ guarda cambio en BD (impresoras/reglas del terminal caja-01)
    │
    ▼
Laravel
    │ POST /api/v1/agents/caja-01/config-refresh?business_id=...
    │
    ▼
Servidor (usqay-print-server) — hub.RefreshAgentConfig
    │ localiza la conexión activa de caja-01 en el Hub
    │ vuelve a llamar a Laravel: POST /agents/validate (mismo token del agente)
    │ arma ConfigMsg con la lista de impresoras fresca
    │ client.Send(ConfigMsg{...})  ← sobre el WS ya abierto, sin cerrarlo
    │
    ▼
Cliente (usqay-print-client) — connection.go
    │ dispatch() recibe TypeConfig como cualquier otro mensaje
    │ applyConfig() detecta que ya tenía terminalID (isRefresh = true)
    │ Registry.Clear() + Set() — mutex-protegido, seguro aunque el Worker
    │ esté imprimiendo en paralelo
    │ emite log banner [CONFIG] WARN visible
    │
    ▼
Worker sigue corriendo sin interrupción, ahora con el Registry actualizado
    Los trabajos PENDING en cola se procesan en el próximo tick (500ms)
    con las impresoras nuevas — no hubo ventana sin conexión.
```

La conexión WebSocket **nunca se cierra** en este flujo. No hay backoff, no hay
re-registro, no hay reenvío de `SyncMsg`. El único WS que sigue existiendo para forzar
una reconexión real es el `TypeKick` (desplazar una conexión duplicada del mismo
terminal — ver regla 4 de `CLAUDE.md`), que es un caso distinto y sigue cerrando el
socket a propósito.

---

## Qué ve el operador en el log

Inicio normal (primera conexión):
```
15:04:05 INFO  [SERVER]  impresora registrada  id=uuid-1 tipo=RED addr=192.168.1.10:9100
15:04:05 INFO  [SERVER]  configuración aplicada  terminal_id=caja-01 impresoras=1
```

Cuando el servidor empuja un refresh (sin reconexión):
```
15:10:23 WARN  [CONFIG]  ▶ CONFIGURACIÓN ACTUALIZADA POR EL SERVIDOR ◀  terminal_id=caja-01
15:10:23 INFO  [SERVER]  impresora registrada  id=uuid-2 tipo=RED addr=192.168.1.20:9100
15:10:23 WARN  [CONFIG]  ▶ REFRESH COMPLETADO ◀  terminal_id=caja-01 impresoras_activas=2
```

No aparece ningún `WebSocket desconectado, reintentando` — la sesión sigue siendo la
misma de punta a punta.

---

## Implementación

### Servidor

- `usqay-print-server/internal/ws/client.go` — `Client` guarda el `token` recibido en
  el registro (`c.token = reg.Token`). El método `Client.RefreshConfig()` reutiliza
  `validateWithLaravel(c.token)` (la misma llamada que ya se hace al conectar) para
  traer la lista de impresoras vigente, valida que la identidad del terminal no haya
  cambiado, y hace `c.Send(ConfigMsg{...})` sobre la conexión existente.
- `usqay-print-server/internal/ws/hub.go` — `Hub.RefreshAgentConfig(businessID, terminalID)`
  busca el `Client` conectado y llama a `client.RefreshConfig()`. Devuelve `false` (y
  loguea el detalle) si el terminal no está conectado, el token ya no es válido, o
  Laravel no responde.
- `usqay-print-server/cmd/server/main.go` — `POST /api/v1/agents/{terminal_id}/config-refresh`
  expone `Hub.RefreshAgentConfig` sin cambios de contrato HTTP.

### Cliente

- `usqay-print-client/internal/ws/connection.go` — `applyConfig()` decide si el
  `TypeConfig` recibido es la config inicial o un refresh en vivo mirando si
  `c.terminalID` ya estaba seteado (`isRefresh := c.terminalID != ""`), en vez de
  depender de una señal explícita previa. Ya no existe el mensaje de wire
  `config_refresh` ni el campo `pendingRefresh` — el servidor manda `TypeConfig`
  directo, como cualquier actualización de config.

---

## Garantías durante el refresh

| Situación | Comportamiento |
|---|---|
| Hay trabajos PENDING en cola cuando llega el refresh | El Worker sigue corriendo sobre la misma conexión; no hay ventana de Registry vacío como con el reconnect viejo — `Clear()`+`Set()` es atómico bajo el mutex de `Registry`. |
| Laravel no responde o el token ya no es válido durante el refresh | `RefreshConfig()` devuelve error, se loguea del lado server, y el agente sigue operando con la config anterior (nunca se le manda nada) — no hay downtime ni config a medio aplicar. |
| Llegan varios refresh seguidos (varias mutaciones de Laravel en poco tiempo) | Cada uno es independiente y no disruptivo — en el peor caso el agente re-valida contra Laravel varias veces seguidas, pero la conexión y el Worker nunca se enteran. |
| El terminal no está conectado cuando Laravel llama a config-refresh | `RefreshAgentConfig` devuelve `false` de inmediato (no hay a quién mandarle nada). Al conectar, el agente recibe la config vigente en el `ConfigMsg` inicial del registro — siempre queda correcta. |
