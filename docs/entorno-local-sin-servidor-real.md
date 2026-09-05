# Entorno local / offline (sin servidor real ni internet)

## Por qué

Epic 5 (resiliencia offline) agrega: config cacheada para arranque sin conexión (5.1), reintentos
controlados en fallos de impresión (5.2) y una prueba end-to-end del escenario caída/reconexión (5.3).
Para verificar este comportamiento **no hace falta** el servidor real de producción
(`wss://app.usqay.com/ws`) ni conexión a internet — de hecho la propia historia 5.3 exige que la prueba
corra aislada, sin backend real. Este documento explica las dos formas de reproducir eso localmente.

## Estado actual (sprint-status.yaml)

| Historia | Estado |
|---|---|
| 5.1 — Configuración cacheada para arranque offline | `done` |
| 5.2 — Reintentos controlados en fallos de impresión | `done` |
| 5.3 — Test end-to-end del escenario caída/reconexión | `review` |

La 5.3 está **implementada y en verde**, no pendiente de código: `go test ./...` pasa en ambos módulos
(`usqay-print-client/internal/ws` y `usqay-print-server/internal/ws`), incluye
`TestE2E_DropAndReconnect` y `TestHub_HandleSync_ReflectsPrintedState`, y `go vet`/`go build` están
limpios. `review` significa que falta el paso manual de code-review de la historia (marcar `done` en
`sprint-status.yaml`), no que falte trabajo de implementación.

Build verificado en este entorno (Linux, sin cross-compile):

```bash
cd usqay-print-server && go build ./cmd/server   # OK
cd usqay-print-client && go build ./cmd/client   # OK
```

## Opción 1 (recomendada) — la prueba automatizada ya ES el entorno local/offline

No se necesita levantar ningún binario a mano. `TestE2E_DropAndReconnect`
(`usqay-print-client/internal/ws/e2e_dropreconnect_test.go`) ya:

- Levanta un servidor WebSocket falso en memoria (`httptest.NewServer`), sin red externa.
- Levanta una "impresora" falsa con `net.Listen("tcp", "127.0.0.1:0")` que solo descarta bytes.
- Corta la conexión a propósito y verifica que el job pasa a `PRINTED` en SQLite **sin** conexión activa.
- Reconecta y verifica que el `sync` real llega al servidor.

```bash
cd usqay-print-client
go test -v -run TestE2E_DropAndReconnect ./internal/ws

cd ../usqay-print-server
go test -v -run TestHub_HandleSync ./internal/ws
```

Esto es determinista, no depende de hardware ni de internet, y es la forma más rápida de comprobar
que el punto 5 funciona. Para un smoke manual de extremo a extremo con los binarios reales, usa la
Opción 2.

## Opción 2 — levantar server + client reales, 100% local

### 1. Servidor local

El servidor **no tiene modo sin autenticación ni fallback local**: `laravel_base_url` es obligatorio
(`Config.Validate()` corta el arranque si falta) y toda validación de token — de agente o de monitor —
pasa siempre por Laravel. No hay forma de levantarlo "a secas" sin un Laravel real o un mock.

Para probar el flujo de impresión completo local necesitas un mock mínimo de ese endpoint. Un
`http.server` de una sola ruta alcanza (no se commitea, es solo para pruebas manuales):

```python
# mock_laravel.py — python3 mock_laravel.py
from http.server import BaseHTTPRequestHandler, HTTPServer
import json

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({
            "valid": True,
            "terminal_id": "caja-local",
            "business_id": "local-dev",
            "printers": [{"id": "impresora-local", "tipo": "RED", "addr": "127.0.0.1:9100", "mode": "escpos"}],
        }).encode())

HTTPServer(("127.0.0.1", 8000), Handler).serve_forever()
```

Con el mock corriendo, `usqay-print-server/config.json`:

```json
{
  "port": 8080,
  "log_level": "debug",
  "laravel_base_url": "http://127.0.0.1:8000",
  "internal_token": "local-test-token"
}
```

Arrancar:

```bash
cd usqay-print-server
go build -o /tmp/usqay-print-server ./cmd/server
/tmp/usqay-print-server
```

### 2. Impresora fake (sin hardware, regla del proyecto: nunca imprimir físico en Linux)

```bash
nc -l -k 127.0.0.1 9100 > /dev/null
```

Esto simula la impresora RED de `127.0.0.1:9100` del mock de arriba: acepta bytes ESC/POS y los
descarta, igual que hace el test automatizado.

### 3. Cliente local

`usqay-print-client/config.json` (`terminal_id` es opcional del lado cliente — solo llave de caché
offline; la identidad real la resuelve el server contra el mock de Laravel de arriba, vía el token):

```json
{
  "server_url": "ws://127.0.0.1:8080/ws/agent",
  "token": "cualquier-valor",
  "log_level": "debug"
}
```

```bash
cd usqay-print-client
go build -o /tmp/usqay-print-client ./cmd/client
/tmp/usqay-print-client
```

### 4. Disparar un job

```bash
curl -X POST http://127.0.0.1:8080/api/v1/jobs \
  -H "X-Internal-Token: local-test-token" \
  -H "Content-Type: application/json" \
  -d '{
    "job_id": "job-local-1",
    "business_id": "local-dev",
    "terminal_id": "caja-local",
    "impresora_name_id": "impresora-local",
    "tipo": "RED",
    "documento_slug": "comanda",
    "payload": {"options":{"cut":false,"drawer":false},"margins":{"ancho_dimension":80.0},"body":[{"type":"text","value":"PRUEBA LOCAL"}]}
  }'
```

### 5. Reproducir el escenario de caída/reconexión a mano

1. Con el cliente corriendo y el job recién enviado, revisa `usqay-print-client/agent.db` (o los logs)
   para confirmar `PENDING`.
2. Mata el proceso del servidor (`Ctrl+C` o `kill`) — el cliente sigue vivo, el worker sigue corriendo.
3. Confirma en logs que el job pasó a `PRINTED` **sin** conexión activa (Autoridad de Impresión Local).
4. Vuelve a levantar el servidor (mismo puerto). El cliente reconecta solo (backoff desde 1s) y envía
   `sync` — verifica en el log del servidor o en `GET /api/v1/agents/caja-local/status` que el job
   figura como impreso.

```bash
curl -H "X-Internal-Token: local-test-token" "http://127.0.0.1:8080/api/v1/agents/caja-local/status?business_id=local-dev"
```

## Notas

- Nada de esto toca `wss://app.usqay.com/ws` ni requiere internet: server, mock de Laravel e impresora
  fake corren todos en `127.0.0.1`.
- Para impresión física real (layout, corte, cajón) sigue aplicando la regla del proyecto: se prueba en
  la VM Windows, nunca en Linux — ver [`entorno-pruebas-windows.md`](entorno-pruebas-windows.md).
- El mock de Laravel de este documento es solo para pruebas manuales locales; no reemplaza ni modifica
  el contrato real de `/api/tenant/print-configuration/agents/validate`.
