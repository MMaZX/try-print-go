# Recarga de configuración de impresoras en caliente

Este documento explica cómo el binario `usqay-print-client` detecta que su pila de
impresoras cambió en el servidor y cómo recarga la configuración sin reiniciarse.

---

## El problema

El agente arranca y recibe del servidor la lista de impresoras asignadas a ese terminal
(`TypeConfig`). Esa lista queda en memoria en el `Registry`. Si un administrador cambia
la configuración en el panel (agrega una impresora, cambia la IP, reasigna terminales),
el agente no se entera hasta que se reinicie manualmente.

La solución es que el servidor notifique al agente en tiempo real mediante el mensaje
`config_refresh` sobre la conexión WebSocket activa.

---

## Flujo completo

```
Admin panel
    │ guarda cambio en BD (impresoras del terminal caja-01)
    │
    ▼
Servidor (usqay-print-server)
    │ detecta el cambio y localiza la conexión activa de caja-01
    │ envía: {"type": "config_refresh"}
    │
    ▼
Cliente (usqay-print-client) — connection.go
    │ readLoop recibe "config_refresh"
    │ establece pendingRefresh = true
    │ cierra el WebSocket con StatusNormalClosure
    │
    ▼
Loop de reconexión — Run()
    │ detecta cierre, espera 1 segundo, llama connectAndServe()
    │
    ▼
Handshake de reconexión
    │ envía RegisterMsg (token, versión)
    │ envía SyncMsg (trabajos ya impresos)
    │
    ▼
Servidor responde TypeConfig con la configuración NUEVA
    │
    ▼
applyConfig() — detecta pendingRefresh == true
    │ limpia el Registry (Clear)
    │ registra impresoras nuevas
    │ emite log banner [CONFIG] WARN visible
    │ resetea pendingRefresh = false
    │
    ▼
Worker retoma procesamiento con el Registry actualizado
    Los trabajos que estaban en cola PENDING se procesan
    con las impresoras nuevas en el próximo tick (500ms)
```

---

## Qué ve el operador en el log

Inicio normal (sin refresh):
```
15:04:05 INFO  [SERVER]  impresora registrada  id=uuid-1 tipo=RED addr=192.168.1.10:9100
15:04:05 INFO  [SERVER]  configuración aplicada  terminal_id=caja-01 impresoras=1
```

Cuando el servidor envía `config_refresh`:
```
15:10:22 INFO  [SERVER]  servidor solicitó recargar configuración — reconectando
15:10:23 WARN  [CONFIG]  ▶ CONFIGURACIÓN ACTUALIZADA POR EL SERVIDOR ◀  terminal_id=caja-01
15:10:23 INFO  [SERVER]  impresora registrada  id=uuid-2 tipo=RED addr=192.168.1.20:9100
15:10:23 WARN  [CONFIG]  ▶ REFRESH COMPLETADO ◀  terminal_id=caja-01 impresoras_activas=1
```

El tag `[CONFIG]` en blanco brillante sobre fondo de terminal y el nivel `WARN` en
amarillo hacen que el bloque sea imposible de ignorar frente a los logs normales en azul.

---

## Lo que debe implementar el servidor

### 1. Detectar el cambio

El servidor necesita saber cuándo cambia la configuración de un terminal. Las opciones
más comunes son:

**Opción A — Evento de Laravel (recomendada)**

```php
// App/Listeners/NotifyPrintAgentOnPrinterChange.php

class NotifyPrintAgentOnPrinterChange
{
    public function handle(ImpresoraActualizada $event): void
    {
        $terminalId = $event->terminal->id;
        PrintHub::sendToTerminal($terminalId, ['type' => 'config_refresh']);
    }
}
```

Se dispara desde el observer del modelo o desde el controller que guarda los cambios.

**Opción B — Polling interno del servidor**

Un goroutine en el servidor consulta la BD cada N segundos y compara un hash/timestamp
de la configuración de cada terminal conectado. Si cambió, empuja `config_refresh`.
Más simple de implementar pero consume más recursos.

### 2. Enviar el mensaje al terminal correcto

El Hub del servidor ya mantiene un mapa `terminal_id → conexión`. Solo necesita:

```go
// En usqay-print-server/internal/ws/hub.go

func (h *Hub) SendToTerminal(terminalID string, msg any) error {
    h.mu.RLock()
    client, ok := h.clients[terminalID]
    h.mu.RUnlock()
    if !ok {
        return fmt.Errorf("terminal %s no conectado", terminalID)
    }
    select {
    case client.send <- msg:
        return nil
    default:
        return fmt.Errorf("canal del terminal %s lleno", terminalID)
    }
}
```

Y en el handler HTTP/endpoint que recibe la notificación de cambio:

```go
// Endpoint llamado por Laravel cuando cambia config de un terminal
func (s *Server) handleConfigChanged(w http.ResponseWriter, r *http.Request) {
    terminalID := r.URL.Query().Get("terminal_id")
    if err := s.hub.SendToTerminal(terminalID, map[string]string{
        "type": "config_refresh",
    }); err != nil {
        slog.Warn("no se pudo notificar config_refresh", "terminal_id", terminalID, "error", err)
        w.WriteHeader(http.StatusNotFound)
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

### 3. Enviar la configuración nueva al reconectar

Cuando el cliente reconecta y envía `RegisterMsg`, el servidor debe responder con el
`TypeConfig` actualizado. Este paso **ya debe funcionar hoy** si el servidor consulta
la BD en cada reconexión — no asume estado en memoria de la sesión anterior.

```go
// En el handler de RegisterMsg — usqay-print-server/internal/ws/hub.go

func (h *Hub) handleRegister(client *Client, msg RegisterMsg) {
    terminal, printers := h.db.LoadTerminalByToken(msg.Token)
    client.terminalID = terminal.ID

    // Siempre enviar la config más reciente de la BD, no cachear
    client.send <- ConfigMsg{
        Type:       TypeConfig,
        TerminalID: terminal.ID,
        Printers:   printers, // leído de BD en este momento
    }
}
```

---

## Garantías durante el refresh

| Situación | Comportamiento |
|---|---|
| Hay trabajos PENDING en cola cuando llega `config_refresh` | Se mantienen en SQLite. El Worker los retiene mientras el Registry está vacío (después de `Clear()`) y los procesa con la nueva config al reconectar. |
| La reconexión falla varias veces | `pendingRefresh` permanece `true` hasta que se reciba un `TypeConfig` exitoso. |
| Llegan dos `config_refresh` antes de reconectar | `pendingRefresh` es un `atomic.Bool`, el segundo `Store(true)` es idempotente. Sin efecto adverso. |
| El servidor envía `config_refresh` cuando el cliente está desconectado | El cliente no lo recibe. Al reconectar recibirá el `TypeConfig` actual de la BD, que ya tiene la nueva config. El banner `[CONFIG]` no se mostrará en ese caso, pero la config queda correcta. |

---

## Alternativa: polling activo desde el cliente

Si el servidor no puede implementar el push de `config_refresh` todavía, el cliente
puede solicitar una recarga periódica. Esto **no está implementado** actualmente pero
es el siguiente nivel si el push no es viable:

```
Cliente → {"type": "request_config_refresh"}  cada 5 minutos
Servidor → {"type": "config"}  con la config actual de la BD
```

Esta opción asegura que incluso sin notificación push, la config se actualiza cada
5 minutos. Se puede combinar con el push para tener cobertura completa.
