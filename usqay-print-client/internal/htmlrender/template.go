package htmlrender

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/adcondev/poster/pkg/graphics"

	"usqay-print-client/internal/printer"
)

// PrintPayload representa el objeto JSON raíz recibido para imprimir.
type PrintPayload struct {
	Options         PrintOptions      `json:"options"`
	PaperProperties PaperProperties   `json:"paper_properties"`
	Margins         *legacyMargins    `json:"margins,omitempty"`
	Body            []json.RawMessage `json:"body"`
}

type legacyMargins struct {
	DimensionPapel float64 `json:"dimension_papel"`
	Padding        float64 `json:"padding"`
}

func (p *PrintPayload) applyLegacyMargins() {
	if p.PaperProperties.Width <= 0 && p.Margins != nil && p.Margins.DimensionPapel > 0 {
		p.PaperProperties.Width = p.Margins.DimensionPapel
		if p.Margins.Padding > 0 && len(p.PaperProperties.Padding) == 0 {
			pad := p.Margins.Padding
			p.PaperProperties.Padding = []float64{pad, pad, pad, pad}
		}
	}
}

// PrintOptions define el comportamiento electromecánico del hardware.
type PrintOptions struct {
	Cut    bool `json:"cut"`
	Drawer bool `json:"drawer"`
}

// PaperProperties define las dimensiones físicas en mm y márgenes de 4 lados.
type PaperProperties struct {
	Width   float64   `json:"width"`   // Ancho del papel en mm (ej. 58.0, 70.0, 80.0)
	Scale   float64   `json:"scale"`   // Multiplicador de zoom sobre el tamaño tipográfico base
	Padding []float64 `json:"padding"` // Márgenes [top, right, bottom, left] en mm
}

// DefaultScale es el valor de scale asumido cuando el payload no especifica
// scale (o lo envía en 0 / negativo). Es el scale "de cliente", antes de
// aplicar BaseScaleFactor.
const DefaultScale = 1.0

// BaseScaleFactor es el multiplicador base aplicado SIEMPRE sobre el scale
// solicitado en el payload. A 1.0x puro la tipografía sale demasiado chica en
// las térmicas objetivo, así que el "1.0" del cliente se renderiza en realidad
// a 1.7x. El cliente pide 1, obtiene 1.7; pide 1.5, obtiene 2.55; etc.
const BaseScaleFactor = 1.7

// scaleModelVersion identifica el algoritmo de escala vigente. Se imprime en
// la línea de calibración (ver scaleDebugEnabled) para saber qué modelo produjo
// el raster. Bump manual en cada iteración del modelo de escala.
const scaleModelVersion = "dpi-zoom-v1"

