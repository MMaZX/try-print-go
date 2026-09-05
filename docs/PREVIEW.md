# Guía de Previsualización Local (PREVIEW.md)

> **Validación visual rápida de tickets en Linux antes de enviar a Windows por SSH.**

---

## 1. Por qué existe esta herramienta

Históricamente, cualquier cambio en el diseño de un ticket (ancho de columnas de una tabla, tamaño de fuentes, ajuste de márgenes o inclusión de logotipos) requería un ciclo largo y costoso:
1. Cross-compilar el binario para Windows (`GOOS=windows go build`).
2. Transferirlo por SSH/SCP a la máquina virtual Windows.
3. Imprimir físicamente en el rollo de papel térmico.
4. Desechar papel y repetir el ciclo si una columna quedaba desalineada o un texto se solapaba.

Con el nuevo motor modular [`internal/ticketrender`](file:///home/fulanito/development/try-print-go/usqay-print-client/internal/ticketrender), **el 100% de la geometría, tipografía, tablas, códigos QR, imágenes y códigos de barras se renderiza primero como una imagen de alta precisión en memoria**. 

Por ello, ahora puedes convertir cualquier payload JSON a una imagen **PNG idéntica a la impresión física** en menos de 50 milisegundos directamente en tu máquina Linux de desarrollo.

> [!TIP]
> **Regla de oro:** El 95% del desarrollo, maquetación y ajuste de diseño se valida localmente en Linux con el previsualizador PNG. A Windows solo se envía el binario cuando el ticket visual ya está perfecto y únicamente se requiere validar hardware mecánico (corte de papel y apertura de gaveta).

---

## 2. Herramienta CLI: `preview_ticket`

El proyecto incluye el binario CLI [`usqay-print-client/cmd/preview_ticket/main.go`](file:///home/fulanito/development/try-print-go/usqay-print-client/cmd/preview_ticket/main.go).

### Sintaxis básica:

```bash
cd usqay-print-client
go run ./cmd/preview_ticket <ruta_al_payload.json> [ruta_de_salida.png]
```

- Si no especificas `ruta_de_salida.png`, la herramienta genera automáticamente un archivo con el mismo nombre y sufijo `_preview.png` en la misma carpeta del JSON.

### Ejemplos de uso:

```bash
# 1. Probar el payload de referencia completo (contiene texto, tablas, barcode, QR, logo):
go run ./cmd/preview_ticket dist/test_full_payload.json dist/test_preview.png

# 2. Probar un ticket temporal rápido:
go run ./cmd/preview_ticket /tmp/comanda_mesa_4.json
# Genera: /tmp/comanda_mesa_4_preview.png
```

### Salida esperada en consola:

```text
==================================================
🎨 RENDERIZANDO PREVIEW A IMAGEN (PNG)...
📄 Entrada : dist/test_full_payload.json
🖼️  Salida  : dist/test_preview.png
==================================================
✅ PREVIEW GENERADO EXITOSAMENTE!
⏱️  Tiempo de renderizado : 28.4 ms
📐 Dimensiones imagen    : 552 x 1366 px
📦 Tamaño del archivo PNG : 77.4 KB
🚀 Archivo listo en       : dist/test_preview.png
==================================================
```

### 2.1. Extracción y Previsualización Directa desde SQL (`cola_impresion`)

Si extraes un volcado de sentencias `INSERT INTO cola_impresion` directamente desde la base de datos de producción (MySQL/PostgreSQL), puedes procesarlas y previsualizarlas automáticamente con el script [`scripts/extract_sql_payloads.py`](file:///home/fulanito/development/try-print-go/scripts/extract_sql_payloads.py):

```bash
# Extrae cada payload a cola_XX.json y genera su imagen cola_XX.png correspondiente
python3 scripts/extract_sql_payloads.py ruta/a/inserts.sql --out dist/rest --preview
```

Opciones:
- `--out <dir>`: Carpeta de destino relativa a la raíz (por defecto `dist/rest`).
- `--no-preview`: Solo extrae los archivos JSON sin generar imágenes.

---

## 3. Flujo de Trabajo Recomendado (Iteración Local Rápida)

```
  ┌─────────────────────────┐
  │  Editar payload .json   │
  └────────────┬────────────┘
               │
               ▼
  ┌─────────────────────────┐
  │ Ejecutar preview_ticket │ ◄── En Linux (~30 ms, sin compilar a Windows)
  └────────────┬────────────┘
               │
               ▼
  ┌─────────────────────────┐
  │   Inspeccionar el PNG   │ ◄── En tu visor de imágenes favorito o IDE
  └────────────┬────────────┘
               │
        ¿Quedó perfecto?
         ├── NO  ──► Volver a editar JSON / código de layout
         │
         └── SÍ  ──► ¡Listo para enviar a Windows! (validar corte/gaveta)
```

### Cómo abrir y visualizar la imagen en Linux:

Puedes abrir el archivo generado con cualquier visor gráfico de tu entorno:

```bash
# Con el visor por defecto del sistema de escritorio:
xdg-open dist/test_preview.png

# O con visores ligeros comunes:
feh dist/test_preview.png
eog dist/test_preview.png
```

---

## 4. Checklist de Validación Visual

Al inspeccionar el archivo PNG generado, revisa los siguientes puntos críticos:

- [ ] **Ancho y Márgenes:** ¿El contenido respeta el ancho del papel configurado (`80.0` o `58.0` mm) sin recortarse lateralmente?
- [ ] **Tablas y Columnas:** 
  - ¿Las columnas numéricas (precios, importes) están correctamente alineadas a la derecha (`align: "right"`)?
  - ¿Las descripciones largas se envuelven a la siguiente línea sin montarse sobre las demás celdas?
- [ ] **Filas combinadas (`merge: true`):** ¿Las notas de cocina o subtítulos ocupan todo el ancho y tienen espacio suficiente antes y después?
- [ ] **Fuentes Grandes (`medium`, `double`):** ¿Los textos destacados tienen suficiente separación vertical respecto a las filas precedentes?
- [ ] **Código de Barras (1D):** ¿Las barras verticales del Code-128 son nítidas y se lee el texto HRI numérico abajo?
- [ ] **Código QR (2D):** ¿El código QR tiene buen contraste y suficiente margen blanco alrededor (*quiet zone*)?

---

## 5. Pruebas Automatizadas en CI / Consola

No es necesario abrir la imagen manualmente para cada cambio si tienes tests automatizados. Puedes ejecutar la suite de pruebas del motor de renderizado con:

```bash
cd usqay-print-client
go test -v ./internal/ticketrender
```

Esto verifica de forma determinista:
1. Que el deserializador de JSON no falle con tipos inválidos.
2. Que la imagen generada tenga dimensiones no nulas ($W > 0, H > 0$).
3. Que la codificación a PNG sea válida.
4. Que la traducción a bytes térmicos ESC/POS (`GS v 0`) se genere sin errores de buffer.

---

## 6. Cuándo migrar la prueba a Windows (SSH)

Una vez que el archivo PNG cumple al 100% con los requerimientos estéticos y de diseño:

> [!IMPORTANT]
> **Solo migra a Windows cuando requieras validar comportamiento mecánico:**
> 1. Verificación del corte de papel (`"options": { "cut": true }`).
> 2. Verificación del pulso eléctrico del cajón monedero (`"options": { "drawer": true }`).
> 3. Nivel de calor o contraste del cabezal térmico del modelo de impresora físico del restaurante.
> 4. Comportamiento del sensor de fin de papel.

Para este último paso mecánico, consulta las instrucciones detalladas de SSH, SCP y PowerShell en:
👉 [`docs/entorno-pruebas-windows.md`](file:///home/fulanito/development/try-print-go/docs/entorno-pruebas-windows.md)
