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
	Margins         *legacyMargins   `json:"margins,omitempty"`
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

// PrintOptions controla el comportamiento del hardware.
type PrintOptions struct {
	Cut    bool `json:"cut"`
	Drawer bool `json:"drawer"`
}

// PaperProperties define la geometría física del papel, escala y márgenes de 4 lados.
// Scale se parsea para compatibilidad de schema; su aplicación vive en el motor
// htmlrender (PaperProperties.EffectiveScale), no en el renderer de texto plano.
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
	p.applyLegacyMargins()
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

// renderTableText renderiza un bloque table a texto plano. Comparte con
// drawTable (render_thermal.go) el mismo layout de wrap por celda
// (wrapRowCells/formatRowLine) para que ambos motores produzcan la misma
// estructura de tabla — la única diferencia es que acá no hay negrita/tamaño.
func renderTableText(sb *strings.Builder, block TableBlock, totalWidth int) {
	colWidths, colAligns := resolveTableLayout(block, totalWidth)

	if hasTableHeaders(block.Columns) {
		headerRow := TableRow{Cells: make([]TableCell, len(block.Columns))}
		for i, col := range block.Columns {
			if col.Header != nil {
				headerRow.Cells[i] = TableCell{Text: *col.Header}
			}
		}
		cells, maxLines := wrapRowCells(headerRow, colWidths, colAligns)
		for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
			sb.WriteString(formatRowLine(cells, colWidths, lineIdx) + "\n")
		}
		sb.WriteString(strings.Repeat("-", totalWidth) + "\n")
	}

	for _, row := range block.Rows {
		if row.Merge {
			if len(row.Cells) == 0 {
				continue
			}
			align := orDefault(row.Cells[0].Align, "left")
			for _, line := range wrapCellLines(row.Cells[0].Text, totalWidth) {
				sb.WriteString(formatCol(line, totalWidth, align) + "\n")
			}
			continue
		}
		cells, maxLines := wrapRowCells(row, colWidths, colAligns)
		for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
			sb.WriteString(formatRowLine(cells, colWidths, lineIdx) + "\n")
		}
	}
}

// wrapCellLines envuelve text en tantas líneas de hasta width runas como
// haga falta, partiendo por espacios; una palabra más larga que width se
// corta a la fuerza para no bloquear el layout. Mismo criterio de word-wrap
// que ya usa el bloque "text" (ver drawText/PrintWrapped), contado en
// caracteres para poder compartirlo entre el renderer de texto plano y el
// motor de imagen (drawTable en render_thermal.go).
func wrapCellLines(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	var cur []rune
	for _, word := range words {
		wr := []rune(word)
		for len(wr) > width {
			if len(cur) > 0 {
				lines = append(lines, string(cur))
				cur = nil
			}
			lines = append(lines, string(wr[:width]))
			wr = wr[width:]
		}
		switch {
		case len(cur) == 0:
			cur = wr
		case len(cur)+1+len(wr) <= width:
			cur = append(cur, ' ')
			cur = append(cur, wr...)
		default:
			lines = append(lines, string(cur))
			cur = wr
		}
	}
	if len(cur) > 0 {
		lines = append(lines, string(cur))
	}
	return lines
}

// cellWrapWidth devuelve el ancho de wrap disponible en runas para una celda
// según su tamaño de fuente: "double" ocupa el doble de ancho por carácter
// en el motor de imagen, así que su límite efectivo de wrap es la mitad del
// ancho de columna. "medium" solo duplica el alto, no el ancho.
func cellWrapWidth(colWidth int, size string) int {
	if size == "double" {
		w := colWidth / 2
		if w < 1 {
			w = 1
		}
		return w
	}
	return colWidth
}

// wrappedCell son las líneas ya envueltas de una celda de tabla junto con el
// estilo efectivo resuelto (align heredado de columna, bold heredado de
// fila, size), listas para componerse línea por línea.
type wrappedCell struct {
	lines []string
	align string
	bold  bool
	size  string
}

// wrapRowCells resuelve el estilo efectivo de cada celda de una fila y
// envuelve su texto al ancho de su columna. Devuelve una celda por columna
// (rellenando con vacío si la fila trae menos celdas que columnas, para que
// la tabla no pierda su estructura) y el número de líneas que ocupa la fila
// completa (el máximo entre todas sus celdas envueltas).
func wrapRowCells(row TableRow, colWidths []int, colAligns []string) ([]wrappedCell, int) {
	cells := make([]wrappedCell, len(colWidths))
	maxLines := 1
	for i := range colWidths {
		var cell TableCell
		if i < len(row.Cells) {
			cell = row.Cells[i]
		}
		bold := row.Bold
		if cell.Bold != nil {
			bold = *cell.Bold
		}
		size := orDefault(cell.Size, "normal")
		align := resolveAlign(cell.Align, colAlignAt(colAligns, i))
		lines := wrapCellLines(cell.Text, cellWrapWidth(colWidths[i], size))
		cells[i] = wrappedCell{lines: lines, align: align, bold: bold, size: size}
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	return cells, maxLines
}

// formatRowLine compone la línea de texto lineIdx de una fila ya envuelta
// (wrapRowCells), alineando cada celda dentro de su ancho de columna. Las
// celdas con menos líneas que el máximo de la fila quedan en blanco a partir
// de ahí, para que las demás columnas mantengan su posición.
func formatRowLine(cells []wrappedCell, colWidths []int, lineIdx int) string {
	var sb strings.Builder
	for i, c := range cells {
		line := ""
		if lineIdx < len(c.lines) {
			line = c.lines[lineIdx]
		}
		sb.WriteString(formatCol(line, colWidths[i], c.align))
	}
	return sb.String()
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