// scaleDebugEnabled activa la línea de calibración de escala sobre el ticket.
// SOLO para pruebas físicas: se enciende con USQAY_SCALE_DEBUG=1 (o true/yes/on).
// Por defecto está apagada y el ticket no lleva nada fuera del JSON.
func scaleDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("USQAY_SCALE_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// maxScale acota el zoom EFECTIVO (ya multiplicado por BaseScaleFactor) para
// evitar rasters desproporcionados. Con BaseScaleFactor 1.7 equivale a un
// scale de cliente máximo de ~4.7x.
const maxScale = 8.0

// EffectiveScale devuelve el multiplicador de zoom real a aplicar: toma el
// scale solicitado (<= 0 o ausente => DefaultScale), lo multiplica por
// BaseScaleFactor y acota el resultado a maxScale por seguridad.
func (p PaperProperties) EffectiveScale() float64 {
	s := p.Scale
	if s <= 0 {
		s = DefaultScale
	}
	s *= BaseScaleFactor
	if s > maxScale {
		return maxScale
	}
	return s
}

func (p PaperProperties) TopMM() float64 {
	if len(p.Padding) > 0 {
		return p.Padding[0]
	}
	return 0.0
}

func (p PaperProperties) RightMM() float64 {
	if len(p.Padding) > 1 {
		return p.Padding[1]
	}
	return 0.0
}

func (p PaperProperties) BottomMM() float64 {
	if len(p.Padding) > 2 {
		return p.Padding[2]
	}
	return 0.0
}

func (p PaperProperties) LeftMM() float64 {
	if len(p.Padding) > 3 {
		return p.Padding[3]
	}
	return 0.0
}

type blockEnvelope struct {
	Type string `json:"type"`
}

type textBlock struct {
	Value string `json:"value"`
	Align string `json:"align"`
	Bold  bool   `json:"bold"`
	Size  string `json:"size"` // "normal", "medium", "double"
}

type separatorBlock struct {
	Character string `json:"character"`
}

type spacerBlock struct {
	Lines int `json:"lines"`
}

type columnsCell struct {
	Text  string  `json:"text"`
	Width float64 `json:"width"`
	Align string  `json:"align"`
	Bold  bool    `json:"bold"`
	Size  string  `json:"size"`
}

type columnsBlock struct {
	Columns []columnsCell `json:"columns"`
}

type tableColumn struct {
	Header *string `json:"header"`
	Width  float64 `json:"width"`
	Align  string  `json:"align"`
}

type tableCell struct {
	Text  string `json:"text"`
	Bold  *bool  `json:"bold"`
	Size  string `json:"size"`
	Align string `json:"align"`
}

type tableRow struct {
	Cells []tableCell `json:"cells"`
	Bold  bool        `json:"bold"`
	Merge bool        `json:"merge"`
}

type tableBlock struct {
	Columns []tableColumn `json:"columns"`
	Rows    []tableRow    `json:"rows"`
}

type imageBlock struct {
	Data   string `json:"data"`
	Align  string `json:"align,omitempty"`
	Width  int    `json:"width,omitempty"`
	Dither bool   `json:"dither,omitempty"`
}

type qrBlock struct {
	Value      string `json:"value"`
	Data       string `json:"data,omitempty"`
	Align      string `json:"align"`
	Size       int    `json:"size"`
	PixelWidth int    `json:"pixel_width,omitempty"`
}

type barcodeBlock struct {
	Symbology string `json:"symbology"`
	Value     string `json:"value"`
	Align     string `json:"align"`
	Height    int    `json:"height"`
	Width     int    `json:"width"`
	HRI       string `json:"hri"` // "below", "above", "none"
}

// BuildHTMLFromJSON toma el string JSON del payload y genera un documento HTML5
// completo con CSS térmico optimizado para renderizado headless y posterior rasterizado.
func BuildHTMLFromJSON(prof *printer.DeviceProfile, payloadJSON string) (string, int, *PrintPayload, error) {
	var payload PrintPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return "", 0, nil, fmt.Errorf("deserializar payload JSON: %w", err)
	}
	if len(payload.Body) == 0 {
		return "", 0, nil, fmt.Errorf("el payload no contiene bloques en body")
	}
	payload.applyLegacyMargins()

	widthDots := 384
	if prof != nil && prof.WidthDots > 0 {
		widthDots = prof.WidthDots
	} else if payload.PaperProperties.Width > 0 {
		t := (payload.PaperProperties.Width - 58.0) / (80.0 - 58.0)
		d := 384.0 + t*(576.0-384.0)
		widthDots = int(math.Round(d))
		if widthDots < 200 {
			widthDots = 384
		}
	}

	htmlContent, err := BuildHTML(&payload, widthDots)
	if err != nil {
		return "", 0, nil, err
	}
	return htmlContent, widthDots, &payload, nil
}

