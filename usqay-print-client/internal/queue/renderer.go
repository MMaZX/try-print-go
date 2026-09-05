package queue

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"unicode/utf8"

	"usqay-print-client/internal/printer"
)

// --- Tipos del schema de payload ---

// PrintPayload es el payload raíz de un trabajo de impresión.
type PrintPayload struct {
	Options         PrintOptions     `json:"options"`
	PaperProperties PaperProperties  `json:"paper_properties"`
	Body            []json.RawMessage `json:"body"`
}

// PrintOptions controla el comportamiento del hardware.
type PrintOptions struct {
	Cut    bool `json:"cut"`
	Drawer bool `json:"drawer"`
}

// PaperProperties define la geometría física del papel, escala y márgenes de 4 lados.
type PaperProperties struct {
	Width   float64   `json:"width"`
	Scale   float64   `json:"scale"`
	Padding []float64 `json:"padding"` // Convención [top, right, bottom, left] en mm
}

// TopMM devuelve el margen superior en mm (default 0.0).
func (p PaperProperties) TopMM() float64 {
	if len(p.Padding) > 0 {
		return p.Padding[0]
	}
	return 0.0
}

// RightMM devuelve el margen derecho en mm (default 0.0).
func (p PaperProperties) RightMM() float64 {
	if len(p.Padding) > 1 {
		return p.Padding[1]
	}
	return 0.0
}

// BottomMM devuelve el margen inferior en mm (default 0.0).
func (p PaperProperties) BottomMM() float64 {
	if len(p.Padding) > 2 {
		return p.Padding[2]
	}
	return 0.0
}

// LeftMM devuelve el margen izquierdo en mm (default 0.0).
func (p PaperProperties) LeftMM() float64 {
	if len(p.Padding) > 3 {
		return p.Padding[3]
	}
	return 0.0
}

// BaseDefaultScale es la escala estándar por defecto del motor de renderizado (1.4x de la fuente base).
const BaseDefaultScale = 1.4

// EffectiveScale devuelve el factor de escala global aplicando la base por defecto de 1.4.
func (p PaperProperties) EffectiveScale() float64 {
	scale := p.Scale
	if scale <= 0 {
		scale = 1.0
	}
	return BaseDefaultScale * scale
}

// blockEnvelope inspecciona el campo type sin deserializar el bloque completo.
type blockEnvelope struct {
	Type string `json:"type"`
}

// TextBlock representa un bloque type:"text".
type TextBlock struct {
	Value string `json:"value"`
	Align string `json:"align"`
	Bold  bool   `json:"bold"`
	Size  string `json:"size"`
}

// SeparatorBlock representa un bloque type:"separator".
type SeparatorBlock struct {
	Character string `json:"character"`
}

// SpacerBlock representa un bloque type:"spacer".
type SpacerBlock struct {
	Lines int `json:"lines"`
}

// TableColumn define una columna dentro de un bloque table.
type TableColumn struct {
	Header *string `json:"header"` // nil cuando no se especifica
	Width  float64 `json:"width"`
	Align  string  `json:"align"`
}

// TableCell es una celda individual dentro de una fila de tabla.
// Bold es puntero para distinguir "no especificado" (hereda de la fila) de "false explícito".
type TableCell struct {
	Text  string `json:"text"`
	Bold  *bool  `json:"bold"`
	Size  string `json:"size"`
	Align string `json:"align"`
}

// TableRow es una fila del cuerpo de una tabla.
type TableRow struct {
	Cells []TableCell `json:"cells"`
	Bold  bool        `json:"bold"`
	Merge bool        `json:"merge"`
}

// TableBlock representa un bloque type:"table".
// Columns es nil en modo automático (anchos iguales, sin encabezado).
type TableBlock struct {
	Columns []TableColumn `json:"columns"`
	Rows    []TableRow    `json:"rows"`
}

// ColumnsCell es una celda dentro de un bloque type:"columns".
type ColumnsCell struct {
	Text  string  `json:"text"`
	Width float64 `json:"width"`
	Align string  `json:"align"`
	Bold  bool    `json:"bold"`
	Size  string  `json:"size"`
}

