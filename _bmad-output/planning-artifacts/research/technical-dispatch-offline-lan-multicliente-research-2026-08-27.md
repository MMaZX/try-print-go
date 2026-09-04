---
tipo: investigacion-tecnica
tema: dispatch offline-first en LAN, cliente multi-conexión, TUI de estado
fecha: 2026-08-27
autor: investigación técnica (agente)
estado: borrador para revisión
relacionado:
  - docs/propuesta-offline/index.md
  - docs/propuesta-offline/propuesta-1-agente-http-y-registro-lan.md
  - docs/propuesta-offline/propuesta-2-descubrimiento-mdns.md
  - docs/propuesta-offline/propuesta-3-hub-embebido-electron.md
  - _bmad-output/planning-artifacts/epics-offline.md
  - docs/entorno-local-sin-servidor-real.md
---

# Investigación: dispatch de impresión offline-first en LAN, cliente multi-conexión y TUI de estado

> Convención de este documento: cada afirmación con respaldo lleva su URL inline. Lo marcado como
> **[Inferencia]** es análisis propio del investigador aplicado al contexto de USQAY, no algo citado de una
> fuente. Lo marcado como **[Documentado]** proviene de la fuente citada.

---

## Resumen ejecutivo

### Veredicto (punto 8, primero de todo)