// BuildHTML convierte el struct PrintPayload en un documento HTML5 completo.
func BuildHTML(payload *PrintPayload, widthDots int) (string, error) {
	var bodyBuf bytes.Buffer

	// scale se comporta como un device-pixel-ratio (el "DPI" de una pantalla):
	// el contenido se maqueta en un lienzo lógico más angosto (widthDots / scale)
	// y luego `zoom` lo reamplía al ancho físico real en dots. Efecto: la
	// tipografía y todo el layout crecen scale× en dots reales, mientras que el
	// texto, las columnas y las tablas reflowean dentro del MISMO ancho de papel,
	// sin recortes. Con scale == 1 no se emite `zoom` y el layout queda byte a
	// byte igual que antes.
	requestedScale := payload.PaperProperties.Scale
	if requestedScale <= 0 {
		requestedScale = DefaultScale
	}
	scale := payload.PaperProperties.EffectiveScale()
	logicalDivisor := 1.0
	if scale != DefaultScale {
		logicalDivisor = scale
	}
	ticketW := int(math.Round(float64(widthDots) / logicalDivisor))

	// Línea de calibración: SOLO para pruebas. Se emite únicamente si
	// USQAY_SCALE_DEBUG está activo; identifica el scale pedido, el efectivo y
	// el modelo que produjeron el raster. En producción no sale nada que no
	// esté en el JSON.
	if scale != DefaultScale && scaleDebugEnabled() {
		fmt.Fprintf(&bodyBuf, `<div class="text-block align-left" style="font-size:11px;font-weight:400;">· scale req=%s eff=%s · model=%s ·</div>`+"\n",
			strconv.FormatFloat(requestedScale, 'f', -1, 64),
			strconv.FormatFloat(scale, 'f', -1, 64), scaleModelVersion)
	}

	for _, raw := range payload.Body {
		var env blockEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}

		switch env.Type {
		case "text":
			var b textBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				renderTextHTML(&bodyBuf, b)
			}
		case "separator":
			var b separatorBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				renderSeparatorHTML(&bodyBuf, b)
			}
		case "spacer":
			var b spacerBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				renderSpacerHTML(&bodyBuf, b)
			}
		case "columns":
			var b columnsBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				renderColumnsHTML(&bodyBuf, b)
			}
		case "table":
			var b tableBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				renderTableHTML(&bodyBuf, b)
			}
		case "image":
			var b imageBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				renderImageHTML(&bodyBuf, b, ticketW)
			}
		case "qr":
			var b qrBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				renderQRHTML(&bodyBuf, b)
			}
		case "barcode":
			var b barcodeBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				renderBarcodeHTML(&bodyBuf, b)
			}
		}
	}

	// Conversión de márgenes mm a píxeles (203 DPI = ~8 dots/mm), expresados en
	// el lienzo lógico: se dividen por el mismo divisor que el ancho para que,
	// tras el `zoom`, equivalgan a los milímetros físicos solicitados.
	padTop := int(math.Round(payload.PaperProperties.TopMM() * 8.0 / logicalDivisor))
	padRight := int(math.Round(payload.PaperProperties.RightMM() * 8.0 / logicalDivisor))
	padBottom := int(math.Round(payload.PaperProperties.BottomMM() * 8.0 / logicalDivisor))
	padLeft := int(math.Round(payload.PaperProperties.LeftMM() * 8.0 / logicalDivisor))

	scaleRule := ""
	if scale != DefaultScale {
		scaleRule = fmt.Sprintf("    zoom: %s;\n", strconv.FormatFloat(scale, 'f', -1, 64))
	}

	fullHTML := fmt.Sprintf(`<!DOCTYPE html>
<html lang="es">
<head>
<meta charset="UTF-8">
<style>
  @font-face {
    font-family: 'CaskaydiaCove';
    src: url('/assets/font.ttf') format('truetype');
    font-weight: 100 900;
    font-style: normal;
    font-display: block;
  }
  * {
    box-sizing: border-box;
    margin: 0;
    padding: 0;
    -webkit-font-smoothing: none;
    text-rendering: geometricPrecision;
  }
  body {
    margin: 0;
    padding: 0;
    background-color: #ffffff;
    color: #000000;
    font-family: 'CaskaydiaCove', 'Cascadia Code', 'Cascadia Mono', Consolas, monospace;
    font-size: 15px;
    line-height: 1.25;
    font-weight: 600;
  }
  #ticket {
    width: %dpx;
    padding: %dpx %dpx %dpx %dpx;
    background-color: #ffffff;
    overflow-wrap: break-word;
    word-break: normal;
%s  }
  .align-left { text-align: left; }
  .align-center { text-align: center; }
  .align-right { text-align: right; }
  .bold { font-weight: 800; }
  .size-normal { font-size: 15px; line-height: 1.25; }
  .size-medium { font-size: 20px; line-height: 1.25; font-weight: 800; }
  .size-double { font-size: 28px; line-height: 1.2; font-weight: 900; }

  .text-block {
    overflow-wrap: break-word;
    word-break: normal;
    white-space: pre-wrap;
    margin-bottom: 2px;
  }

  .separator-dashed {
    border-top: 1.5px dashed #000000;
    margin: 5px 0;
    width: 100%%;
  }
  .separator-solid {
    border-top: 2px solid #000000;
    margin: 5px 0;
    width: 100%%;
  }
  .separator-double {
    border-top: 3px double #000000;
    margin: 5px 0;
    width: 100%%;
  }

  .columns-row {
    display: flex;
    width: 100%%;
    margin-bottom: 2px;
    align-items: flex-start;
  }
  .column-cell {
    min-width: 0;
    overflow-wrap: break-word;
    word-break: normal;
    white-space: pre-wrap;
  }

  table.ticket-table {
    width: 100%%;
    border-collapse: collapse;
    table-layout: fixed;
    margin-bottom: 3px;
  }
  table.ticket-table th, table.ticket-table td {
    vertical-align: top;
    padding: 1px 1px;
    overflow-wrap: break-word;
    word-break: normal;
    white-space: pre-wrap;
  }

  .image-container {
    display: flex;
    margin: 4px 0;
  }
  .image-container.align-left { justify-content: flex-start; }
  .image-container.align-center { justify-content: center; }
  .image-container.align-right { justify-content: flex-end; }

  .qr-container, .barcode-container {
    display: flex;
    flex-direction: column;
    margin: 6px 0;
  }
  .qr-container.align-left, .barcode-container.align-left { align-items: flex-start; }
  .qr-container.align-center, .barcode-container.align-center { align-items: center; }
  .qr-container.align-right, .barcode-container.align-right { align-items: flex-end; }
  
  .barcode-hri {
    font-family: 'CaskaydiaCove', 'Cascadia Code', monospace;
    font-size: 13px;
    font-weight: 700;
    margin-top: 2px;
  }

  svg {
    shape-rendering: crispEdges;
  }
</style>
</head>
<body>
<div id="ticket">
%s
</div>
</body>
</html>`, ticketW, padTop, padRight, padBottom, padLeft, scaleRule, bodyBuf.String())

	return fullHTML, nil
}