// ColumnsBlock representa un bloque type:"columns".
type ColumnsBlock struct {
	Columns []ColumnsCell `json:"columns"`
}

// QRBlock representa un bloque type:"qr".
type QRBlock struct {
	Value string `json:"value"`
	Align string `json:"align"`
	Size  int    `json:"size"`
}

// BarcodeBlock representa un bloque type:"barcode".
type BarcodeBlock struct {
	Symbology string `json:"symbology"`
	Value     string `json:"value"`
	Align     string `json:"align"`
	Height    int    `json:"height"`
	Width     int    `json:"width"`
	HRI       string `json:"hri"`
}

// ImageBlock representa un bloque type:"image".
type ImageBlock struct {
	Data   string `json:"data"`
	Align  string `json:"align,omitempty"`
	Width  int    `json:"width,omitempty"`
	Dither bool   `json:"dither,omitempty"`
}

// --- Puntos de entrada ---

// renderText convierte un payload JSON a texto UTF-8 plano para impresoras no térmicas.
// El resultado termina con \f (form feed) para expulsar la hoja.
func renderText(profile *printer.DeviceProfile, payload string) ([]byte, error) {
	var p PrintPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return nil, fmt.Errorf("parsear payload: %w", err)
	}
	if len(p.Body) == 0 {
		return nil, fmt.Errorf("payload sin bloques en body")
	}
	return renderStructuredText(profile, p)
}

func resolveActiveProfile(prof *printer.DeviceProfile, paper PaperProperties) printer.DeviceProfile {
	if prof != nil {
		return *prof
	}
	anchoPapelMM := paper.Width
	if anchoPapelMM > 0 {
		return printer.DeriveProfileFromWidth(anchoPapelMM)
	}
	return printer.Default58mmProfile()
}

// --- Renderer de texto plano ---

func renderStructuredText(prof *printer.DeviceProfile, payload PrintPayload) ([]byte, error) {
	activeProf := resolveActiveProfile(prof, payload.PaperProperties)
	dots := activeProf.WidthDots
	charWidth := activeProf.CharWidthDots
	if charWidth <= 0 {
		charWidth = 12
	}

	leftPadDots := int(math.Round(payload.PaperProperties.LeftMM() * 8.0))
	rightPadDots := int(math.Round(payload.PaperProperties.RightMM() * 8.0))
	if leftPadDots > 0 || rightPadDots > 0 {
		dots = dots - (leftPadDots + rightPadDots)
		if dots < 96 {
			dots = 96
		}
	}
	width := dots / charWidth
	var sb strings.Builder

	for _, raw := range payload.Body {
		var env blockEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}

		switch env.Type {
		case "text":
			var block TextBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			sb.WriteString(applyAlignText(block.Value, block.Align, width) + "\n")

		case "separator":
			var block SeparatorBlock
			json.Unmarshal(raw, &block) //nolint:errcheck
			char := orDefault(block.Character, "-")
			sb.WriteString(strings.Repeat(char, width) + "\n")

		case "spacer":
			var block SpacerBlock
			json.Unmarshal(raw, &block) //nolint:errcheck
			sb.WriteString(strings.Repeat("\n", positiveOrDefault(block.Lines, 1)))

		case "table":
			var block TableBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			renderTableText(&sb, block, width)

		case "columns":
			var block ColumnsBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			renderColumnsText(&sb, block, width)

		case "qr":
			var block QRBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			sb.WriteString(applyAlignText(fmt.Sprintf("[QR: %s]", block.Value), block.Align, width) + "\n")

		case "barcode":
			var block BarcodeBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			label := fmt.Sprintf("[BARCODE %s: %s]", block.Symbology, block.Value)
			sb.WriteString(applyAlignText(label, block.Align, width) + "\n")

		case "image":
			sb.WriteString("[IMAGE]\n")

		default:
			slog.Warn("tipo de bloque desconocido en renderText, saltando", "type", env.Type)
		}
	}

	sb.WriteString("\n\n\n\f")
	return []byte(sb.String()), nil
}

