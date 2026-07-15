# Documentación del Cliente de Impresión (usqay-print-client)

Esta carpeta contiene la documentación detallada sobre el diseño, arquitectura y funcionamiento del cliente de impresión desarrollado en Go (`usqay-print-client`).

El cliente es una aplicación ligera y persistente que se conecta mediante WebSockets a un servidor central (`usqay-print-server`), recibe trabajos de impresión en formato JSON estructurado, los procesa localmente utilizando una cola SQLite y los envía a las impresoras físicas correspondientes (ya sean térmicas ESC/POS o impresoras convencionales).

---

## Estructura del Proyecto de Documentación

Para facilitar la lectura y el mantenimiento, la documentación está dividida en las siguientes secciones:

1. **[Estructura y Arquitectura General](README.md)** (este archivo): Resumen global del código, archivos que lo componen, base de datos local y configuración.
2. **[Flujo de una Petición de Impresión](flujo-peticion.md)**: Detalle técnico paso a paso de lo que ocurre desde que entra una petición al WebSocket hasta que se envía al hardware físico, incluyendo sincronización de cola.
3. **[Motor de Renderizado e Impresión](motor-render.md)**: Explicación de cómo se transforman los payloads JSON en comandos ESC/POS binarios o texto plano, compatibilidad con papel, transcodificación a CP850 y drivers del sistema operativo.

---

## Estructura de Archivos del Código Fuente

El cliente sigue las convenciones estándar de un proyecto en Go:

```
usqay-print-client/
├── build/                 # Binarios compilados para diferentes plataformas.
├── cmd/
│   └── client/
│       └── main.go        # Punto de entrada principal. Inicializa logs, base de datos y goroutines.
├── config.json            # Configuración local de credenciales y servidor (creado por el usuario).
├── go.mod / go.sum        # Definición y sumas de dependencias de Go.
└── internal/              # Código fuente interno privado de la aplicación.
    ├── config/
    │   └── config.go      # Cargador y validador de config.json.
    ├── escpos/
    │   ├── builder.go     # Constructor fluido de secuencias de comandos ESC/POS.
    │   ├── encoding.go    # Transcodificador de UTF-8 a CP850 (para caracteres en español).
    │   └── builder_test.go
    ├── logging/
    │   # Handlers para manejo de logs diarios rotativos y archivado mensual en formato ZIP.
    ├── printer/
    │   ├── printer.go     # Interfaz Printer y autodetección de tecnologías.
    │   ├── printer_linux.go   # Implementación para Linux (CUPS mediante comando 'lp').
    │   ├── printer_windows.go # Implementación para Windows (API de winspool).
    │   ├── network.go     # Conexión directa TCP en puerto 9100 para impresoras de red.
    │   └── registry.go    # Registro concurrente en memoria de impresoras configuradas.
    ├── queue/
    │   ├── sqlite.go      # Inicializador y migraciones de SQLite.
    │   ├── repository.go  # Operaciones CRUD sobre la tabla de trabajos de impresión.
    │   ├── renderer.go    # Generador/intérprete de bloques JSON a bytes físicos.
    │   ├── worker.go      # Bucle en segundo plano que procesa y ejecuta la cola de impresión.
    │   └── renderer_test.go
    └── ws/
        ├── connection.go  # Cliente WebSocket con auto-reconexión y latido (heartbeat).
        └── messages.go    # Definiciones de los mensajes del protocolo WebSocket.
```

---

## Componentes Principales y su Rol

### 1. Punto de Entrada (`main.go`)
- Lee los argumentos de la línea de comandos (como `-list` para ver impresoras locales o `-insert` para un test manual).
- Carga el archivo `config.json`.
- Configura el sistema de logs dobles: consola (`stdout`) y archivos diarios rotativos en la carpeta `./logs` (además comprime a `.zip` los logs del mes anterior para ahorrar espacio).
- Inicializa la base de datos local SQLite (`agent.db`).
- Levanta e interactúa con dos goroutines principales en paralelo mediante un ciclo de vida controlado por un `context.Context` de Go:
  1. **WebSocket Loop (`go conn.Run`)**: Mantiene la conexión abierta con el servidor remoto.
  2. **Queue Worker Loop (`go worker.Run`)**: Busca trabajos pendientes de impresión y los procesa continuamente.
- Maneja el apagado controlado (Graceful Shutdown) capturando señales `SIGINT` / `SIGTERM`, deteniendo los bucles de manera ordenada y otorgando un margen de `600ms` para vaciar los buffers.

### 2. Configuración (`internal/config`)
- Carga `config.json` buscando en el directorio del ejecutable o en el directorio de ejecución actual (`cwd`) como respaldo de desarrollo.
- Estructura de `config.json`:
  ```json
  {
    "server_url": "wss://mi-servidor.usqay.pe/ws/print",
    "token": "token_unico_de_la_terminal_generado_en_laravel",
    "log_level": "info"
  }
  ```
- **Campos:**
  - `server_url`: Dirección WebSocket del servidor.
  - `token`: Credencial de autenticación que identifica de manera única a la terminal física sin necesidad de codificar un `terminal_id` en el cliente.
  - `log_level`: Severidad de registro (`debug`, `info`, `warn`, `error`).

### 3. Base de Datos Local (`internal/queue/sqlite.go`)
- Utiliza un motor de SQLite embebido en Go puro (`modernc.org/sqlite`) para no requerir CGO ni dependencias del compilador de C.
- Archivo de base de datos generado: `agent.db` en el mismo directorio del ejecutable.
- **Esquema de la Tabla `print_jobs`:**
  - `id` (`TEXT PRIMARY KEY`): UUID del trabajo de impresión (enviado desde el backend).
  - `payload` (`TEXT`): Documento completo en formato JSON estructurado.
  - `tipo_documento` (`TEXT`): Propósito del documento (por ejemplo, `"comanda"`, `"boleta"`, `"factura"`).
  - `impresora_id` (`TEXT`): ID de la impresora destino registrada en el servidor.
  - `estado` (`TEXT`): Lifecycle del trabajo en el cliente (`PENDING`, `PROCESSING`, `PRINTED`, `ERROR`).
  - `error_msg` (`TEXT`): Razón de fallo si el estado es `ERROR`.
  - `created_at` / `updated_at` (`DATETIME`): Registros temporales UTC.
- Cuenta con un índice en `(estado, created_at)` para optimizar la consulta del worker en segundo plano.
- Configura `SetMaxOpenConns(1)` para evitar bloqueos por escritura concurrente (`SQLITE_BUSY`).
- Auto-aplica migraciones de schema, como la adición de columnas dinámicas en bases de datos antiguas.

---

Siguiente sección: **[Flujo de una Petición de Impresión](flujo-peticion.md)**
