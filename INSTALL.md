# Instrucciones de Build — usqay-print-client

El cliente genera **dos binarios**: uno para Linux y uno para Windows.
El servidor solo genera binario Linux (se despliega en servidor).

---

## Requisitos previos

| Herramienta | Versión mínima | Para qué |
|---|---|---|
| Go | 1.22+ | Build en host (Linux o Windows) |
| Docker + Docker Compose | cualquier versión reciente | Build vía contenedor |

> `modernc.org/sqlite` no usa CGO, por lo que **no se necesita GCC ni MinGW** en ningún caso.

---

## Si estás en Linux

### Opción A — Build en host (necesita Go instalado)

```bash
./client.sh --target=host
```

Genera:
- `usqay-print-client/build/linux/usqay-print-client`
- `usqay-print-client/build/windows/usqay-print-client.exe`

Al terminar arranca automáticamente el cliente Linux.

### Opción B — Build con Docker (no necesita Go instalado)

```bash
./client.sh --target=docker
```

Usa los servicios `client-linux-build` y `client-windows-build` del `docker-compose.yml`.
El resultado queda en los mismos directorios que la Opción A.

### Solo el binario, sin arrancar

Si solo quieres compilar sin ejecutar, llama a `go build` directamente:

```bash
cd usqay-print-client

# Linux
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/linux/usqay-print-client ./cmd/client

# Windows (cross-compile desde Linux)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o build/windows/usqay-print-client.exe ./cmd/client
```

---

## Si estás en Windows

En Windows no puedes ejecutar `client.sh` directamente. Tienes dos caminos:

### Opción A — Build con Go instalado en Windows

Abre PowerShell o CMD en la carpeta `usqay-print-client\` y ejecuta:

```powershell
# Solo el binario Windows (el más común en este caso)
go build -o build\windows\usqay-print-client.exe .\cmd\client
```

Si también necesitas el binario Linux (cross-compile):

```powershell
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -o build/linux/usqay-print-client ./cmd/client
```

### Opción B — Build con Docker Desktop

Con Docker Desktop instalado, ejecuta desde la raíz del repositorio:

```powershell
# Solo Windows
docker compose run --rm client-windows-build

# Solo Linux
docker compose run --rm client-linux-build

# Ambos
docker compose run --rm client-windows-build
docker compose run --rm client-linux-build
```

### Opción C — WSL (Windows Subsystem for Linux)

Desde una terminal WSL puedes usar exactamente los mismos comandos que en Linux:

```bash
./client.sh --target=host
# o
./client.sh --target=docker
```

---

## Dónde quedan los binarios

```
usqay-print-client/
└── build/
    ├── linux/
    │   ├── usqay-print-client      ← ejecutable Linux
    │   └── config.json
    └── windows/
        ├── usqay-print-client.exe  ← ejecutable Windows
        └── config.json
```

Antes de ejecutar, asegúrate de que `config.json` esté junto al binario con los valores correctos de `server_url`, `terminal_id` y `token`.
