package queue

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"usqay-print-client/internal/escpos"
)

// --- Tipos del schema de payload ---

// PrintPayload es el payload raíz de un trabajo de impresión.
type PrintPayload struct {
	Options PrintOptions  `json:"options"`
	Margins PrintMargins  `json:"margins"`
	Body    []json.RawMessage `json:"body"`
}

// PrintOptions controla el comportamiento del hardware.
type PrintOptions struct {
	Cut    bool `json:"cut"`
	Drawer bool `json:"drawer"`
}

// PrintMargins define el tamaño físico del papel.
type PrintMargins struct {
	AnchoDimension  float64 `json:"ancho_dimension"`
	AlturaDimension float64 `json:"altura_dimension"`
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

// --- Puntos de entrada ---

// render convierte un payload JSON a bytes ESC/POS para impresoras térmicas.
func render(_, payload string) ([]byte, error) {
	var p PrintPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return nil, fmt.Errorf("parsear payload: %w", err)
	}
	if len(p.Body) == 0 {
		return nil, fmt.Errorf("payload sin bloques en body")
	}
	return renderStructured(p)
}

// renderText convierte un payload JSON a texto UTF-8 plano para impresoras no térmicas.
// El resultado termina con \f (form feed) para expulsar la hoja.
func renderText(_, payload string) ([]byte, error) {
	var p PrintPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return nil, fmt.Errorf("parsear payload: %w", err)
	}
	if len(p.Body) == 0 {
		return nil, fmt.Errorf("payload sin bloques en body")
	}
	return renderStructuredText(p)
}

// --- Renderer ESC/POS ---

func renderStructured(payload PrintPayload) ([]byte, error) {
	b := escpos.New()
	if payload.Options.Drawer {
		b.Drawer()
	}
	width := pageWidth(payload.Margins.AnchoDimension)

	for _, raw := range payload.Body {
		var env blockEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			slog.Warn("skip bloque con JSON inválido", "err", err)
			continue
		}

		switch env.Type {
		case "text":
			var block TextBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				slog.Warn("skip bloque text inválido", "err", err)
				continue
			}
			applyAlign(b, block.Align)
			b.Bold(block.Bold).Size(block.Size).Line(block.Value)
			b.Bold(false).Size("normal")

		case "separator":
			var block SeparatorBlock
			json.Unmarshal(raw, &block) //nolint:errcheck // todos los campos son opcionales
			char := orDefault(block.Character, "-")
			b.Left().Line(strings.Repeat(char, width))

		case "spacer":
			var block SpacerBlock
			json.Unmarshal(raw, &block) //nolint:errcheck // todos los campos son opcionales
			b.Feed(positiveOrDefault(block.Lines, 1))

		case "table":
			var block TableBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				slog.Warn("skip bloque table inválido", "err", err)
				continue
			}
			renderTableESCPOS(b, block, width)

		case "columns":
			var block ColumnsBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				slog.Warn("skip bloque columns inválido", "err", err)
				continue
			}
			renderColumnsESCPOS(b, block, width)

		case "qr":
			var block QRBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				slog.Warn("skip bloque qr inválido", "err", err)
				continue
			}
			applyAlign(b, block.Align)
			b.QR(block.Value, positiveOrDefault(block.Size, 6))
			b.Left()

		case "barcode":
			var block BarcodeBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				slog.Warn("skip bloque barcode inválido", "err", err)
				continue
			}
			applyAlign(b, block.Align)
			b.Barcode(block.Symbology, block.Value, block.Height, block.Width, block.HRI)
			b.Left()

		case "image":
			slog.Warn("bloque image aún no implementado, saltando")

		default:
			slog.Warn("tipo de bloque desconocido, saltando", "type", env.Type)
		}
	}

	if payload.Options.Cut {
		b.Feed(3).Cut()
	} else {
		b.Feed(3)
	}
	return b.Bytes(), nil
}

// renderTableESCPOS renderiza un bloque table a comandos ESC/POS.
func renderTableESCPOS(b *escpos.Builder, block TableBlock, totalWidth int) {
	colWidths, colAligns := resolveTableLayout(block, totalWidth)

	if hasTableHeaders(block.Columns) {
		headers := make([]string, len(block.Columns))
		for i, col := range block.Columns {
			if col.Header != nil {
				headers[i] = *col.Header
			}
		}
		b.Left().Bold(true).Line(formatColumns(headers, colWidths, colAligns)).Bold(false)
		b.Left().Line(strings.Repeat("-", totalWidth))
	}

	for _, row := range block.Rows {
		b.Left()
		if row.Merge {
			renderMergeRowESCPOS(b, row, totalWidth)
		} else {
			renderTableRowESCPOS(b, row, colWidths, colAligns)
		}
	}
}

