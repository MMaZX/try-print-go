# Auto-actualización segura del binario del cliente

Este documento describe el diseño para que `usqay-print-client` pueda actualizarse
solo sin intervención del operador, con las garantías de seguridad necesarias.

---

## Por qué no implementarlo todavía

Antes de activar la auto-actualización se necesita resolver la infraestructura de
distribución. Sin ella, la funcionalidad es un vector de ataque: si alguien compromete
el servidor o el CDN puede empujar cualquier binario a todos los agentes instalados.

Los tres requisitos de infraestructura que deben existir primero:

1. **Bucket/CDN propio con HTTPS** — S3, R2, Bunny, o equivalente. URL fija y predecible.
2. **Pipeline de firma** — el binario se firma en CI con una clave privada que nunca
   sale del entorno de build.
3. **Endpoint de versión en el servidor** — el servidor conoce la versión mínima
   aceptada y la última disponible.

---

## Modelo de seguridad

### Firma con clave asimétrica (ed25519)

El checksum SHA256 solo garantiza integridad en tránsito (protege contra corrupción
accidental). No protege contra un servidor comprometido que sirva un binario malicioso
con su propio SHA256 válido.

La solución es firmar el binario con una clave privada `ed25519` que solo existe en
el pipeline de CI. El cliente valida la firma con la clave pública embebida en el
propio ejecutable en tiempo de compilación:

```go
// Embebida en tiempo de compilación via ldflags, nunca hardcodeada en código fuente.
// go build -ldflags "-X main.updatePublicKey=<base64-pubkey>" ...
var updatePublicKey string
```

La clave pública en el binario no es un secreto — si alguien la obtiene no puede
falsificar firmas porque para eso necesitaría la clave privada.

### Flujo de verificación antes de reemplazar

```
Descarga binario nuevo (HTTPS) → temp file
       ↓
Descarga archivo .sig (HTTPS)
       ↓
Verifica ed25519(binario, sig, pubkey_embebida)
       ↓  falla → descarta temp, log ERROR, continúa con versión actual
Verifica que version_nueva > version_actual
       ↓  falla → descarta temp, log WARN
Reemplaza ejecutable en disco
       ↓
Reinicia proceso
```

---

## Protocolo WebSocket para notificación de updates

### El servidor envía (después del `register` + validación de token):

```json
{
  "type": "update_available",
  "version": "1.2.0",
  "url": "https://cdn.usqay.com/releases/usqay-print-client-windows-amd64-1.2.0.exe",
  "signature_url": "https://cdn.usqay.com/releases/usqay-print-client-windows-amd64-1.2.0.exe.sig",
  "urgency": "optional"
}
```

| Campo | Valores | Comportamiento del cliente |
|---|---|---|
| `urgency` | `"optional"` | Descarga en background, aplica al próximo reinicio |
| `urgency` | `"required"` | Descarga inmediata, reinicia al terminar cola actual |
| `urgency` | `"critical"` | Descarga y reinicia inmediatamente (ignora cola) |

### El cliente responde con el resultado:

```json
{ "type": "update_result", "version": "1.2.0", "status": "applied" }
{ "type": "update_result", "version": "1.2.0", "status": "failed", "error": "firma inválida" }
```

---

## Consideraciones por sistema operativo

### Linux

`os.Rename()` funciona sobre un ejecutable en ejecución. El kernel mantiene el inode
abierto hasta que el proceso termina. El reemplazo es atómico a nivel de sistema de
archivos.

```go
// Reemplazo en Linux: atómico con Rename
os.Rename(tmpPath, currentExePath)
```

### Windows

**No se puede reemplazar un archivo `.exe` en ejecución** — Windows mantiene un lock
exclusivo sobre el ejecutable mientras corre. La solución estándar:

1. Renombrar el ejecutable actual a `usqay-print-client.exe.old`
2. Escribir el nuevo ejecutable en `usqay-print-client.exe`
3. Reiniciar el proceso (el nuevo exe ya está en disco)
4. Al arrancar, el nuevo proceso elimina `*.exe.old` si existe

```go
// Reemplazo en Windows
os.Rename(currentExePath, currentExePath+".old")  // libera el nombre
copyFile(tmpPath, currentExePath)                  // escribe nuevo
restartSelf()                                       // lanza el nuevo y sale
// Al próximo arranque:
os.Remove(currentExePath + ".old")
```

---

## Biblioteca recomendada

`github.com/inconshreveable/go-update` maneja el reemplazo cross-platform incluyendo
el caso de Windows. Soporta verificación de firma ed25519 de forma nativa.

```go
import (
    "crypto/ed25519"
    "encoding/base64"
    update "github.com/inconshreveable/go-update"
)

func applyUpdate(binaryReader io.Reader, sigReader io.Reader) error {
    pubKeyBytes, _ := base64.StdEncoding.DecodeString(updatePublicKey)
    pubKey := ed25519.PublicKey(pubKeyBytes)

    opts := update.Options{
        Signature:     sigBytes,
        PublicKey:     pubKey,
        SignatureType: update.ECDSA, // go-update usa ECDSA; para ed25519 usar validación manual
    }
    return update.Apply(binaryReader, opts)
}
```

> Nota: `go-update` v0.0.0 usa ECDSA. Para ed25519 puro, verificar la firma manualmente
> antes de llamar a `update.Apply` sin `Signature` (solo para el reemplazo en disco).

---

## Pipeline de firma en CI (GitHub Actions)

```yaml
- name: Build y firma del cliente Windows
  run: |
    GOOS=windows GOARCH=amd64 go build \
      -ldflags "-X main.updatePublicKey=${{ secrets.UPDATE_PUBLIC_KEY }} \
                -X main.Version=${{ github.ref_name }}" \
      -o build/usqay-print-client-windows-amd64-${{ github.ref_name }}.exe \
      ./cmd/client

    # Firmar con clave privada almacenada como secret en GitHub
    echo "${{ secrets.UPDATE_PRIVATE_KEY }}" | base64 -d > /tmp/sign.key
    openssl pkeyutl -sign \
      -inkey /tmp/sign.key \
      -in <(sha256sum build/*.exe | awk '{print $1}') \
      -out build/usqay-print-client-windows-amd64-${{ github.ref_name }}.exe.sig
    rm /tmp/sign.key
```

Los secrets `UPDATE_PRIVATE_KEY` y `UPDATE_PUBLIC_KEY` se generan una sola vez y
se guardan en GitHub → Settings → Secrets. La clave privada nunca toca disco local.

---

## Qué implementar en el cliente cuando llegue el momento

1. `internal/updater/updater.go` — descarga, verificación de firma, reemplazo
2. `internal/updater/updater_windows.go` — lógica de renombrado + lanzamiento de nuevo exe
3. `internal/updater/updater_linux.go` — `os.Rename` atómico + `syscall.Exec` para restart
4. Nuevo mensaje `update_available` en `ws/messages.go` y su handler en `ws/connection.go`
5. Limpieza de `*.exe.old` al arrancar en `cmd/client/main.go`

La variable `Version` ya se envía en el `RegisterMsg` (actualmente hardcodeada como `"1.0.3"`).
Cambiarla a `-ldflags "-X usqay-print-client/internal/version.Current=..."` para que el
binario sepa su propia versión y pueda comparar antes de descargar.