// renderTableText renderiza un bloque table a texto plano.
func renderTableText(sb *strings.Builder, block TableBlock, totalWidth int) {
	colWidths, colAligns := resolveTableLayout(block, totalWidth)

	if hasTableHeaders(block.Columns) {
		headers := make([]string, len(block.Columns))
		for i, col := range block.Columns {
			if col.Header != nil {
				headers[i] = *col.Header
			}
		}
		sb.WriteString(formatColumns(headers, colWidths, colAligns) + "\n")
		sb.WriteString(strings.Repeat("-", totalWidth) + "\n")
	}

	for _, row := range block.Rows {
		if row.Merge {
			if len(row.Cells) > 0 {
				cell := row.Cells[0]
				align := orDefault(cell.Align, "left")
				sb.WriteString(formatCol(cell.Text, totalWidth, align) + "\n")
			}
			continue
		}
		var line strings.Builder
		for i, cell := range row.Cells {
			if i >= len(colWidths) {
				break
			}
			align := resolveAlign(cell.Align, colAlignAt(colAligns, i))
			line.WriteString(formatCol(cell.Text, colWidths[i], align))
		}
		sb.WriteString(line.String() + "\n")
	}
}

// renderColumnsText renderiza un bloque type:"columns" a texto plano.
func renderColumnsText(sb *strings.Builder, block ColumnsBlock, totalWidth int) {
	colWidths := layoutColumnWidths(block.Columns, totalWidth)
	var line strings.Builder
	for i, col := range block.Columns {
		align := orDefault(col.Align, "left")
		line.WriteString(formatCol(col.Text, colWidths[i], align))
	}
	sb.WriteString(line.String() + "\n")
}

// --- Helpers de layout ---

// Anclas de calibración conocidas para rollos térmicos ESC/POS estándar:
// ancho nominal de papel (mm) → área imprimible real en dots (fuente A,
// 203dpi). Estos valores no son un simple mm×dots_por_mm porque el área
// imprimible real descuenta el margen mecánico no imprimible del cabezal;
// son los que documentan los fabricantes (Epson, etc.) para 58mm y 80mm.
const (
	anchoRef1MM, dotsRef1 = 58.0, 384.0
	anchoRef2MM, dotsRef2 = 80.0, 576.0
	dotsPerChar           = 12 // ancho de celda de carácter, fuente A tamaño normal
)

// paperGeometry calcula el ancho imprimible en dots y en caracteres a partir
// del ancho de papel en mm que llega en el payload (ancho_dimension). Es un
// cálculo continuo por interpolación/extrapolación lineal entre las dos
// anclas conocidas (58mm y 80mm) — no un snap a un valor fijo — para que
// anchos personalizados (62, 70, 76, 90mm...) definidos desde el frontend se
// reflejen proporcionalmente en vez de perderse dentro de un bucket.
//
// Si el resultado excede el área imprimible real del cabezal físico, la
// propia impresora recorta el exceso al recibir GS W (ver
// escpos.PrintAreaWidth), así que extrapolar fuera de 58–80mm es seguro,
// aunque pierde precisión mientras más lejos de ese rango esté el valor.
func paperGeometry(anchoDimMM float64) (dots, chars int) {
	if anchoDimMM <= 0 {
		anchoDimMM = anchoRef1MM // sin configurar: usar el ancho térmico más angosto conocido (58mm) por seguridad
	}
	frac := (anchoDimMM - anchoRef1MM) / (anchoRef2MM - anchoRef1MM)
	d := dotsRef1 + frac*(dotsRef2-dotsRef1)
	dots = int(math.Round(d))
	if dots < dotsPerChar {
		dots = dotsPerChar
	}
	chars = dots / dotsPerChar
	return dots, chars
}

// pageWidth devuelve el ancho en caracteres según el ancho físico del papel.
func pageWidth(anchoDim float64) int {
	_, chars := paperGeometry(anchoDim)
	return chars
}

