# Usqay Print Server 0.1.1

Servidor de impresión multiplataforma para gestionar trabajos de impresión desde sistemas Usqay.

## 📋 Características

- ✅ Multiplataforma (Windows, Linux, macOS)
- ✅ Instalación como servicio del sistema
- ✅ API REST para monitoreo
- ✅ Sistema de logs diarios automáticos
- ✅ Configuración de impresoras por terminal
- ✅ Soporte para múltiples tipos de documentos
- ✅ Limpieza automática de logs antiguos

## 🛠️ Compilación

### Requisitos Previos

- Go 1.21 o superior
- Git

> **Nota:** El binario compilado debe llamarse `usqaytry` (o `usqaytry.exe` en Windows) según lo requerido por el sistema.

### Windows (PowerShell)

```powershell
# Clonar repositorio
git clone https://github.com/usuario/usqay-printer.git
cd usqay-printer

# Instalar dependencias
go mod download

# Compilar
go build -o usqaytry.exe

# Compilar con optimizaciones
go build -ldflags="-s -w" -o usqaytry.exe
```

### Linux

```bash
# Clonar repositorio
git clone https://github.com/usuario/usqay-printer.git
cd usqay-printer

# Instalar dependencias
go mod download

# Compilar
go build -o usqaytry

# Compilar con optimizaciones
go build -ldflags="-s -w" -o usqaytry

# Dar permisos de ejecución
chmod +x usqaytry
```

### macOS

```bash
# Clonar repositorio
git clone https://github.com/usuario/usqay-printer.git
cd usqay-printer

# Instalar dependencias
go mod download

# Compilar
go build -o usqaytry

# Compilar con optimizaciones
go build -ldflags="-s -w" -o usqaytry

# Dar permisos de ejecución
chmod +x usqaytry
```

### Compilación Cruzada

**Desde Windows:**

```powershell
# Para Linux
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o usqaytry

# Para macOS
$env:GOOS="darwin"; $env:GOARCH="amd64"; go build -o usqaytry
```

**Desde Linux/macOS:**

```bash
# Para Windows
GOOS=windows GOARCH=amd64 go build -o usqaytry.exe

# Para Linux (desde macOS)
GOOS=linux GOARCH=amd64 go build -o usqaytry

# Para macOS (desde Linux)
GOOS=darwin GOARCH=amd64 go build -o usqaytry
```


## 🚀 Instalación

### Windows

```bash
# Instalar servicio
usqaytry.exe --install windows

# Iniciar servicio
usqaytry.exe --start windows
```

### Linux

```bash
# Instalar servicio (requiere sudo)
sudo ./usqaytry --install linux

# Iniciar servicio
sudo systemctl start usqay-print
```

### macOS

```bash
# Instalar servicio
./usqaytry --install macos

# Iniciar servicio
./usqaytry --start macos
```

## ⚙️ Configuración

El archivo de configuración se crea automáticamente en:

- **Windows:** `C:\usqay\cnfg\config.json`
- **Linux/macOS:** `~/.usqay/config.json`

```json
{
    "db_server": "localhost",
    "db_database": "restaurantes",
    "db_user": "root",
    "db_password": "",
    "url_base": "localhost/usqay",
    "agrupar": true,
    "save_debug": false,
    "api_port": 8765
}
```

## 📝 Uso

### Comandos de Servicio

```bash
# Instalar
usqaytry --install <windows|linux|macos>

# Iniciar
usqaytry --start <windows|linux|macos>

# Detener
usqaytry --stop <windows|linux|macos>

# Reiniciar
usqaytry --restart <windows|linux|macos>

# Ver estado
usqaytry --status <windows|linux|macos>

# Desinstalar
usqaytry --uninstall <windows|linux|macos>
```

### Operaciones

```bash
# Actualizar lista de impresoras
usqaytry -r

# Limpiar logs antiguos (30+ días)
usqaytry --clear

# Iniciar con API habilitada
usqaytry --api

# Ver versión
usqaytry --version

# Ver ayuda
usqaytry --help
```

## 🌐 API REST

Habilitar con `--api` o configurando `api_port` en el config.

### Endpoints

```bash
# Estado del servicio
GET http://localhost:8765/api/status

# Configuración actual
GET http://localhost:8765/api/config

# Cola de impresión
GET http://localhost:8765/api/queue

# Health check
GET http://localhost:8765/health
```

## 📁 Estructura de Archivos

```
Windows:
C:\usqay\
├── cnfg\
│   └── config.json
└── logs\
                ├── DD-MM-YYYY\
                │   ├── access.log
                │   ├── error.log
                │   └── debug.log
                └── prints\

Linux/macOS:
~/.usqay/
├── config.json
└── logs/
```

## 🔧 Requisitos de Base de Datos

### Tabla `cola_impresion`

```sql
CREATE TABLE cola_impresion (
        id INT AUTO_INCREMENT PRIMARY KEY,
        codigo VARCHAR(50),
        terminal VARCHAR(50),
        aux TEXT,
        tipo VARCHAR(50)
);
```

### Tabla `impresoras`

```sql
CREATE TABLE impresoras (
        id INT AUTO_INCREMENT PRIMARY KEY,
        nombre VARCHAR(255)
);
```

### Tabla `configuracion_impresion`

```sql
CREATE TABLE configuracion_impresion (
        id INT AUTO_INCREMENT PRIMARY KEY,
        terminal VARCHAR(50),
        opcion VARCHAR(100),
        impresora VARCHAR(255)
);
```

### Tabla `margenes_impresion`

```sql
CREATE TABLE margenes_impresion (
        id INT AUTO_INCREMENT PRIMARY KEY,
        impresora VARCHAR(255),
        ratio DECIMAL(4,2) DEFAULT 0.99
);
```

## 🐛 Solución de Problemas

### El servicio no inicia

```bash
# Ver logs
# Windows: C:\usqay\logs\
# Linux: sudo journalctl -u usqay-print -f
# macOS: ~/Library/Logs/usqay/

# Verificar configuración
usqaytry --status <os>
```

### Error de conexión a BD

1. Verificar credenciales en `config.json`
2. Confirmar que MySQL está corriendo
3. Verificar permisos del usuario de BD

### Impresoras no aparecen

```bash
# Actualizar lista de impresoras
usqaytry -r
```

## 📊 Monitoreo

### Ver logs en tiempo real

**Windows: (Solo PowerShell)**

```powershell
Get-Content C:\usqay\logs\DD-MM-YYYY\access.log -Wait
```

**Linux:**

```bash
sudo journalctl -u usqay-print -f
```

**macOS:**

```bash
tail -f ~/.usqay/logs/DD-MM-YYYY/access.log
```

## 🔐 Permisos

- **Windows:** Ejecutar como Administrador para instalar servicio
- **Linux:** Requiere `sudo` para operaciones de servicio
- **macOS:** Usuario normal para LaunchAgent

