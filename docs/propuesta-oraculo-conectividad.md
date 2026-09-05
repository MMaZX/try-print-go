# Propuesta: oráculo de conectividad compartido (Electron + print client)

> **Estado: pendiente de discusión con el developer. No implementado, no aprobado.**
> Origen: sesión cruzada `front-rest-2026` (2026-09-04), a raíz de que Electron hace
> polling HTTP cada 15s contra Laravel para saber si hay internet.

## La propuesta original

Un binario Go nuevo y separado (no `usqay-print-client`), embebido en Electron con
versión pineada en el build, que actúe como único "oráculo de conectividad" local en
la máquina: el único proceso que chequea de verdad si Laravel responde, exponiendo ese
estado por socket local / named pipe / puerto localhost para que cualquier proceso de
esa máquina lo consulte en vez de reinventar su propia detección.

El motivo de fondo: hoy `usqay-print-client` (corriendo en la VM Windows del POS) no
tiene ningún switch explícito de "me quedé sin internet" — no hay manejo de ese caso.
La idea era que el print client consulte a ese mismo oráculo compartido en vez de
armar su propia lógica desde cero.

## Diagnóstico verificado sobre el código real (no supuesto)

Revisado `usqay-print-client/internal/ws/connection.go` e
`internal/localapi/server.go`:

- **Confirmado: no hay estado de conectividad expuesto.** Existe reconexión con
  backoff exponencial (1s → 60s tope, `connection.go:22,72-90`), pero ningún campo o
  endpoint que otro proceso pueda consultar. `localapi/server.go` solo expone
  `POST /print` — no hay `GET /status` ni nada similar.
- **Pero sí hay resiliencia offline implícita:** los trabajos se encolan en SQLite
  (`queue.Repository`) sin importar si el WS está caído, el worker sigue imprimiendo
  con la última config cacheada (`LoadCachedConfig`, `connection.go:296`), y al
  reconectar se sincroniza lo impreso (`buildSync`). El agente sigue funcionando sin
  internet — lo que no hace es *anunciar* que está en ese estado.

## Evaluación técnica (mi lectura, a validar con el developer)

1. **¿Es un gap real?** Depende de si el negocio necesita que *alguien* (operador,
   Laravel, Electron) se entere en tiempo real de que un POS quedó aislado. Si alcanza
   con que el agente siga imprimiendo solo, el gap es cosmético, no funcional.

2. **¿Print client debería consultar un oráculo externo, o manejar su propia
   detección?** El print client ya sabe mejor que nadie si tiene conectividad —
   mantiene la conexión WS persistente él mismo. Pedirle que en cambio le pregunte a
   un tercer proceso agrega indirección (¿qué hace si ese oráculo no arrancó
   todavía?) para algo que ya conoce de primera mano. La alternativa más simple:
   invertir la propuesta — el print client **expone** su propio estado (ej. un
   `GET /status` nuevo en el `localapi` ya existente, puerto 9100) y Electron le
   pregunta a él, en vez de que el print client dependa de un oráculo ajeno.

3. **¿Dónde debería vivir?** Si la solución termina siendo "el print client expone su
   estado", va en este repo (`try-print-go`, un endpoint más en `internal/localapi`).
   Si el developer insiste en un oráculo separado y genérico (para máquinas que no
   corren el print client), eso no tiene lugar natural en este repo.

## Pregunta abierta clave antes de diseñar nada

¿Electron y `usqay-print-client` **siempre** corren en la misma máquina física (el
POS)? Si es así, "oráculo compartido" y "el print client expone su propio estado"
terminan siendo la misma solución con menos piezas — un tercer binario sería
redundante. Si Electron puede correr en una máquina distinta a la del print client
(varias cajas, un solo print client central, por ejemplo), la respuesta cambia y un
oráculo separado por máquina vuelve a tener sentido.

## Siguiente paso

Retomar esta conversación con el developer antes de tocar código en cualquiera de los
tres repos (Electron, `try-print-go`, Laravel). No se avanza hasta que él apruebe un
diseño concreto.
