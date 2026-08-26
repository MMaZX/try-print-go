# Diagramas de arquitectura — motor de impresión

## Pipeline de render (tres etapas, IR central)

```text
PrintPayload (JSON del servidor — contrato fijo)
        │
        ▼
┌─────────────────────────────────────────────┐
│ 1. LAYOUT (Go puro, portable)               │
│    measure → arrange                        │
│    restricción única: maxWidthDots          │
│    (papel − márgenes, desde DeviceProfile)  │
└─────────────────────────────────────────────┘
        │  árbol de primitivas medidas
        ▼  (líneas de texto posicionadas, imágenes 1-bit)
┌─────────────────────────────────────────────┐
│ 2. EMISIÓN (backends; solo traducen,        │
│    nunca recalculan geometría)              │
│    • escpos-text   → default                │
│    • escpos-raster → GS v 0, selectivo      │
│    • texto plano   → inkjet/láser (existente)│
└─────────────────────────────────────────────┘
        │  []byte
        ▼
┌─────────────────────────────────────────────┐
│ 3. ENTREGA (única capa por SO, build tags)  │
│    printer_windows.go → winspool RAW        │
│    printer_linux.go   → lp -d <cola> -o raw │
│    [flag captura] ────→ archivo .prn        │
└─────────────────────────────────────────────┘
```

## Resolución del DeviceProfile

```text
config de impresora
   │
   ├─ ¿perfil explícito? ──────────► usarlo
   │
   ├─ ¿ancho de papel declarado? ──► derivar perfil
   │       (58→384, 80→576 como defaults;
   │        interpolación solo para anchos custom)
   │
   └─ nada ─────────────────────────► fallback perfil 58mm
```

## Frontera de portabilidad

Etapas 1 y 2 son Go puro sin syscalls — idénticas en Windows y Linux. Solo la etapa 3 difiere por SO y ya existe; el rediseño no la toca.