func renderTextHTML(buf *bytes.Buffer, b textBlock) {
	align := sanitizeAlign(b.Align)
	sizeClass := sanitizeSize(b.Size)
	boldClass := ""
	if b.Bold {
		boldClass = "bold"
	}
	escaped := html.EscapeString(b.Value)
	buf.WriteString(fmt.Sprintf(`<div class="text-block align-%s %s %s">%s</div>`+"\n",
		align, boldClass, sizeClass, escaped))
}

func renderSeparatorHTML(buf *bytes.Buffer, b separatorBlock) {
	char := strings.TrimSpace(b.Character)
	class := "separator-dashed"
	if char == "=" {
		class = "separator-double"
	} else if char == "_" {
		class = "separator-solid"
	}
	buf.WriteString(fmt.Sprintf(`<div class="%s"></div>`+"\n", class))
}

func renderSpacerHTML(buf *bytes.Buffer, b spacerBlock) {
	lines := b.Lines
	if lines <= 0 {
		lines = 1
	}
	buf.WriteString(fmt.Sprintf(`<div style="height: %dem;"></div>`+"\n", lines))
}

func normalizeWidthPercent(w float64) float64 {
	if w <= 0 {
		return 0
	}
	if w <= 1.0 {
		return w * 100.0
	}
	return w
}

func renderColumnsHTML(buf *bytes.Buffer, b columnsBlock) {
	if len(b.Columns) == 0 {
		return
	}
	buf.WriteString(`<div class="columns-row">` + "\n")
	for _, col := range b.Columns {
		align := sanitizeAlign(col.Align)
		sizeClass := sanitizeSize(col.Size)
		boldClass := ""
		if col.Bold {
			boldClass = "bold"
		}
		widthStyle := ""
		wPct := normalizeWidthPercent(col.Width)
		if wPct > 0 {
			widthStyle = fmt.Sprintf("width: %.2f%%;", wPct)
		} else {
			widthStyle = "flex: 1;"
		}
		escaped := html.EscapeString(col.Text)
		buf.WriteString(fmt.Sprintf(`  <div class="column-cell align-%s %s %s" style="%s">%s</div>`+"\n",
			align, boldClass, sizeClass, widthStyle, escaped))
	}
	buf.WriteString(`</div>` + "\n")
}

