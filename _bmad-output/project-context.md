---
project_name: 'try-print-go'
user_name: 'Fulanito'
date: '2026-07-15'
sections_completed: ['technology_stack', 'critical_rules', 'language_rules', 'testing_rules', 'code_quality_rules', 'workflow_rules']
existing_patterns_found: 6
---

# Project Context for AI Agents

_This file contains critical rules and patterns that AI agents must follow when implementing code in this project. Focus on unobvious details that agents might otherwise miss._

---

## Technology Stack & Versions

- **Go version**: 1.26.2
- **Client core packages**:
  - `modernc.org/sqlite` (v1.52.0): CGO-free SQLite implementation. **MUST NOT** import `mattn/go-sqlite3` to ensure compilation on Windows without MinGW/GCC.
  - `github.com/alexbrainman/printer` (v0.0.0-20200912035444-f40f26f0bdeb): Used exclusively for sending RAW bytes to the Windows spooler.
  - `github.com/coder/websocket` (v1.8.14): Lightweight, performant WebSocket library.
- **Server core packages**:
  - `github.com/coder/websocket` (v1.8.14)
- **Formatting and Geometry dependencies** (allowed from Phase 3):
  - `fogleman/gg` (Pure Go 2D rendering engine)
  - `golang.org/x/image` (Pure Go image processing)

---

## Critical Implementation Rules

### 1. Impresión Local y Autoridad del Cliente
El principio fundamental es la **Autoridad de Impresión Local**. El cliente es el único que puede declarar que un trabajo fue impreso con éxito. El servidor nunca asume que la impresión ocurrió solo por enviarla.

### 2. Aislamiento por Plataforma
El código que interactúa con el sistema de impresión de Windows y Linux **DEBE** estar completamente aislado mediante tags de compilación en archivos separados:
- `printer_windows.go`: Usará `github.com/alexbrainman/printer` con build tags `//go:build windows`.
- `printer_linux.go`: Usará comandos del sistema como `lp` con build tags `//go:build linux`.
No mezclar lógica específica de sistema operativo en el resto de la aplicación.

### 3. Flujo de Trabajo en SQLite y Worker Autónomo
El cliente inserta las órdenes entrantes en la tabla SQLite `print_jobs` con estado `PENDING`, responde de inmediato con `ACK` al servidor, y un worker en background procesa de manera secuencial los trabajos encolados:
- Estados válidos: `PENDING`, `PROCESSING`, `PRINTED`, `ERROR`.
- El worker **NUNCA** debe entrar en `panic` por fallos de hardware. Los errores deben registrarse y actualizar el estado a `ERROR` en la base de datos local y reportarse al servidor cuando sea posible.

### 4. Conexiones WebSocket Duplicadas (Kick)
Si se conecta un cliente con un `terminal_id` que ya tiene una conexión WebSocket activa, el servidor debe hacer **kick** (cerrar la conexión anterior y liberar recursos) y aceptar la nueva. La última conexión siempre prevalece.

### 5. Configuración Sin Variables de Entorno (.env)
La configuración del cliente y servidor se lee exclusivamente de un archivo `config.json` ubicado en el directorio de ejecución. El cliente debe validar su estructura al arrancar y abortar inmediatamente con un error explícito si faltan credenciales o URLs críticas.

### 6. Rotación de Logs
Los logs se escriben localmente en `logs/YYYY-MM-DD.log`. Al inicio de cada mes, se deben comprimir automáticamente los archivos de log del mes anterior a `logs/archive/YYYY/mes/YYYY-mes.zip` y eliminar los originales. Este mantenimiento lo realiza el binario del cliente en su rutina de inicio.

---

## Language-Specific Rules (Go)

- **Pure Go sin CGO**: Mantener la portabilidad y facilidad de compilación del binario en Windows/Linux. No introducir dependencias CGO.
- **Manejo de Errores Estricto**: Todo error de sistema, base de datos o red debe ser capturado, wrapped con `%w` si se propaga, y logueado adecuadamente. No usar `panic` para recuperar errores operacionales de red o impresión.
- **Go Routines & Channels**: El loop del worker y las lecturas de WebSocket deben usar selectores, tickers y contexto para cancelación limpia.

---

## Testing Rules

- **Suite Golden de Bytes**: Para cambios en el motor de impresión, usar una suite de pruebas con salidas estables (archivos `.prn` de referencia) y fallar el test ante cualquier desviación no intencionada de bytes ESC/POS.
- **Pruebas Unitarias de Layout**: Asegurar que las alineaciones, offsets y padding funcionen contando runas en Go en lugar de bytes, para evitar desalineación con caracteres multibyte (ej. tildes, eñes).
- **Ejecución local**: Todos los tests deben poder ejecutarse de forma aislada y sin hardware físico mediante: `go test ./...`.

---

## Development Workflow Rules

- **Branching**: Toda característica nueva o corrección de error debe desarrollarse en ramas específicas (ej. `feature/render-printer`) y no directamente en `poc` o `main`.
- **Golden Diffs**: Los cambios en los tests golden de bytes deben validarse visualmente mediante renderizadores/visualizadores ESC/POS (como escpresso o esc2html) y registrar el veredicto en el log.