// applyAlignText aplica alineación a un string para el renderer de texto plano.
// Cuenta runas (no bytes) por el mismo motivo que formatCol.
func applyAlignText(s, align string, width int) string {
	n := utf8.RuneCountInString(s)
	switch align {
	case "center":
		if n >= width {
			return s
		}
		pad := (width - n) / 2
		return strings.Repeat(" ", pad) + s
	case "right":
		if n >= width {
			return s
		}
		return strings.Repeat(" ", width-n) + s
	default:
		return s
	}
}

// resolveTableLayout calcula anchos y alineaciones de columnas para un TableBlock.
// Si Columns es nil, distribuye el ancho en partes iguales sin encabezado.
func resolveTableLayout(block TableBlock, totalWidth int) (colWidths []int, colAligns []string) {
	if len(block.Columns) == 0 {
		n := 1
		if len(block.Rows) > 0 && len(block.Rows[0].Cells) > 0 {
			n = len(block.Rows[0].Cells)
		}
		w := totalWidth / n
		rem := totalWidth - w*n
		for i := range n {
			width := w
			if i == n-1 {
				width += rem
			}
			colWidths = append(colWidths, width)
			colAligns = append(colAligns, "left")
		}
		return
	}

	sum := 0
	for _, col := range block.Columns {
		colAligns = append(colAligns, orDefault(col.Align, "left"))
		w := int(col.Width * float64(totalWidth))
		if w < 1 {
			w = 1
		}
		colWidths = append(colWidths, w)
		sum += w
	}
	if sum != totalWidth && len(colWidths) > 0 {
		colWidths[len(colWidths)-1] += totalWidth - sum
	}
	return
}

// hasTableHeaders devuelve true si al menos una columna tiene header no vacío.
func hasTableHeaders(columns []TableColumn) bool {
	for _, col := range columns {
		if col.Header != nil && *col.Header != "" {
			return true
		}
	}
	return false
}

// layoutColumnWidths calcula los anchos en caracteres para un bloque columns.
func layoutColumnWidths(cols []ColumnsCell, totalWidth int) []int {
	widths := make([]int, len(cols))
	sum := 0
	for i, col := range cols {
		w := int(col.Width * float64(totalWidth))
		if w < 1 {
			w = 1
		}
		widths[i] = w
		sum += w
	}
	if sum != totalWidth && len(widths) > 0 {
		widths[len(widths)-1] += totalWidth - sum
	}
	return widths
}

// formatCol ajusta text al ancho colWidth con la alineación indicada.
// Cuenta runas (no bytes) para que texto acentuado no desalinee las columnas:
// en la impresora cada runa ocupa exactamente una celda tras transcodificar a CP850.
func formatCol(text string, colWidth int, align string) string {
	runes := []rune(text)
	if len(runes) > colWidth {
		return string(runes[:colWidth])
	}
	pad := colWidth - len(runes)
	switch align {
	case "right":
		return strings.Repeat(" ", pad) + text
	case "center":
		left := pad / 2
		return strings.Repeat(" ", left) + text + strings.Repeat(" ", pad-left)
	default: // "left"
		return text + strings.Repeat(" ", pad)
	}
}

// formatColumns construye la línea de texto para un conjunto de columnas.
func formatColumns(cols []string, colWidths []int, aligns []string) string {
	var sb strings.Builder
	for i, text := range cols {
		if i >= len(colWidths) {
			break
		}
		sb.WriteString(formatCol(text, colWidths[i], colAlignAt(aligns, i)))
	}
	return sb.String()
}

// resolveAlign devuelve cellAlign si no está vacío, de lo contrario colAlign.
func resolveAlign(cellAlign, colAlign string) string {
	if cellAlign != "" {
		return cellAlign
	}
	return colAlign
}

// colAlignAt devuelve la alineación de la columna i, o "left" si está fuera de rango.
func colAlignAt(aligns []string, i int) string {
	if i < len(aligns) {
		return aligns[i]
	}
	return "left"
}

// orDefault devuelve s si no está vacío, de lo contrario def.
func orDefault(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

// positiveOrDefault devuelve n si es positivo, de lo contrario def.
func positiveOrDefault(n, def int) int {
	if n > 0 {
		return n
	}
	return def
}