func renderTableHTML(buf *bytes.Buffer, b tableBlock) {
	if len(b.Columns) == 0 && len(b.Rows) == 0 {
		return
	}
	buf.WriteString(`<table class="ticket-table">` + "\n")

	// Colgroup para anchos de columna
	buf.WriteString(`  <colgroup>` + "\n")
	for _, col := range b.Columns {
		wPct := normalizeWidthPercent(col.Width)
		if wPct > 0 {
			buf.WriteString(fmt.Sprintf(`    <col style="width: %.2f%%;">`+"\n", wPct))
		} else {
			buf.WriteString(`    <col>` + "\n")
		}
	}
	buf.WriteString(`  </colgroup>` + "\n")

	// Headers si existen
	hasHeaders := false
	for _, col := range b.Columns {
		if col.Header != nil && *col.Header != "" {
			hasHeaders = true
			break
		}
	}
	if hasHeaders {
		buf.WriteString(`  <thead>` + "\n" + `    <tr>` + "\n")
		for _, col := range b.Columns {
			headerText := ""
			if col.Header != nil {
				headerText = *col.Header
			}
			align := sanitizeAlign(col.Align)
			buf.WriteString(fmt.Sprintf(`      <th class="align-%s bold">%s</th>`+"\n",
				align, html.EscapeString(headerText)))
		}
		buf.WriteString(`    </tr>` + "\n" + `  </thead>` + "\n")
	}

	// Filas
	buf.WriteString(`  <tbody>` + "\n")
	for _, row := range b.Rows {
		if row.Merge && len(row.Cells) > 0 {
			cell := row.Cells[0]
			bold := row.Bold
			if cell.Bold != nil {
				bold = *cell.Bold
			}
			boldClass := ""
			if bold {
				boldClass = "bold"
			}
			align := sanitizeAlign(cell.Align)
			sizeClass := sanitizeSize(cell.Size)
			colspan := len(b.Columns)
			if colspan == 0 {
				colspan = len(row.Cells)
			}
			buf.WriteString(fmt.Sprintf(`    <tr><td colspan="%d" class="align-%s %s %s">%s</td></tr>`+"\n",
				colspan, align, boldClass, sizeClass, html.EscapeString(cell.Text)))
			continue
		}

		buf.WriteString(`    <tr>` + "\n")
		for colIdx, cell := range row.Cells {
			align := ""
			if colIdx < len(b.Columns) {
				align = b.Columns[colIdx].Align
			}
			if cell.Align != "" {
				align = cell.Align
			}
			align = sanitizeAlign(align)

			bold := row.Bold
			if cell.Bold != nil {
				bold = *cell.Bold
			}
			boldClass := ""
			if bold {
				boldClass = "bold"
			}
			sizeClass := sanitizeSize(cell.Size)

			buf.WriteString(fmt.Sprintf(`      <td class="align-%s %s %s">%s</td>`+"\n",
				align, boldClass, sizeClass, html.EscapeString(cell.Text)))
		}
		buf.WriteString(`    </tr>` + "\n")
	}
	buf.WriteString(`  </tbody>` + "\n" + `</table>` + "\n")
}

func renderImageHTML(buf *bytes.Buffer, b imageBlock, maxW int) {
	data := strings.TrimSpace(b.Data)
	if data == "" {
		return
	}
	align := sanitizeAlign(b.Align)
	wStyle := ""
	if b.Width > 0 {
		w := b.Width
		if w > maxW {
			w = maxW
		}
		wStyle = fmt.Sprintf("width: %dpx; max-width: 100%%;", w)
	} else {
		wStyle = "max-width: 100%;"
	}

	src := data
	if !strings.HasPrefix(data, "data:") {
		src = "data:image/png;base64," + data
	}

	buf.WriteString(fmt.Sprintf(`<div class="image-container align-%s"><img src="%s" style="%s" /></div>`+"\n",
		align, src, wStyle))
}

func renderQRHTML(buf *bytes.Buffer, b qrBlock) {
	val := b.Value
	if val == "" {
		val = b.Data
	}
	if val == "" {
		return
	}

	align := sanitizeAlign(b.Align)
	opts := graphics.DefaultQROptions()
	if b.PixelWidth > 0 {
		opts.PixelWidth = b.PixelWidth
	} else if b.Size > 0 {
		opts.PixelWidth = b.Size * 30
	}
	if opts.PixelWidth < 96 {
		opts.PixelWidth = 120
	}

	img, err := graphics.ProcessQRImage(val, opts)
	if err != nil {
		buf.WriteString(fmt.Sprintf(`<!-- error generando QR: %s -->`+"\n", html.EscapeString(err.Error())))
		return
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		buf.WriteString(fmt.Sprintf(`<!-- error encoding QR: %s -->`+"\n", html.EscapeString(err.Error())))
		return
	}

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBuf.Bytes())
	buf.WriteString(fmt.Sprintf(`<div class="qr-container align-%s"><img src="%s" style="width: %dpx;" /></div>`+"\n",
		align, dataURI, opts.PixelWidth))
}