**Un solo binario multi-rol (orquestador de LAN + servicio de impresión + multi-servidor saliente + TUI)
es viable y está alineado con cómo lo resuelven los productos de referencia, pero el rol de "hub/orquestador
de LAN" debe poder activarse/desactivarse por configuración y no acoplarse a Electron.** Toast, TouchBistro,
Lightspeed y SambaPOS convergen en el mismo patrón: **un dispositivo cableado de la LAN asume el rol de hub
local y retransmite** trabajos entre las estaciones cuando no hay nube
([Toast](https://doc.toasttab.com/doc/platformguide/platformOfflineModeLocalSync.html),
[SambaPOS](https://kb.sambapos.com/en/2-2-2-what-is-sambapos-messaging-server-how-to-install/),
[TouchBistro/Lightspeed](https://www.touchbistro.com/blog/touchbistro-vs-lightspeed/)). Ninguno expone N
endpoints HTTP por estación que un cliente deba conocer uno por uno; todos centralizan en un hub y hacen que
las estaciones hablen *con el hub*. Eso valida la **Propuesta 3** como destino arquitectónico, con dos
correcciones respecto a como está escrita hoy: (a) el hub debe ser un rol del propio `usqay-print-*` (server
o client), **no** un binario que solo vive dentro de Electron —porque si la máquina de Electron se apaga se
cae la impresión de todo el local, riesgo que la propia Propuesta 3 ya identifica—; (b) mDNS (Propuesta 2)
no es opcional a largo plazo: es el mecanismo estándar (DNS-SD) que usan AirPrint, IPP Everywhere y todos los
POS multi-estación para descubrir el hub y las estaciones sin configurar IPs, pero **tiene modos de fallo
reales en Wi-Fi** (AP/client isolation, VLANs, filtrado de multicast) que obligan a mantener siempre un
fallback de registro manual.

Orden recomendado, en una sola frase: **Propuesta 1 ahora (destraba) → Propuesta 3 como arquitectura
objetivo con el hub como rol del binario Go (no de Electron) → Propuesta 2 (mDNS) encima, como descubrimiento
del hub, con fallback manual permanente.**

### Las 3 decisiones de arquitectura más importantes que sugiere la investigación

1. **Centralizar en un hub, no en N endpoints por estación.** El orquestador (Electron/Laravel-local) le
   habla a *un* destino (`localhost` → hub), y el hub reenvía por WebSocket a la estación correcta. Es el
   modelo Toast/SambaPOS/TouchBistro y el de PrintNode/QZ Tray/Star CloudPRNT. Colapsa "dos protocolos"
   (WS nube / HTTP LAN) en uno solo y reduce la superficie de firewall a un puerto.
2. **El hub es un rol del binario Go, activable por config, con elección de hub y failover.** Reutiliza
   `usqay-print-server` (o un modo `--hub` del client) corriendo como servicio del SO en la máquina más
   estable del local. Patrón supervisor/actor en Go (`suture`) para los sub-servicios (listener entrante,
   N conexiones salientes, worker, mDNS, TUI), cada uno reiniciable de forma independiente sin tumbar el
   proceso.
3. **Descubrimiento por DNS-SD (mDNS) con fallback manual permanente y sin dependencia de que el móvil
   ejecute Go.** Los móviles/tablets son solo clientes HTTP/WS del hub (nunca corren un servidor Go
   embebido: iOS lo suspende en segundo plano). mDNS resuelve "¿dónde está el hub?"; si el multicast está
   bloqueado en la red del local, se cae a la tabla `local_estaciones` / `hub_url` fija de la Propuesta 1.

---

## Punto 1 — Dispatch offline-first en LAN: N clientes → 1 orquestador

### Cómo lo resuelven los productos reales

**Toast — "Offline Mode with Local Sync": un dispositivo cableado hace de hub.**
[Documentado] Toast designa automáticamente un dispositivo elegible de la LAN como *local hub device*, que
"retransmite actualizaciones de un dispositivo a los demás y a la nube de Toast". Elegibilidad: no ser
handheld ni Elo V1, estar conectado por Ethernet cableado, estar en la red local, y haber respondido a un
ping de Toast en los últimos 3 minutos. Toast puede reasignarlo manualmente
([doc.toasttab.com/.../platformOfflineModeLocalSync.html](https://doc.toasttab.com/doc/platformguide/platformOfflineModeLocalSync.html)).
[Documentado] Tras ~40 segundos sin conexión aparece el banner de modo offline en cada dispositivo afectado
([support.toasttab.com/.../Using-Toast-in-Offline-Mode](https://support.toasttab.com/en/article/Using-Toast-in-Offline-Mode)).
[Documentado] Límite duro de topología: **un solo hub por red; si hay múltiples subredes/VLANs la
información no se comparte entre ellas estando offline**, y "el hub solo se comunica con dispositivos de su
subred"
([doc.toasttab.com/.../platformOfflineMode.html](https://doc.toasttab.com/doc/platformguide/platformOfflineMode.html),
[.../platformOfflineModeLocalSync.html](https://doc.toasttab.com/doc/platformguide/platformOfflineModeLocalSync.html)).
[Documentado] Funciona offline: KDS, impresión de tickets, cajón, cobro con tarjeta. No funciona: kiosco,
fichaje en dispositivos separados, gift cards, lealtad. La documentación **no** describe failover si el hub
cae estando offline
([platformOfflineModeLocalSync.html](https://doc.toasttab.com/doc/platformguide/platformOfflineModeLocalSync.html)).

**SambaPOS — "Message Server": mismo patrón, explícito.**
[Documentado] El *Message Server* es una aplicación servidor que habilita la comunicación entre terminales;
"debe correr solo en la computadora principal" y "las demás computadoras se comunican a través del Message
Server". Puerto TCP por defecto 9000. Si se toma una orden para una mesa en un terminal, la información se
envía a todos los demás
([kb.sambapos.com/.../2-2-2-what-is-sambapos-messaging-server](https://kb.sambapos.com/en/2-2-2-what-is-sambapos-messaging-server-how-to-install/),
[kb.sambapos.com/.../2-1-6-how-to-run-sambapos-on-multiple-computers](https://kb.sambapos.com/en/2-1-6-how-to-run-sambapos-on-multiple-computers/)).

**TouchBistro y Lightspeed — servidor local como "cerebro".**
[Documentado] Ambos son sistemas iPad que "requieren un servidor local que actúa como el cerebro del sistema
y permite backup local y uso offline". TouchBistro: conexión cableada local que actúa como respaldo cuando
cae internet, "permitiendo que los terminales POS sigan hablándose entre sí offline". Lightspeed corre sobre
una LAN dedicada creada por un router del negocio; su "TrueSync serverless" sigue enviando comandas a cocina
y sincroniza al volver internet
([touchbistro.com/blog/touchbistro-vs-lightspeed](https://www.touchbistro.com/blog/touchbistro-vs-lightspeed/),
[k-series-support.lightspeedhq.com/.../Networking-for-Lightspeed-Restaurant](https://k-series-support.lightspeedhq.com/hc/en-us/articles/16154347413275-Networking-for-Lightspeed-Restaurant)).

**Square — el contraejemplo: NO hay dispatch entre dispositivos.**
[Documentado] El "Offline Mode" de Square es puramente **por dispositivo**: cada equipo encola sus pagos
localmente y los procesa al reconectar; "los pagos offline se almacenan localmente en el dispositivo móvil".
Un dispositivo no ve ni procesa lo de otro
([squareup.com/help/.../7777-process-card-payments-with-offline-mode](https://squareup.com/help/us/en/article/7777-process-card-payments-with-offline-mode),
[developer.squareup.com/docs/mobile-payments-sdk/android/offline-payments](https://developer.squareup.com/docs/mobile-payments-sdk/android/offline-payments)).
Límites concretos (útiles como referencia de "cuánto tiempo/monto"): tope por transacción configurable entre
1 y 50.000 USD; con Square Reader hasta 1 hora de sesión offline; hay 24 h para reconectar y subir; los pagos
expiran si no se reconecta dentro de 72 h de la transacción original; el comerciante asume el riesgo de pagos
expirados/rechazados
([squareup.com/help/.../7777](https://squareup.com/help/us/en/article/7777-process-card-payments-with-offline-mode),
[developer.squareup.com/docs/mobile-payments-sdk/ios/offline-payments](https://developer.squareup.com/docs/mobile-payments-sdk/ios/offline-payments)).

**Loyverse — también por dispositivo.**
[Documentado] Sigue registrando ventas y las guarda locales marcadas como "Unsynced" hasta que vuelve
internet; refunds y alta de clientes deshabilitados offline; los terminales de tarjeta integrados no
funcionan offline. No documenta duración máxima
([support.loyverse.com/.../3178196-offline-use-of-loyverse-pos](https://support.loyverse.com/en/articles/3178196-offline-use-of-loyverse-pos)).

### Lectura para USQAY

[Inferencia] Los dos productos que sí necesitan que *un pedido llegue a otra máquina* sin internet (Toast,
SambaPOS) y los dos que dependen de servidor local (TouchBistro, Lightspeed) usan **exactamente** el modelo
de la Propuesta 3: un hub por red que retransmite. Square/Loyverse no aplican porque su problema offline es
"guardar mi propia venta", no "mandar la comanda a la impresora de la cocina". El caso de USQAY es el de
Toast: hay que enrutar el trabajo a la estación correcta.

[Inferencia] Los límites de topología de Toast (un hub por subred, sin cruce de VLANs) son directamente
aplicables: si un local tiene la caja en una VLAN y la cocina en otra, **ninguna** de las 3 propuestas
funciona sin un reflector mDNS o ruteo explícito. Esto debe ser un requisito de instalación documentado, no
un problema de software.

[Inferencia] "Cuánto tiempo offline" en USQAY no tiene el problema financiero de Square (no se autoriza
tarjeta local), así que el límite práctico es solo el espacio en SQLite y la caducidad `expira_en` que ya
existe (épica 5). No hace falta un tope de tiempo artificial.

---

## Punto 2 — Service discovery en LAN

### mDNS / DNS-SD: el estándar de facto

[Documentado] DNS-SD sobre mDNS (RFC 6762 / 6763) es lo que usan AirPrint y **IPP Everywhere**: "para
descubrimiento, la impresora debe soportar DNS-SD"; "la impresora se anuncia vía mDNS/Bonjour y CUPS la
descubre automáticamente"; "el 98% de las impresoras vendidas hoy soportan IPP/2.0 y DNS-SD"
([wiki.debian.org/CUPSIPPEverywhere](https://wiki.debian.org/CUPSIPPEverywhere),
[pwg.org/ipp/everywhere.html](https://www.pwg.org/ipp/everywhere.html)).
Herramientas de referencia: `ippfind` localiza colas anunciadas por DNS-SD
([wiki.debian.org/CUPSDriverlessPrinting](https://wiki.debian.org/CUPSDriverlessPrinting)).

### Librerías Go

| Librería | Estado (2025) | Notas |
|---|---|---|
| `grandcat/zeroconf` | Fork de `hashicorp/mdns` creado por falta de mantenimiento del original; registra y resuelve servicios; probado en Win/macOS/Linux | [Documentado] Tiene reportes de **"dropped mdns results"** (issue #47): a veces no devuelve todos los servicios; algún usuario recomienda volver a `hashicorp/mdns` por consistencia ([github.com/grandcat/zeroconf](https://github.com/grandcat/zeroconf), [issue #47](https://github.com/grandcat/zeroconf/issues/47)) |
| `hashicorp/mdns` | Base histórica; varios usuarios lo reportan más consistente en resultados | [Documentado] `grandcat` nació como fork suyo con PRs pendientes mergeados ([grandcat/zeroconf README](https://github.com/grandcat/zeroconf/blob/master/README.md)) |
| `betamos/zeroconf` | **Activo**, última versión publicada 2025-02-07; probado Win/macOS/Linux; compatible con Avahi/Bonjour | Candidato preferente por mantenimiento reciente ([pkg.go.dev/github.com/betamos/zeroconf](https://pkg.go.dev/github.com/betamos/zeroconf)) |
| `pion/mdns` | Clasificado **inactivo** por Snyk (revisado 2025-02-11); pensado para WebRTC, no DNS-SD completo | No recomendado para este caso ([snyk.io/advisor/golang/github.com/pion/mdns](https://snyk.io/advisor/golang/github.com/pion/mdns)) |

[Inferencia] La Propuesta 2 nombra `grandcat/zeroconf`; conviene reconsiderar a favor de `betamos/zeroconf`
(más reciente) o validar `hashicorp/mdns` con una prueba de campo, dado el problema documentado de resultados
perdidos en `grandcat`. Ninguno rompe la regla "sin CGO" del proyecto: son Go puro.

### Limitaciones reales de multicast en Wi-Fi (esto es lo que hay que documentar para instaladores)

[Documentado] **Client/AP isolation**: aísla clientes del mismo AP entre sí. mDNS "siempre va a todos los
dispositivos de la misma red — es un paquete broadcast Ethernet", pero SSIDs de invitados típicamente
**bloquean el multicast UDP 5353 y activan client isolation**, con lo que el descubrimiento no ocurre sin un
gateway/reflector
([whizz-tech.com/support/printers/airprint-bonjour-mdns-blocked-vlans-fix-reflector](https://whizz-tech.com/support/printers/airprint-bonjour-mdns-blocked-vlans-fix-reflector/),
[community.ui.com/.../mdns-reflector](https://community.ui.com/questions/8e962859-3052-4286-9ce9-3334abd971ec)).
[Documentado] **Cruce de VLAN/subred**: la dirección multicast de mDNS es "administrativamente scoped", no
cruza subredes; hace falta un **mDNS reflector/repeater** (Avahi en modo reflector, `mdns-repeater`) en el
router para reflejar entre VLANs
([irq5.io/2011/01/02/mdns-repeater-mdns-across-subnets](https://irq5.io/2011/01/02/mdns-repeater-mdns-across-subnets/),
[whizz-tech.com/.../mdns-not-propagating-across-vlans-reflector-config](https://whizz-tech.com/support/printers/mdns-not-propagating-across-vlans-reflector-config/)).
[Documentado] **IGMP snooping + AP isolation estricto** a veces suprime el multicast link-local
224.0.0.251; algunos controladores convierten multicast a unicast y hay que fijar el "mDNS mode"
([community.cisco.com/.../wireless-iosxe-client-ioslation-and-multicast](https://community.cisco.com/t5/wireless/wireless-iosxe-client-ioslation-and-multicast/m-p/5073240/highlight/true)).

[Documentado] **Android NSD** tiene problemas de fiabilidad a nivel plataforma (no de librería): descubre al
principio y deja de encontrar servicios tras unos escaneos; históricamente inusable en Android 4.1–4.3; la
implementación de `NsdManager` "termina siendo poco útil"; muchos devs usan jmDNS en su lugar; los
emuladores no reciben multicast sin configuración especial
([developer.android.com/develop/connectivity/wifi/use-nsd](https://developer.android.com/develop/connectivity/wifi/use-nsd),
[justanapplication.wordpress.com/2013/10/29/...nsdmanager-considered-not-very-useful](https://justanapplication.wordpress.com/2013/10/29/service-discovery-in-android-and-ios-part-one-the-android-net-nsd-nsdmanager-class-considered-not-very-useful-at-all/),
[github.com/mozilla-mobile/fenix/issues/16835](https://github.com/mozilla-mobile/fenix/issues/16835)).
[Inferencia] iOS/Bonjour es más sólido en foreground, pero un cliente móvil no debe *depender* de mDNS para
encontrar el hub: mejor un campo "dirección del hub" configurable + mDNS como conveniencia.

### Alternativas si mDNS está bloqueado

[Inferencia, con base en las fuentes anteriores]:
- **Registro manual** (`local_estaciones` / `hub_url` fijo de la Propuesta 1): siempre disponible, cero
  dependencia de red. Debe permanecer como fallback permanente, no como escalón descartable.
- **Reserva de IP por MAC en el router** (ya sugerida en la Propuesta 1): práctica común, elimina el
  problema de DHCP sin introducir multicast.
- **Broadcast UDP propio** en un puerto fijo (el hub responde a un "hello" broadcast): más simple que
  implementar DNS-SD, pero sufre las mismas restricciones de client isolation que mDNS; no es una mejora
  real sobre mDNS, solo evita la dependencia de librería.
- **SSDP** (UPnP): mismo problema de multicast (239.255.255.250:1900), además mala reputación de seguridad;
  no aporta sobre mDNS.
- **mDNS reflector en el router** (Avahi/`mdns-repeater`/UniFi "mDNS service"): la solución correcta para
  locales con VLANs, pero es configuración de red del cliente, no algo que el software pueda garantizar.

---

## Punto 3 — Transporte para comandas en LAN

### Qué usan los productos de referencia

| Producto | Transporte local | Modelo |
|---|---|---|
| Toast local sync | Hub retransmite a dispositivos de la subred | push desde hub ([doc.toasttab.com](https://doc.toasttab.com/doc/platformguide/platformOfflineModeLocalSync.html)) |
| SambaPOS | TCP al Message Server, puerto 9000 | cliente-servidor, hub retransmite ([kb.sambapos.com](https://kb.sambapos.com/en/2-2-2-what-is-sambapos-messaging-server-how-to-install/)) |
| PrintNode | Cliente de escritorio hace **polling** REST a la nube y baja los jobs | pull ([printnode.com docs](https://www.printnode.com/en/docs/gcp), [proxynodes.com/guides/receipt-printer-api](https://www.proxynodes.com/guides/receipt-printer-api)) |
| Star CloudPRNT | La impresora hace **POST poll** periódico; si hay job lo baja por GET; confirma con DELETE | pull + confirmación explícita ([star-m.jp CloudPRNT protocol guide](https://star-m.jp/products/s_print/sdk/StarCloudPRNT/manual/en/protocol-guide.html)) |
| QZ Tray | WebSocket **navegador → localhost** (Jetty embebido), puertos 8181/8182 | push local, transmisión "one-way" al spooler ([github.com/qzind/tray/wiki/Architecture](https://github.com/qzind/tray/wiki/Architecture)) |
| Epson Server Direct Print | La impresora inteligente hace **HTTP POST** periódico al web app; respuesta trae el job en ePOS-Print XML; spooler interno permite seguir aceptando jobs sin importar estado de impresora | pull + spooler ([files.support.epson.com/.../server_direct_print_um_en_revk.pdf](https://files.support.epson.com/pdf/pos/bulk/server_direct_print_um_en_revk.pdf)) |
| IPP Everywhere / CUPS | IPP (HTTP) push a la cola; descubrimiento DNS-SD | push ([wiki.debian.org/CUPSIPPEverywhere](https://wiki.debian.org/CUPSIPPEverywhere)) |

[Inferencia] Patrón dominante en middleware de impresión que atraviesa NAT/firewall (PrintNode, Star, Epson):
**el que imprime hace polling saliente**, para no exponer puertos entrantes. En LAN pura (Toast, SambaPOS,
QZ Tray) sí hay conexión entrante hacia el hub. USQAY ya tiene lo mejor de ambos: el `usqay-print-client`
abre WebSocket **saliente** al server; en LAN, ese server es el hub local. No hace falta polling.

### REST vs WebSocket vs MQTT vs gRPC para USQAY

[Inferencia, apoyada en fuentes de más abajo]:

- **WebSocket** (lo que ya existe): bidireccional, el server empuja `PrintJobMsg` y el client responde
  `received`/`sync`. Ya probado en LAN pura (`docs/laboratorio-offline.html`). **Recomendado**: es el
  transporte de la Propuesta 3 y no añade dependencias (`coder/websocket` ya está en el stack).
- **REST** (`:9100/print` de Propuestas 1/2): trivial de implementar y es el contrato que el frontend ya
  asume, pero es unidireccional (el server no puede notificar "impreso" ni "sin papel" de vuelta sin que el
  emisor haga polling). Sirve como *ingreso* al hub, no como transporte hub→estación.
- **MQTT con broker local**: `mochi-mqtt/server` es un broker MQTT v5 **embebible en Go puro**, throughput
  "comparable con Mosquitto"
  ([github.com/mochi-mqtt/server](https://github.com/mochi-mqtt/server)). Mosquitto en C pesa ~200 KB de
  arranque pero **no soporta multi-threading ni clustering nativo**
  ([emqx.com/.../mosquitto-mqtt-broker-pros-cons](https://www.emqx.com/en/blog/mosquitto-mqtt-broker-pros-cons-tutorial-and-modern-alternatives)).
  MQTT aporta QoS 1 (at-least-once) y retained messages "gratis", pero **introduce un broker y un modelo
  pub/sub nuevos** para un problema que hoy son <10 estaciones. [Inferencia] Sobredimensionado para USQAY
  ahora; reconsiderable si se llega a decenas de estaciones o topics de estado (impresora sin papel, cajón
  abierto) que hoy no existen.
- **gRPC**: streams bidireccionales tipados, pero sobre HTTP/2 y con toolchain de protobuf; [Inferencia]
  más fricción que WebSocket para un payload JSON que ya está definido (`PrintJob`) y sin ganancia clara en
  LAN.

### Cola outbox, idempotencia, at-least-once + dedupe

[Documentado] El patrón **outbox transaccional** resuelve el "dual write": en una sola transacción local se
persiste el cambio de estado *y* el registro de evento a enviar. Garantiza **at-least-once**, no
exactly-once: un relay puede publicar y morir antes de anotar que lo hizo, y reenviar al reiniciar
([milanjovanovic.tech/blog/implementing-the-outbox-pattern](https://milanjovanovic.tech/blog/implementing-the-outbox-pattern),
[docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html](https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html)).
[Documentado] La otra mitad es la **idempotency key**: cada evento lleva un ID único; el consumidor
comprueba una tabla de control antes de procesar; si el ID ya está, descarta el duplicado; si no, procesa e
inserta el ID **en la misma transacción** que su cambio de estado. "At-least-once en el productor +
procesamiento idempotente en el consumidor = effectively-once de punta a punta"
([event-driven.io/en/outbox_inbox_patterns_and_delivery_guarantees_explained](https://event-driven.io/en/outbox_inbox_patterns_and_delivery_guarantees_explained/),
[systemoverflow.com/.../idempotency-at-least-once-delivery-and-the-outbox-inbox-pattern](https://www.systemoverflow.com/learn/design-fundamentals/communication-patterns/idempotency-at-least-once-delivery-and-the-outbox-inbox-pattern)).

[Inferencia] USQAY **ya implementa las dos mitades**, solo hay que no romperlas al meter el hub:
- Productor: `rest_web_react/electron/sync/sync-engine.js` con `local_trabajos_impresion` +
  `idempotency_key` (según `docs/propuesta-offline/index.md`).
- Consumidor: el `usqay-print-client` inserta en `print_jobs` con `job_id` como PK y estados
  `PENDING/PROCESSING/PRINTED/ERROR` (project-context.md, regla 3). Insertar por `job_id` **es** el dedupe:
  un `INSERT OR IGNORE` sobre PK ya existente descarta el duplicado.
- El "relay" es el ciclo de reintento de `print-retry.js` (cada 30 s, hasta 10 intentos) y el `sync` al
  reconectar (épica 5, story 5.3).
- **Punto de atención con el hub**: al agregar el salto Electron→hub→estación hay **dos** tramos
  at-least-once. El `job_id` debe propagarse **sin cambiar** por los dos tramos para que el dedupe en la
  estación siga funcionando. El hub no debe generar IDs nuevos.

---

## Punto 4 — Go en móvil

### Estado de `gomobile bind` (2025–2026)

[Documentado] `gomobile bind` genera bindings (Java/Kotlin para Android, Objective-C para
Apple) y compila una librería/XCFramework; requiere Go 1.16+; para targets Apple debe correrse en macOS con
Xcode
([pkg.go.dev/golang.org/x/mobile/cmd/gomobile](https://pkg.go.dev/golang.org/x/mobile/cmd/gomobile),
[go.dev/wiki/Mobile](https://go.dev/wiki/Mobile)).
[Documentado] Limitaciones: "solo un subconjunto de tipos Go está soportado"; "los bindings tienen overhead
de rendimiento"; solo se pueden pasar primitivos y `[]byte` entre Go y la capa móvil (structs complejos →
Protocol Buffers, con coste extra); en Android los callbacks Go→Java corren en un thread pool de workers, no
en el UI thread
([go.dev/wiki/Mobile](https://go.dev/wiki/Mobile),
[igorsteblii.medium.com/go-golang-and-gomobile-vs-android-kotlin-and-ios-swift](https://igorsteblii.medium.com/go-golang-and-gomobile-vs-android-kotlin-and-ios-swift-599469d7e74a)).
[Documentado] El overhead "no se considera crítico" para lógica de negocio; el caso de uso fuerte es
**compartir modelo de datos y lógica** entre un backend Go y la app
([rbaconsulting.com/blog/how-gomobile-bridges-golang-and-native-mobile](https://www.rbaconsulting.com/blog/how-gomobile-bridges-golang-and-native-mobile-for-high-performance-apps/)).
[Inferencia] El proyecto `golang.org/x/mobile` sigue vivo pero en modo mantenimiento bajo: la wiki no
declara "activamente desarrollado", el tracker acumula issues antiguos abiertos
([goissues.org/golang.org/x/mobile](https://goissues.org/golang.org/x/mobile)). Es usable para librerías,
no es una plataforma de apps de primera clase.

### Por qué un servidor Go embebido NO sobrevive en iOS

[Documentado] iOS **suspende** la app poco después de pasar a segundo plano; la suspensión "impide que el
proceso ejecute cualquier código". Mantener un socket server vivo en background solo es posible para tipos
de app concretos (navegación, audio); "los límites para ejecución en background no acotada son
extremadamente estrictos". Las notificaciones en background despiertan la app como mucho 1–2 veces por hora,
~10 s cada vez
([developer.apple.com/forums/thread/750136](https://developer.apple.com/forums/thread/750136),
[appsonair.com/blogs/background-execution-limits-in-ios](https://www.appsonair.com/blogs/background-execution-limits-in-ios-what-every-developer-must-know),
[developer.apple.com/forums/thread/124737](https://developer.apple.com/forums/thread/124737)).

### Conclusión del punto 4

[Inferencia] **El móvil no debe correr Go ni actuar de servidor/hub.** Un iPad de mesero que pasa a
background deja de escuchar en segundos. El rol de hub va en una máquina de escritorio/mini-PC siempre
encendida y enchufada a corriente y Ethernet — exactamente el criterio de elegibilidad de Toast (device
cableado, no handheld)
([doc.toasttab.com/.../platformOfflineModeLocalSync.html](https://doc.toasttab.com/doc/platformguide/platformOfflineModeLocalSync.html)).
Los móviles/tablets son **solo clientes HTTP/WS** que le hablan al hub. Esto encaja con que USQAY ya tiene
el orquestador en Electron (escritorio), no en móvil.

---

## Punto 5 — El cliente como servicio multi-conexión

### Requisito

Un mismo proceso debe: (a) **aceptar** muchas conexiones LAN entrantes (estaciones/POS que le mandan
trabajos cuando actúa de hub), y (b) **mantener** varias conexiones **salientes** simultáneas a múltiples
servidores (nube multi-sucursal + hub local), con failover y prioridad.

### Patrón supervisor/actor en Go

[Documentado] `thejerf/suture` implementa **árboles de supervisión estilo Erlang** en Go idiomático: se
crean `Service`s que implementan una interfaz y se `.Add()` a un `Supervisor`; los supervisores son también
servicios, así que se anidan; cuando un servicio "crashea" (devuelve error), el supervisor lo reinicia. Es
"industrial-strength, testeado, desplegado en entornos hostiles"
([github.com/thejerf/suture](https://github.com/thejerf/suture),
[pkg.go.dev/github.com/thejerf/suture/v4](https://pkg.go.dev/github.com/thejerf/suture/v4),
[jerf.org/iri/post/2930](https://www.jerf.org/iri/post/2930/)).
[Inferencia] Encaja perfecto con un proceso multi-rol: cada sub-servicio (listener HTTP/WS entrante, cada
conexión saliente, worker de impresión, anunciante mDNS, TUI) es un `Service` bajo un `Supervisor` raíz.
Si la conexión a la nube de la sucursal 2 entra en loop de error, se reinicia sola sin tumbar el listener
entrante ni la impresión local. Es la forma idiomática de contener el "riesgo de proceso multi-rol".

### Connection manager con failover / prioridad / health-check

[Documentado] Patrón de referencia — `failoverconnector` de OpenTelemetry Collector: `priority_levels` en
configuración 1..n; `retry_interval` para reintentar reconectar con niveles de mayor prioridad; enruta según
salud del downstream
([pkg.go.dev/github.com/open-telemetry/opentelemetry-collector-contrib/connector/failoverconnector](https://pkg.go.dev/github.com/open-telemetry/opentelemetry-collector-contrib/connector/failoverconnector)).
[Documentado] `coredns` plugin `forward`: reutiliza sockets abiertos a upstreams; **health check in-band**
que corre en loop cada 0,5 s mientras el upstream está unhealthy y para al recuperarse; opción `failover`
para pasar al siguiente upstream ante ciertos códigos de error, no solo timeout
([coredns.io/plugins/forward](https://coredns.io/plugins/forward/)).
[Documentado] Buenas prácticas de health check: cada backend en su propia goroutine (un backend lento no
retrasa a los demás), context timeout, y **transiciones con umbral** (N fallos seguidos antes de marcar
"down") para evitar flapping
([oneuptime.com/blog/post/2026-01-25-layer-7-load-balancer-health-checks-go](https://oneuptime.com/blog/post/2026-01-25-layer-7-load-balancer-health-checks-go/view)).

### Reconexión con backoff + jitter

[Documentado] Estrategia estándar: `onclose` agenda reconexión con **exponential backoff + jitter**,
empezando en ~500 ms, duplicando, con tope de 30 s. El jitter evita el "thundering herd" cuando muchos
clientes reconectan a la vez; AWS usa "full jitter". Además: heartbeat (ping cada 25–45 s; si fallan dos,
cerrar y reconectar) y **resync explícito tras cada conexión nueva** (session ID para reanudar y reenviar
lo perdido)
([aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/),
[websocket.org/guides/reconnection](https://websocket.org/guides/reconnection/),
[oneuptime.com/blog/post/2026-01-27-websocket-reconnection](https://oneuptime.com/blog/post/2026-01-27-websocket-reconnection/view)).
[Inferencia] USQAY ya tiene backoff (épica 5). Falta: (a) jitter explícito, (b) la lista ordenada
`server_urls` con preferencia por la de mayor prioridad al recuperarse (justo lo que propone la Propuesta 3,
punto 2), (c) heartbeat ya existe (el proyecto menciona heartbeat/zombie detection en CLAUDE.md).

### Riesgos de un proceso multi-rol

[Inferencia, apoyada en principios de separación de concerns
([en.wikipedia.org/wiki/Single-responsibility_principle](https://en.wikipedia.org/wiki/Single-responsibility_principle),
[en.wikipedia.org/wiki/Separation_of_concerns](https://en.wikipedia.org/wiki/Separation_of_concerns))]:
- **Fallo compartido**: un panic no contenido en el rol hub mata también la impresión local de esa máquina.
  Mitigación: supervisión `suture` + la regla del proyecto de "worker nunca hace panic".
- **Acoplamiento de despliegue**: actualizar el hub obliga a reiniciar el agente de impresión de esa
  máquina. Mitigación: sub-servicios reiniciables in-process; y que el hub sea *opcional por config*, no
  siempre activo.
- **Superficie de seguridad**: abrir un listener entrante en cada instalación aunque no sea hub. Mitigación:
  el listener entrante solo se activa si `role: hub` en `config.json`.
- **Observabilidad**: mezclar logs de 5 roles. Mitigación: logging estructurado con campo `component`
  (slog, ya en el stack).
- **Contención de recursos**: la TUI redibujando y el hub sirviendo a la vez. Ver punto 6.

---

## Punto 6 — TUI de estado de servicios

### bubbletea / lipgloss

[Documentado] Bubble Tea usa **un event loop de un solo hilo** para las actualizaciones de estado, combinado
con ejecución concurrente de `Cmd` en goroutines; esto da I/O concurrente "sin locks ni mutexes en el código
de la aplicación". Los `Msg` (cualquier tipo) son el resultado de I/O (tecla, tick, respuesta de servidor)
y se identifican con un type switch; `eventLoop()` los procesa **de a uno, en orden serial estricto**
([github.com/charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea),
[deepwiki.com/charmbracelet/bubbletea/5.1-concurrency-and-goroutines](https://deepwiki.com/charmbracelet/bubbletea/5.1-concurrency-and-goroutines)).

### Patrón de actualización concurrente desde goroutines

[Documentado] La forma correcta de "empujar" datos desde una goroutine (p. ej. un health-check de conexión)
al modelo es **enviar un `Msg` custom** al programa; trabajar *con* el ciclo `Update > View > Update`, no
contra él. `p.Send(msg)` desde fuera, o un `Cmd` que hace la lectura y devuelve el `Msg`
([m3talsmith.medium.com/handling-polling-in-bubbletea-for-go](https://m3talsmith.medium.com/handling-polling-in-bubbletea-for-go-b17185835549),
[deepwiki.com/charmbracelet/bubbletea/5.1-concurrency-and-goroutines](https://deepwiki.com/charmbracelet/bubbletea/5.1-concurrency-and-goroutines)).

### Ejemplos reales de dashboards de servicios en TUI

[Documentado] **k9s** procesa eventos en una goroutine que los aplica a un árbol de recursos en memoria y
dispara re-render; **contrapunto operativo**: si la tasa de ingestión supera la de render (limitada por la
velocidad del terminal y el timer de refresco), el buffer del canal interno se llena y la goroutine del
watcher se bloquea en el send, dejando de consumir del stream — un patrón de fallo a tener en cuenta
([perun.au/insights/k9s-production](https://perun.au/insights/k9s-production/)).
[Documentado] **lazydocker** es una TUI de Docker en Go (usa `gocui`, no bubbletea)
([brianlovin.com/hn/36778905](https://brianlovin.com/hn/36778905)).
[Documentado] **easydocker** es una TUI inspirada en lazydocker y k9s construida **con bubbletea**
([github.com/joao-zanutto/easydocker](https://github.com/joao-zanutto/easydocker)).
[Inferencia] El patrón común: goroutine(s) de sondeo → canal/`Msg` → modelo → `View`. Para USQAY el modelo
mostraría: estado de cada `server_url` (conectado/reconectando/caído + último ping), estaciones LAN
descubiertas por mDNS y "vistas hace X", cola `print_jobs` por estado, y si el rol hub está activo.

### TUI + servicio de red en el mismo binario: ¿cuándo sí, cuándo separar?

[Inferencia, apoyada en
[procmux](https://github.com/napisani/procmux) y
[TUIOS](https://deepwiki.com/Gaurav-Gosain/tuios) que separan daemon y frontend vía IPC/Unix socket, y en
el modelo de servicio de Windows]:
- **Separar** cuando el servicio corre como **servicio de Windows / systemd** sin sesión interactiva
  (lo normal en producción: el agente arranca con la máquina, sin que nadie inicie sesión). Una TUI no
  tiene TTY ahí. El daemon expone un socket/endpoint de estado y la TUI es un binario/subcomando aparte
  (`usqay-print-client tui`) que se conecta a ese endpoint. Es además el patrón `lazydocker`/`k9s`: la TUI
  es un cliente de una API, no vive dentro del servicio.
- **Mismo proceso con flag `--tui`** solo para **desarrollo/diagnóstico en foreground** (equivalente al
  modo `-insert` que ya tiene el client): útil para el "smoke manual" del `docs/entorno-local-sin-servidor-real.md`.
- Riesgo de mismo proceso en prod: la TUI bloqueada redibujando puede robar CPU al hub (patrón k9s de
  buffer lleno); y si la TUI hace panic por un terminal raro, se lleva el servicio.

**Recomendación**: TUI como **subcomando cliente** del binario (comparte código, proceso separado), que lee
un endpoint de estado local (`GET /status` en `127.0.0.1`, que el agente ya expone algo parecido:
`/api/v1/agents/{id}/status` en el server). No meter bubbletea dentro del servicio de red de producción.

---

## Punto 7 — Middleware de impresión de referencia

| Sistema | LAN | Nube | Offline | Múltiples orígenes | Arquitectura |
|---|---|---|---|---|---|
| **PrintNode** | Cliente local imprime vía drivers del SO | API REST en la nube | Cola en la nube; el cliente baja cuando puede | API JSON, cualquier app | Agente de escritorio (Win/mac/Linux) que hace **polling saliente** y baja jobs; nube nunca empuja directo ([printnode.com/en/docs/gcp](https://www.printnode.com/en/docs/gcp), [proxynodes.com/guides/receipt-printer-api](https://www.proxynodes.com/guides/receipt-printer-api)) |
| **QZ Tray** | WebSocket navegador→`localhost` (Jetty embebido, 8181/8182) | No (es local) | N/A (la web debe estar cargada) | Cualquier web en esa máquina | Servicio local que **firma y envía RAW al spooler**; transmisión one-way ([github.com/qzind/tray/wiki/Architecture](https://github.com/qzind/tray/wiki/Architecture), [qz.io/docs/print-server](https://qz.io/docs/print-server)) |
| **Star CloudPRNT** | La impresora hace POST poll a un server (puede ser LAN o nube) | Igual, misma mecánica | El server encola; la impresora confirma con DELETE tras imprimir | El server agrega jobs de varias fuentes a la **cola por impresora** | **Pull** desde la impresora + confirmación explícita (POST poll → GET job → DELETE) ([star-m.jp CloudPRNT protocol guide](https://star-m.jp/products/s_print/sdk/StarCloudPRNT/manual/en/protocol-guide.html), [.../server-polling-post/polling-timing.html](https://star-m.jp/products/s_print/sdk/StarCloudPRNT/manual/en/protocol-reference/http-method-reference/server-polling-post/polling-timing.html)) |
| **Epson Server Direct Print** | Impresora inteligente (TM-i/TM-DT) POST periódico al web app | Igual | **Spooler interno**: acepta el siguiente job sin importar el estado de la impresora; forward printing | El web app decide qué responder a cada inquiry | **Pull** (HTTP POST inquiry → respuesta con ePOS-Print XML) + spooler en la impresora ([files.support.epson.com/.../server_direct_print_um_en_revk.pdf](https://files.support.epson.com/pdf/pos/bulk/server_direct_print_um_en_revk.pdf)) |
| **CUPS / IPP Everywhere** | IPP (HTTP) a la cola local; descubrimiento **DNS-SD** | IPP sobre internet posible | Cola local de CUPS retiene jobs | Cualquier app que hable IPP | **Push** a la cola + spooler de CUPS; sin drivers, capacidades por consulta IPP ([wiki.debian.org/CUPSIPPEverywhere](https://wiki.debian.org/CUPSIPPEverywhere), [pwg.org/ipp/everywhere.html](https://www.pwg.org/ipp/everywhere.html)) |

### Patrones transversales

[Inferencia sobre la tabla]:
1. **Todos tienen un agente local** en la máquina/impresora que finalmente imprime. Ninguno confía en que la
   nube "llegue" a la impresora. Es exactamente la "Autoridad de Impresión Local" de USQAY.
2. **Los que cruzan internet usan pull (polling saliente)** para evitar puertos entrantes (PrintNode, Star,
   Epson). Los que son puramente LAN usan push/WebSocket entrante (QZ Tray, CUPS). USQAY con el hub local
   está en el caso LAN → WebSocket entrante al hub es correcto.
3. **Cola + confirmación explícita de impresión** es universal (Star DELETE, Epson spooler, CUPS job states,
   USQAY `PRINTED` + `sync`). No asumir impreso por haber entregado.
4. **El descubrimiento estándar es DNS-SD/mDNS** (CUPS/IPP Everywhere, AirPrint). Valida la Propuesta 2 como
   dirección correcta, no como lujo.

---

## Punto 8 — Veredicto: ¿un binario multi-rol o separar orquestador/agente?

### Evidencia acumulada

- **A favor de centralizar en un hub** (no N endpoints por estación): Toast, SambaPOS, TouchBistro,
  Lightspeed — los cuatro productos que necesitan enrutar trabajo entre máquinas offline — usan un hub por
  red ([Toast](https://doc.toasttab.com/doc/platformguide/platformOfflineModeLocalSync.html),
  [SambaPOS](https://kb.sambapos.com/en/2-2-2-what-is-sambapos-messaging-server-how-to-install/),
  [TouchBistro/Lightspeed](https://www.touchbistro.com/blog/touchbistro-vs-lightspeed/)). Es la Propuesta 3.
- **A favor de un proceso multi-rol bien supervisado**: `suture` demuestra que árboles de supervisión
  estilo Erlang en Go son "industrial-strength" y permiten contener el fallo de un sub-servicio sin tumbar
  el resto ([github.com/thejerf/suture](https://github.com/thejerf/suture)). El propio USQAY ya corre
  `conn.Run` + `worker.Run` como goroutines coordinadas en un proceso.
- **En contra de meter TODO en un proceso**: k9s muestra el patrón de fallo de mezclar ingestión de eventos
  y render en el mismo runtime ([perun.au/insights/k9s-production](https://perun.au/insights/k9s-production/));
  lazydocker/k9s mantienen la TUI como cliente de una API, no dentro del servicio. iOS suspende cualquier
  servidor embebido ([developer.apple.com/forums/thread/750136](https://developer.apple.com/forums/thread/750136)) —
  el hub nunca puede vivir en un móvil.
- **En contra de acoplar el hub a Electron** (como está escrita hoy la Propuesta 3): la propia propuesta lo
  marca como riesgo "Alto" — si la máquina de Electron se apaga, se cae la impresión de todas las
  estaciones. Toast lo resuelve **eligiendo automáticamente** un device cableado elegible y pudiendo
  reasignarlo
  ([doc.toasttab.com/.../platformOfflineModeLocalSync.html](https://doc.toasttab.com/doc/platformguide/platformOfflineModeLocalSync.html)).

### Veredicto

**Sí a un binario Go multi-rol, con el rol de hub como modo opcional y desacoplado de Electron; no a un
"mega-proceso" que incluya la TUI de producción.** Concretamente:

- **Un binario, varios roles activables por `config.json`**: `agente` (siempre), `hub` (opcional, solo en la
  máquina designada), `salientes múltiples` (siempre, con lista `server_urls` priorizada). Sub-servicios
  bajo un supervisor tipo `suture`, cada uno reiniciable.
- **La TUI es un subcomando cliente** (`usqay-print-client tui` / `status`) que lee un endpoint local de
  estado, no un módulo dentro del servicio de red. En dev se permite `--tui` en foreground.
- **El hub es `usqay-print-server` reutilizado** (protocolo Register/Config/PrintJob/Sync ya probado en
  `docs/laboratorio-offline.html`), corriendo como servicio del SO en la máquina más estable — no como
  `spawn` de Electron. Electron/Laravel-local le habla por `localhost` como ya prevé la Propuesta 3, pero
  el ciclo de vida del hub no depende de Electron.
- **mDNS** (Propuesta 2) se usa para descubrir el hub y las estaciones, **con `local_estaciones` /
  `hub_url` fijo como fallback permanente** por los modos de fallo de multicast en Wi-Fi.

### Encaje con las 3 propuestas existentes

| Propuesta | ¿La investigación la valida? | Rol en la hoja de ruta |
|---|---|---|
| **1 — Agente HTTP + registro de IPs** | Parcial. El endpoint `:9100/print` es el contrato que el frontend ya asume y es el camino más rápido a producción, pero "N endpoints que el emisor debe conocer uno a uno" **no** es como lo hace ningún POS de referencia. | **Escalón 1 (ahora)**: destraba el trabajo dependiente. El servidor HTTP entrante que agrega al client **se reutiliza** como vía de ingreso al hub en la Propuesta 3, así que no es tiempo perdido. Mantener la tabla `local_estaciones` como fallback para siempre. |
| **2 — Descubrimiento mDNS** | Sí como dirección (DNS-SD es el estándar: CUPS/IPP Everywhere, AirPrint, todos los POS multi-estación). Con reservas: `grandcat/zeroconf` tiene resultados perdidos documentados (evaluar `betamos/zeroconf` o `hashicorp/mdns`); multicast se rompe con AP isolation/VLANs; Android NSD es poco fiable. | **Escalón 3 (encima de la 3)**: mDNS descubre *el hub* (un solo servicio a anunciar), no N estaciones. Fallback manual permanente. Requisito de red (sin client isolation en la VLAN del POS, o reflector mDNS) documentado para instaladores. |
| **3 — Hub local embebido** | **Sí, es el destino.** Es literalmente el modelo Toast "local hub device" / SambaPOS "Message Server". Un protocolo online/offline, un puerto de firewall, cola y confirmación de impresión ya probadas. | **Escalón 2 (arquitectura objetivo)**, con **una corrección**: el hub NO se empaqueta como sidecar de Electron ni depende de su ciclo de vida. Es un servicio del SO en la máquina más estable, con capacidad futura de que otra máquina asuma el rol (elección de hub estilo Toast). El punto 2 de la propuesta (`server_urls` como lista priorizada con failover + jitter) se implementa tal cual. |

**Orden**: 1 → 3 (con hub desacoplado de Electron) → 2. La 2 puede adelantarse solo para "descubrir el hub"
si el registro manual del `hub_url` genera fricción antes de que la 3 esté lista.

---

## Tabla comparativa de opciones de arquitectura

### Topología

| Opción | Pros | Contras | Esfuerzo | Referencia externa |
|---|---|---|---|---|
| **N endpoints HTTP por estación + tabla de IPs** (Prop. 1) | Mínimo cambio; contrato ya asumido por el frontend; sin dependencias nuevas; debug trivial ("mirá la tabla") | Emisor debe conocer N destinos; IP se mantiene a mano (DHCP); 2 protocolos (WS nube / HTTP LAN); N puertos en el firewall; sin canal de vuelta (impreso/sin papel) | **Bajo** (1 archivo Go + 1 línea + 1 tabla SQLite) | Ninguno de los POS de referencia lo hace así |
| **Hub local por red** (Prop. 3) | 1 destino para el emisor; 1 protocolo online/offline; 1 puerto de firewall; cola + confirmación ya probadas; features nuevas del protocolo funcionan igual online/offline | Trabajo de resolución de impresoras sin Laravel; si el hub cae, cae la impresión del local (mitigable con elección de hub); empaquetado/servicio del SO | **Medio-alto** (server: fuente de config sin Laravel ~1-2 d; client: `server_urls` con failover ~1 d; despliegue como servicio) | **Toast local hub**, **SambaPOS Message Server**, TouchBistro/Lightspeed server local |
| **Hub embebido en Electron** (Prop. 3 tal cual) | Reusa `spawn` que Electron ya hace; cero despliegue extra | **Ciclo de vida atado a Electron**: se apaga la caja, se cae todo; Electron no está en la máquina más estable necesariamente | Medio | — (es una variante frágil del anterior) |
| **Malla P2P entre estaciones (sin hub)** | Sin punto único de fallo | Complejidad de consenso/enrutamiento; nadie en el sector lo hace para esto; multicast igual de frágil | Alto | — |

### Transporte hub ↔ estación

| Opción | Pros | Contras | Esfuerzo | Referencia |
|---|---|---|---|---|
| **WebSocket** (ya existe) | Bidireccional; ya probado en LAN; sin dependencias nuevas; empuja job y recibe `sync` | Requiere conexión entrante al hub (OK en LAN) | **Bajo** (ya está) | QZ Tray (WS local), USQAY hoy |
| **REST/HTTP** (`:9100/print`) | Trivial; contrato ya escrito | Unidireccional; polling para estado | Bajo | PrintNode, Star, Epson (pero para *cruzar internet*, no LAN) |
| **MQTT broker local** (`mochi-mqtt`) | QoS1 at-least-once y retained "gratis"; desacopla emisor/consumidor; Go puro embebible | Broker y modelo pub/sub nuevos; sobredimensionado para <10 estaciones | Medio | — (común en IoT/KDS genérico) |
| **gRPC** | Streams tipados bidireccionales | Toolchain protobuf; sin ganancia clara sobre WS en LAN con payload JSON ya definido | Medio | — |

### Discovery

| Opción | Pros | Contras | Esfuerzo | Referencia |
|---|---|---|---|---|
| **mDNS / DNS-SD** | Estándar (RFC 6762/6763); cero config de IPs; lo usan AirPrint/IPP Everywhere/POS multi-estación | Se rompe con AP/client isolation, VLANs, filtrado multicast; Android NSD poco fiable; libs Go con matices de mantenimiento | Medio (lib + anuncio + descubrimiento + fallback) | CUPS/IPP Everywhere, AirPrint |
| **Registro manual / IP fija por MAC** (Prop. 1) | Cero dependencia de red; siempre funciona; debug simple | Trabajo operativo; frágil ante cambio de hardware | Bajo | SambaPOS (se configura la IP del Message Server a mano) |
| **Broadcast UDP propio** | Sin dependencia de librería DNS-SD | Mismas restricciones de multicast que mDNS; reinventa DNS-SD peor | Medio | — |
| **SSDP/UPnP** | Estándar viejo | Mismo problema multicast; mala fama de seguridad | Medio | — |
| **mDNS reflector en el router** (Avahi/mdns-repeater/UniFi) | Resuelve VLANs | Es config de red del cliente, no del software | N/A (documentación) | Práctica estándar AirPrint entre VLANs |

---

## Recomendación final priorizada

1. **Ahora — Escalón 1 (Propuesta 1, tal cual):** agregar `internal/localapi` con `POST /print` al
   `usqay-print-client` (ya iniciado según `git status`), tabla `local_estaciones`, `resolveAgentUrl()` en
   `print-dispatch.ts`. Regla de firewall en el instalador NSIS. Esto destraba el trabajo dependiente esta
   semana. **Diseñar el handler HTTP factorizando `handlePrintJob`** para que HTTP y WS llamen la misma
   función interna — así el mismo endpoint sirve luego de ingreso al hub. Preservar `job_id` como PK
   (dedupe).
2. **Siguiente — Escalón 2 (Propuesta 3 con hub desacoplado):**
   - `usqay-print-server` gana fuente de resolución de impresoras sin Laravel (config estática primero,
     caché automática después — punto 3 de la propuesta).
   - `usqay-print-client`: `server_urls` como **lista priorizada** con failover, backoff **+ jitter**,
     preferencia por la de mayor prioridad al recuperarse (patrón OpenTelemetry `failoverconnector` /
     coredns `forward`).
   - El hub corre como **servicio del SO** (Windows service / systemd) en la máquina designada del local,
     **no** como `spawn` de Electron. Electron le habla por `localhost:8080` (`POST /api/v1/jobs`, que ya
     existe).
   - Sub-servicios bajo supervisor tipo `suture` (o equivalente casero): listener entrante, cada conexión
     saliente, worker, futuro anunciante mDNS.
3. **Después — Escalón 3 (Propuesta 2, acotada a descubrir el hub):**
   - Anunciar **un** servicio `_usqayprint._tcp` desde el hub; los agentes y el orquestador lo descubren.
   - Librería: evaluar `betamos/zeroconf` (activo, 2025) o `hashicorp/mdns` frente a `grandcat/zeroconf`
     (resultados perdidos documentados). Go puro, sin CGO.
   - **`hub_url` fijo en `config.json` permanece como fallback** cuando el multicast está bloqueado.
   - Documentar requisito de red para instaladores: la VLAN del POS y la de las estaciones deben verse por
     multicast, o el router debe tener reflector mDNS.
4. **Transversal — TUI:** subcomando `usqay-print-client status`/`tui` con bubbletea que consume un endpoint
   local `GET /status`; goroutines de sondeo → `Msg` custom → modelo. **No** incrustar bubbletea en el
   servicio de red de producción (que corre sin TTY como servicio del SO).
5. **Transversal — no tocar:** el móvil/tablet sigue siendo cliente HTTP/WS del hub; nunca corre Go ni actúa
   de servidor (iOS lo suspende en background).

### Preguntas abiertas para el equipo (regla de colaboración del proyecto)

- ¿Los locales objetivo tienen VLANs separadas para caja/cocina? Define si mDNS necesita reflector desde el
  día 1.
- ¿Qué máquina del local es "la más estable" para hospedar el hub? ¿Hay un mini-PC/servidor o siempre es
  una caja/tablet?
- ¿Se quiere elección automática de hub (estilo Toast) o basta con designarlo por `config.json` en la
  instalación?
- ¿El rango de estaciones por local es 1-3, 3-10 o más? Define si MQTT llega a tener sentido.

---

## Fuentes

### Productos POS / KDS offline (punto 1)
- Toast — Offline mode overview: https://doc.toasttab.com/doc/platformguide/platformOfflineMode.html
- Toast — Offline mode with local sync: https://doc.toasttab.com/doc/platformguide/platformOfflineModeLocalSync.html
- Toast — Using Toast in Offline Mode (support): https://support.toasttab.com/en/article/Using-Toast-in-Offline-Mode
- Toast — Offline payments overview: https://doc.toasttab.com/doc/platformguide/platformOfflinePaymentsOverview.html
- SambaPOS — What is the Messaging Server / How to install: https://kb.sambapos.com/en/2-2-2-what-is-sambapos-messaging-server-how-to-install/
- SambaPOS — How to run on multiple computers: https://kb.sambapos.com/en/2-1-6-how-to-run-sambapos-on-multiple-computers/
- SambaPOS — Message Server tutorial (foro): https://forum.sambapos.com/t/message-server-tutorial/1042
- TouchBistro vs Lightspeed (2026): https://www.touchbistro.com/blog/touchbistro-vs-lightspeed/
- Lightspeed — Networking for Lightspeed Restaurant (K-Series): https://k-series-support.lightspeedhq.com/hc/en-us/articles/16154347413275-Networking-for-Lightspeed-Restaurant
- Square — Process card payments with Offline Mode: https://squareup.com/help/us/en/article/7777-process-card-payments-with-offline-mode
- Square — Offline Payments (Mobile Payments SDK, iOS): https://developer.squareup.com/docs/mobile-payments-sdk/ios/offline-payments
- Square — Offline Payments (Mobile Payments SDK, Android): https://developer.squareup.com/docs/mobile-payments-sdk/android/offline-payments
- Square — Brings Offline Payments to All Hardware Devices (press): https://squareup.com/us/en/press/square-brings-offline-payments
- Loyverse — Offline use of Loyverse POS: https://support.loyverse.com/en/articles/3178196-offline-use-of-loyverse-pos

### Service discovery / mDNS (punto 2)
- grandcat/zeroconf (repo): https://github.com/grandcat/zeroconf
- grandcat/zeroconf — issue #47 "Dropped mdns results": https://github.com/grandcat/zeroconf/issues/47
- grandcat/zeroconf — README (origen como fork de hashicorp/mdns): https://github.com/grandcat/zeroconf/blob/master/README.md
- betamos/zeroconf (pkg.go.dev, activo 2025): https://pkg.go.dev/github.com/betamos/zeroconf
- pion/mdns — Snyk health (inactivo): https://snyk.io/advisor/golang/github.com/pion/mdns
- Android — Use network service discovery: https://developer.android.com/develop/connectivity/wifi/use-nsd
- "NsdManager considered not very useful at all": https://justanapplication.wordpress.com/2013/10/29/service-discovery-in-android-and-ios-part-one-the-android-net-nsd-nsdmanager-class-considered-not-very-useful-at-all/
- Fenix — Network Service Discovery not supported (issue): https://github.com/mozilla-mobile/fenix/issues/16835
- mdns-repeater: mDNS across subnets: https://irq5.io/2011/01/02/mdns-repeater-mdns-across-subnets/
- Whizz — AirPrint blocked across VLANs / Bonjour reflector: https://whizz-tech.com/support/printers/airprint-bonjour-mdns-blocked-vlans-fix-reflector/
- Whizz — mDNS not propagating across VLANs (reflector config): https://whizz-tech.com/support/printers/mdns-not-propagating-across-vlans-reflector-config/
- Whizz — Guest Wi-Fi blocks printer (mDNS forwarding & L2 isolation): https://whizz-tech.com/support/printers/guest-wifi-blocks-printer/
- Ubiquiti community — mDNS reflector in an isolated network: https://community.ui.com/questions/8e962859-3052-4286-9ce9-3334abd971ec
- Cisco community — client isolation and multicast: https://community.cisco.com/t5/wireless/wireless-iosxe-client-ioslation-and-multicast/m-p/5073240/highlight/true

### Transporte / outbox / idempotencia (punto 3)
- mochi-mqtt/server (repo): https://github.com/mochi-mqtt/server
- EMQ — Mosquitto MQTT broker: pros/cons y alternativas: https://www.emqx.com/en/blog/mosquitto-mqtt-broker-pros-cons-tutorial-and-modern-alternatives
- Milan Jovanović — Implementing the Outbox Pattern: https://milanjovanovic.tech/blog/implementing-the-outbox-pattern
- AWS Prescriptive Guidance — Transactional outbox pattern: https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html
- Event-Driven.io — Outbox, Inbox patterns and delivery guarantees: https://event-driven.io/en/outbox_inbox_patterns_and_delivery_guarantees_explained/
- System Overflow — Idempotency, at-least-once, outbox/inbox: https://www.systemoverflow.com/learn/design-fundamentals/communication-patterns/idempotency-at-least-once-delivery-and-the-outbox-inbox-pattern

### Go en móvil (punto 4)
- Go Wiki: Mobile: https://go.dev/wiki/Mobile
- gomobile command (pkg.go.dev): https://pkg.go.dev/golang.org/x/mobile/cmd/gomobile
- golang.org/x/mobile — Go Issues (tracker): https://goissues.org/golang.org/x/mobile
- Igor Steblii — Golang + gomobile vs nativo: https://igorsteblii.medium.com/go-golang-and-gomobile-vs-android-kotlin-and-ios-swift-599469d7e74a
- RBA — How GoMobile bridges Golang and native mobile: https://www.rbaconsulting.com/blog/how-gomobile-bridges-golang-and-native-mobile-for-high-performance-apps/
- Apple Developer Forums — keep a socket server alive in background: https://developer.apple.com/forums/thread/750136
- AppsOnAir — iOS Background Execution Limits (2026): https://www.appsonair.com/blogs/background-execution-limits-in-ios-what-every-developer-must-know
- Apple Developer Forums — Keep NetService connection alive when device is locked: https://developer.apple.com/forums/thread/124737

### Cliente multi-conexión / supervisor / failover (punto 5)
- thejerf/suture (repo): https://github.com/thejerf/suture
- suture v4 (pkg.go.dev): https://pkg.go.dev/github.com/thejerf/suture/v4
- Jerf — Suture: Supervisor Trees for Go: https://www.jerf.org/iri/post/2930/
- OpenTelemetry Collector — failoverconnector: https://pkg.go.dev/github.com/open-telemetry/opentelemetry-collector-contrib/connector/failoverconnector
- CoreDNS — forward plugin (health checks, failover): https://coredns.io/plugins/forward/
- OneUptime — Layer 7 load balancer with health checks in Go: https://oneuptime.com/blog/post/2026-01-25-layer-7-load-balancer-health-checks-go/view
- AWS Architecture Blog — Exponential Backoff and Jitter: https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/
- WebSocket.org — Reconnection: State Sync and Recovery Guide: https://websocket.org/guides/reconnection/
- OneUptime — WebSocket reconnection logic: https://oneuptime.com/blog/post/2026-01-27-websocket-reconnection/view

### TUI (punto 6)
- charmbracelet/bubbletea (repo): https://github.com/charmbracelet/bubbletea
- DeepWiki — Bubbletea Concurrency and Goroutines: https://deepwiki.com/charmbracelet/bubbletea/5.1-concurrency-and-goroutines
- Michael Christenson II — Handling Polling in BubbleTea for Go: https://m3talsmith.medium.com/handling-polling-in-bubbletea-for-go-b17185835549
- Perun — K9s in production: five failure patterns: https://perun.au/insights/k9s-production/
- easydocker (TUI con bubbletea, inspirada en lazydocker/k9s): https://github.com/joao-zanutto/easydocker
- Lazydocker (Hacker News / brianlovin): https://brianlovin.com/hn/36778905
- procmux — TUI utility (daemon/frontend por IPC): https://github.com/napisani/procmux
- Wikipedia — Single-responsibility principle: https://en.wikipedia.org/wiki/Single-responsibility_principle
- Wikipedia — Separation of concerns: https://en.wikipedia.org/wiki/Separation_of_concerns

### Middleware de impresión (punto 7)
- PrintNode — Guidance for Google Cloud Print users (arquitectura cliente): https://www.printnode.com/en/docs/gcp
- Proxy Nodes — Receipt Printer API: the Complete Guide: https://www.proxynodes.com/guides/receipt-printer-api
- QZ Tray — Architecture (wiki): https://github.com/qzind/tray/wiki/Architecture
- QZ Tray — Print Server (docs): https://qz.io/docs/print-server
- Star CloudPRNT — Protocol Guide: https://star-m.jp/products/s_print/sdk/StarCloudPRNT/manual/en/protocol-guide.html
- Star CloudPRNT — Polling Timing: https://star-m.jp/products/s_print/sdk/StarCloudPRNT/manual/en/protocol-reference/http-method-reference/server-polling-post/polling-timing.html
- Epson — Server Direct Print User's Manual (Rev.K, PDF): https://files.support.epson.com/pdf/pos/bulk/server_direct_print_um_en_revk.pdf
- Epson — Server Direct Print (TM-Intelligent, overview): https://download4.epson.biz/sec_pubs/pos/reference_en/technology/epson_epos_sdk.html
- Debian Wiki — CUPS IPP Everywhere: https://wiki.debian.org/CUPSIPPEverywhere
- Debian Wiki — CUPS Driverless Printing: https://wiki.debian.org/CUPSDriverlessPrinting
- PWG — IPP Everywhere: https://www.pwg.org/ipp/everywhere.html
