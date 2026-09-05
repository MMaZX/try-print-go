# Entorno de pruebas Windows (obligatorio para impresión física)

## Por qué

El agente (`usqay-print-client`) corre en el POS del cliente, que es **Windows**. Ahí la impresión pasa por el
Print Spooler (`winspool.drv`) usando el driver ESC/POS instalado del fabricante, vía
`internal/printer/printer_windows.go`.

En Linux, sin ese driver, solo existe la ruta de escritura cruda al nodo `/dev/usb/lp0`
(`internal/printer/printer_linux.go`). Se comprobó en la práctica (2026-08-18) que esa ruta cruda produce
corrupción de datos en impresoras térmicas clon (bytes de imagen perdidos/corrompidos, comando de corte no
llega, impresión incompleta) incluso con pacing agresivo — el driver de Windows resuelve esto porque el
spooler maneja el buffering/flow-control con el hardware real, cosa que no podemos replicar razonablemente
en Linux con un dispositivo pasado por USB passthrough.

**Regla:** cualquier prueba que involucre imprimir físicamente (verificar layout, raster, corte, cajón,
velocidad real) se hace en la VM Windows. Compilar, testear con `go test ./...`, y vetear código sí se hace
en Linux (host de desarrollo) — solo la impresión física migra a Windows.

## Entorno

VM Windows (QEMU/KVM) con la impresora térmica pasada por USB passthrough, accesible por SSH con
OpenSSH Server nativo de Windows (shell remota: PowerShell `pwsh`). El driver ESC/POS del fabricante ya
está instalado ahí como impresora del sistema (`Get-Printer`), por lo que las pruebas usan ese nombre de
impresora — no un device path. En esta VM las impresoras instaladas están nombradas por rol/estación
(`CAJA`, `COCINA`), no por modelo de driver — confirmar el nombre vigente con `Get-Printer` antes de asumir
un valor, ya que puede cambiar si se reinstala o reconfigura la VM.

### Configuración de conexión

Los datos de conexión (host, usuario, nombre de impresora, etc.) **no viven en este documento** — viven en
un YAML fuera de git:

- `usqay-print-client/windows-test.example.yaml` — plantilla, sí se commitea.
- `usqay-print-client/windows-test.yaml` — datos reales, en `.gitignore`, nunca se commitea.

Para preparar el entorno la primera vez:

```bash
cp usqay-print-client/windows-test.example.yaml usqay-print-client/windows-test.yaml
# editar windows-test.yaml con los valores reales del entorno
```

Prueba de conectividad:

```bash
ssh -o BatchMode=yes -o ConnectTimeout=5 <user>@<host> "echo ok"
```

(sustituir `<user>@<host>` por los valores de `windows-test.yaml`).

## Flujo de trabajo (build → copiar → ejecutar → leer resultado)

1. Cross-compilar en el host Linux:
   ```bash
   cd usqay-print-client
   GOOS=windows GOARCH=amd64 go build -o dist/<binario>.exe ./cmd/<paquete>
   ```
2. Copiar el binario a la VM (`remote_dir` de `windows-test.yaml`):
   ```bash
   scp dist/<binario>.exe <user>@<host>:<remote_dir>/<binario>.exe
   ```
3. Ejecutar remotamente por SSH no interactivo, pasando el **nombre de la impresora** (no un device path):
   ```bash
   ssh <user>@<host> "<remote_dir>/<binario>.exe <printer_name>"
   ```
4. Revisar el ticket físico impreso en la VM (no hay forma automatizable de verificar el resultado físico:
   requiere que un humano mire el papel o una foto).

## Qué NO se usa y por qué

- **SPICE**: protocolo de display/input, no expone canal de texto automatizable. Solo para debug visual
  manual del operador humano.
- **Go instalado en la VM**: no hace falta. Todo se cross-compila en Linux y se copia el `.exe` ya listo.
- **Pruebas de impresión física en Linux**: descartadas — ver sección "Por qué" arriba. El único caso válido
  para escribir directo a `/dev/usb/lp0` en Linux es debug de bajo nivel del propio protocolo ESC/POS, no
  validación de que un ticket sale bien.

## Notas de seguridad

- La llave SSH es de un entorno de pruebas local (VM aislada), no de producción.
- Nunca commitear `windows-test.yaml` ni pegar sus valores en documentación, PRs, o mensajes — solo vive en
  ese archivo ignorado por git.