func renderBarcodeHTML(buf *bytes.Buffer, b barcodeBlock) {
	if b.Value == "" {
		return
	}
	align := sanitizeAlign(b.Align)
	h := b.Height
	if h <= 0 {
		h = 60
	}
	w := b.Width
	if w <= 0 {
		w = 2
	}

	svgStr, err := generateCode128SVG(b.Value, w, h)
	if err != nil {
		buf.WriteString(fmt.Sprintf(`<!-- error barcode: %s -->`+"\n", html.EscapeString(err.Error())))
		return
	}

	buf.WriteString(fmt.Sprintf(`<div class="barcode-container align-%s">`+"\n", align))
	if strings.ToLower(b.HRI) == "above" {
		buf.WriteString(fmt.Sprintf(`  <div class="barcode-hri">%s</div>`+"\n", html.EscapeString(b.Value)))
	}
	buf.WriteString("  " + svgStr + "\n")
	if strings.ToLower(b.HRI) == "below" || b.HRI == "" {
		buf.WriteString(fmt.Sprintf(`  <div class="barcode-hri">%s</div>`+"\n", html.EscapeString(b.Value)))
	}
	buf.WriteString(`</div>` + "\n")
}

var code128Patterns = [107]string{
	"212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213",
	"221312", "231212", "112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132",
	"221231", "213212", "223112", "312131", "311222", "321122", "321221", "312212", "322112", "322211",
	"212123", "212321", "232121", "111323", "131123", "131321", "112313", "132113", "132311", "211313",
	"231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121", "313121", "211331",
	"231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111",
	"314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214",
	"112412", "122114", "122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111",
	"111242", "121142", "121241", "114212", "124112", "124211", "411212", "421112", "421211", "212141",
	"214121", "412121", "111143", "111341", "131141", "114113", "114311", "411113", "411311", "113141",
	"114131", "311141", "411131", "211412", "211214", "211232", "2331112",
}

func generateCode128SVG(text string, moduleWidth, height int) (string, error) {
	if text == "" {
		return "", fmt.Errorf("texto de código de barras vacío")
	}
	if moduleWidth < 1 {
		moduleWidth = 2
	}
	if height < 10 {
		height = 50
	}

	symbols := make([]int, 0, len(text)+3)
	symbols = append(symbols, 104) // Start B
	sum := 104
	for i, r := range text {
		val := int(r) - 32
		if val < 0 || val > 95 {
			val = 0
		}
		symbols = append(symbols, val)
		sum += (i + 1) * val
	}
	symbols = append(symbols, sum%103, 106) // Checksum y Stop

	totalModules := 0
	for _, sym := range symbols {
		for _, ch := range code128Patterns[sym] {
			totalModules += int(ch - '0')
		}
	}

	svgW := totalModules * moduleWidth
	var svg bytes.Buffer
	svg.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+"\n",
		svgW, height, svgW, height))
	svg.WriteString(fmt.Sprintf(`  <rect width="%d" height="%d" fill="#ffffff"/>`+"\n", svgW, height))

	currX := 0
	for _, sym := range symbols {
		pattern := code128Patterns[sym]
		for idx, ch := range pattern {
			width := int(ch-'0') * moduleWidth
			isBar := (idx % 2) == 0
			if isBar {
				svg.WriteString(fmt.Sprintf(`  <rect x="%d" y="0" width="%d" height="%d" fill="#000000"/>`+"\n",
					currX, width, height))
			}
			currX += width
		}
	}
	svg.WriteString(`</svg>`)
	return svg.String(), nil
}

func sanitizeAlign(align string) string {
	switch strings.ToLower(strings.TrimSpace(align)) {
	case "center", "centro":
		return "center"
	case "right", "derecha", "der":
		return "right"
	default:
		return "left"
	}
}

func sanitizeSize(size string) string {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "medium", "mediano", "m":
		return "size-medium"
	case "double", "doble", "large", "l":
		return "size-double"
	default:
		return "size-normal"
	}
}

// ConvertBase64ToDataURI asegura que un string base64 tenga el prefijo data URI.
func ConvertBase64ToDataURI(data string) string {
	trimmed := strings.TrimSpace(data)
	if strings.HasPrefix(trimmed, "data:") {
		return trimmed
	}
	return "data:image/png;base64," + trimmed
}

// Helper para convertir enteros a string en templates
func itoa(i int) string {
	return strconv.Itoa(i)
}