// renderMergeRowESCPOS imprime la primera celda de la fila ocupando el ancho completo.
func renderMergeRowESCPOS(b *escpos.Builder, row TableRow, totalWidth int) {
	if len(row.Cells) == 0 {
		return
	}
	cell := row.Cells[0]
	align := orDefault(cell.Align, "left")
	effectiveBold := row.Bold || (cell.Bold != nil && *cell.Bold)
	cellSize := orDefault(cell.Size, "normal")

	if effectiveBold {
		b.Bold(true)
	}
	if cellSize != "normal" {
		b.Size(cellSize)
	}
	b.Line(formatCol(cell.Text, totalWidth, align))
	if cellSize != "normal" {
		b.Size("normal")
	}
	if effectiveBold {
		b.Bold(false)
	}
}

// renderTableRowESCPOS imprime una fila normal con soporte de formato por celda.
func renderTableRowESCPOS(b *escpos.Builder, row TableRow, colWidths []int, colAligns []string) {
	// Detectar si alguna celda necesita formato individual.
	needsPerCell := false
	for _, cell := range row.Cells {
		if cell.Bold != nil || (cell.Size != "" && cell.Size != "normal") {
			needsPerCell = true
			break
		}
	}

	if !needsPerCell {
		// Camino simple: construir la línea como string.
		var sb strings.Builder
		for i, cell := range row.Cells {
			if i >= len(colWidths) {
				break
			}
			align := resolveAlign(cell.Align, colAlignAt(colAligns, i))
			sb.WriteString(formatCol(cell.Text, colWidths[i], align))
		}
		if row.Bold {
			b.Bold(true)
		}
		b.Line(sb.String())
		if row.Bold {
			b.Bold(false)
		}
		return
	}

	// Camino complejo: emitir segmentos con toggles de bold/size por celda.
	curBold := row.Bold
	if curBold {
		b.Bold(true)
	}

	for i, cell := range row.Cells {
		if i >= len(colWidths) {
			break
		}
		align := resolveAlign(cell.Align, colAlignAt(colAligns, i))

		cellBold := curBold
		if cell.Bold != nil {
			cellBold = *cell.Bold
		}
		if cellBold != curBold {
			b.Bold(cellBold)
			curBold = cellBold
		}

		cellSize := orDefault(cell.Size, "normal")
		if cellSize != "normal" {
			b.Size(cellSize)
			b.Text(cell.Text) // sin padding en tamaños no estándar
			b.Size("normal")
		} else {
			b.Text(formatCol(cell.Text, colWidths[i], align))
		}
	}

	if curBold {
		b.Bold(false)
	}
	b.Text("\n")
}

// renderColumnsESCPOS renderiza un bloque type:"columns" a comandos ESC/POS.
func renderColumnsESCPOS(b *escpos.Builder, block ColumnsBlock, totalWidth int) {
	colWidths := layoutColumnWidths(block.Columns, totalWidth)
	b.Left()
	for i, col := range block.Columns {
		align := orDefault(col.Align, "left")
		if col.Bold {
			b.Bold(true)
		}
		if col.Size != "" && col.Size != "normal" {
			b.Size(col.Size)
			b.Text(col.Text)
			b.Size("normal")
		} else {
			b.Text(formatCol(col.Text, colWidths[i], align))
		}
		if col.Bold {
			b.Bold(false)
		}
	}
	b.Text("\n")
}

// --- Renderer de texto plano ---

func renderStructuredText(payload PrintPayload) ([]byte, error) {
	width := pageWidth(payload.Margins.AnchoDimension)
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

// pageWidth devuelve el ancho en caracteres según el ancho físico del papel.
func pageWidth(anchoDim float64) int {
	if anchoDim > 60 {
		return 48
	}
	return 32
}

// applyAlign emite el comando ESC/POS de alineación correspondiente.
func applyAlign(b *escpos.Builder, align string) {
	switch align {
	case "center":
		b.Center()
	case "right":
		b.Right()
	default:
		b.Left()
	}
}

// applyAlignText aplica alineación a un string para el renderer de texto plano.
func applyAlignText(s, align string, width int) string {
	switch align {
	case "center":
		if len(s) >= width {
			return s
		}
		pad := (width - len(s)) / 2
		return strings.Repeat(" ", pad) + s
	case "right":
		if len(s) >= width {
			return s
		}
		return strings.Repeat(" ", width-len(s)) + s
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
func formatCol(text string, colWidth int, align string) string {
	if len(text) > colWidth {
		return text[:colWidth]
	}
	pad := colWidth - len(text)
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
