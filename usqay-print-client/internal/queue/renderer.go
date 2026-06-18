package queue

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"usqay-print-client/internal/escpos"
)

// PrintPayload represents the generic structured print job payload.
type PrintPayload struct {
	Options struct {
		Cut    bool `json:"cut"`
		Drawer bool `json:"drawer"`
	} `json:"options"`
	Margins struct {
		AnchoDimension  float64 `json:"ancho_dimension"`
		AlturaDimension float64 `json:"altura_dimension"`
	} `json:"margins"`
	Body []PrintBlock `json:"body"`
}

// PrintBlock represents an individual printing instruction block.
type PrintBlock struct {
	Type      string          `json:"type"`
	Value     string          `json:"value,omitempty"`
	Align     string          `json:"align,omitempty"`
	Bold      bool            `json:"bold,omitempty"`
	Size      string          `json:"size,omitempty"`
	Character string          `json:"character,omitempty"`
	Lines     int             `json:"lines,omitempty"`
	Headers   []string        `json:"headers,omitempty"`
	Widths    []float64       `json:"widths,omitempty"`
	Aligns    []string        `json:"aligns,omitempty"`
	Rows      json.RawMessage `json:"rows,omitempty"`
}

// comandaItemRow represents a single row in a comanda table.
type comandaItemRow struct {
	Cant        int     `json:"cant"`
	Producto    string  `json:"producto"`
	Notas       *string `json:"notas"`
	CategoriaID int     `json:"categoria_id"`
}

// Legacy comandaDoc matches the legacy JSON payload for "comanda" documents.
type comandaDoc struct {
	Mesa  int           `json:"mesa"`
	Items []comandaItem `json:"items"`
}

type comandaItem struct {
	Nombre   string  `json:"nombre"`
	Cantidad int     `json:"cantidad"`
	Precio   float64 `json:"precio,omitempty"`
}

// render converts a JSON payload to ESC/POS bytes for thermal printers.
func render(tipoDocumento, payload string) ([]byte, error) {
	var structured PrintPayload
	if err := json.Unmarshal([]byte(payload), &structured); err == nil && len(structured.Body) > 0 {
		return renderStructured(structured)
	}

	switch tipoDocumento {
	case "comanda":
		return renderComanda(payload)
	default:
		return nil, fmt.Errorf("tipo_documento desconocido: %q", tipoDocumento)
	}
}

// renderText converts a JSON payload to plain UTF-8 text for inkjet/laser printers.
// Output ends with \f (form feed) to eject the page on regular printers.
func renderText(tipoDocumento, payload string) ([]byte, error) {
	var structured PrintPayload
	if err := json.Unmarshal([]byte(payload), &structured); err == nil && len(structured.Body) > 0 {
		return renderStructuredText(structured)
	}

	switch tipoDocumento {
	case "comanda":
		return renderComandaText(payload)
	default:
		return nil, fmt.Errorf("tipo_documento desconocido: %q", tipoDocumento)
	}
}

// renderStructured converts a generic structured payload to ESC/POS bytes.
func renderStructured(payload PrintPayload) ([]byte, error) {
	b := escpos.New()

	// Handle drawer opening at the start of the ticket
	if payload.Options.Drawer {
		b.Drawer()
	}

	// Resolve page character width from margins
	width := 32
	if payload.Margins.AnchoDimension > 60 {
		width = 48
	}

	for _, block := range payload.Body {
		switch block.Type {
		case "text":
			switch block.Align {
			case "center":
				b.Center()
			case "right":
				b.Right()
			default:
				b.Left()
			}
			b.Bold(block.Bold)
			b.Size(block.Size)
			b.Line(block.Value)
			b.Size("normal")
			b.Bold(false)

		case "separator":
			char := "-"
			if block.Character != "" {
				char = block.Character
			}
			sepLine := strings.Repeat(char, width)
			if len(sepLine) > width {
				sepLine = sepLine[:width]
			}
			b.Left().Line(sepLine)

		case "spacer":
			lines := 1
			if block.Lines > 0 {
				lines = block.Lines
			}
			b.Feed(lines)

		case "table_comanda":
			var rows []comandaItemRow
			if err := json.Unmarshal(block.Rows, &rows); err != nil {
				continue
			}
			b.Left()
			for _, row := range rows {
				qtyStr := fmt.Sprintf("%-4d", row.Cant)
				b.Line(qtyStr + row.Producto)
				if row.Notas != nil && *row.Notas != "" {
					b.Line("    * " + *row.Notas)
				}
			}

		case "table":
			var rows []map[string]any
			if err := json.Unmarshal(block.Rows, &rows); err != nil {
				continue
			}
			if len(block.Headers) > 0 {
				headerLine := formatTableHeader(block.Headers, block.Widths, block.Aligns, width)
				b.Left().Bold(true).Line(headerLine).Bold(false)
				b.Left().Separator(width)
			}
			for _, row := range rows {
				rowLine := formatTableRow(row, block.Headers, block.Widths, block.Aligns, width)
				b.Left().Line(rowLine)
			}

		case "qr":
			switch block.Align {
			case "center":
				b.Center()
			case "right":
				b.Right()
			default:
				b.Left()
			}
			b.QR(block.Value, 6)
			b.Left() // reset alignment
		}
	}

	if payload.Options.Cut {
		b.Feed(3).Cut()
	} else {
		b.Feed(3)
	}

	return b.Bytes(), nil
}

// renderStructuredText converts a generic structured payload to plain UTF-8 text.
func renderStructuredText(payload PrintPayload) ([]byte, error) {
	width := 48
	if payload.Margins.AnchoDimension > 0 && payload.Margins.AnchoDimension <= 60 {
		width = 32
	}

	var sb strings.Builder

	center := func(s string) string {
		if len(s) >= width {
			return s
		}
		pad := (width - len(s)) / 2
		return strings.Repeat(" ", pad) + s
	}

	right := func(s string) string {
		if len(s) >= width {
			return s
		}
		pad := width - len(s)
		return strings.Repeat(" ", pad) + s
	}

	for _, block := range payload.Body {
		switch block.Type {
		case "text":
			text := block.Value
			if block.Align == "center" {
				sb.WriteString(center(text) + "\n")
			} else if block.Align == "right" {
				sb.WriteString(right(text) + "\n")
			} else {
				sb.WriteString(text + "\n")
			}

		case "separator":
			char := "-"
			if block.Character != "" {
				char = block.Character
			}
			sepLine := strings.Repeat(char, width)
			if len(sepLine) > width {
				sepLine = sepLine[:width]
			}
			sb.WriteString(sepLine + "\n")

		case "spacer":
			lines := 1
			if block.Lines > 0 {
				lines = block.Lines
			}
			sb.WriteString(strings.Repeat("\n", lines))

		case "table_comanda":
			var rows []comandaItemRow
			if err := json.Unmarshal(block.Rows, &rows); err != nil {
				continue
			}
			for _, row := range rows {
				qtyStr := fmt.Sprintf("%-4d", row.Cant)
				sb.WriteString(qtyStr + row.Producto + "\n")
				if row.Notas != nil && *row.Notas != "" {
					sb.WriteString("    * " + *row.Notas + "\n")
				}
			}

		case "table":
			var rows []map[string]any
			if err := json.Unmarshal(block.Rows, &rows); err != nil {
				continue
			}
			if len(block.Headers) > 0 {
				headerLine := formatTableHeader(block.Headers, block.Widths, block.Aligns, width)
				sb.WriteString(headerLine + "\n")
				sb.WriteString(strings.Repeat("-", width) + "\n")
			}
			for _, row := range rows {
				rowLine := formatTableRow(row, block.Headers, block.Widths, block.Aligns, width)
				sb.WriteString(rowLine + "\n")
			}

		case "qr":
			qrText := fmt.Sprintf("[QR CODE: %s]", block.Value)
			if block.Align == "center" {
				sb.WriteString(center(qrText) + "\n")
			} else if block.Align == "right" {
				sb.WriteString(right(qrText) + "\n")
			} else {
				sb.WriteString(qrText + "\n")
			}
		}
	}

	sb.WriteString("\n\n\n\f")

	return []byte(sb.String()), nil
}

// Helpers for table formatting

func calculateColWidths(widths []float64, totalWidth int) []int {
	if len(widths) == 0 {
		return []int{totalWidth}
	}
	colWidths := make([]int, len(widths))
	sum := 0
	for i, w := range widths {
		c := int(w * float64(totalWidth))
		if c < 1 {
			c = 1
		}
		colWidths[i] = c
		sum += c
	}
	if sum != totalWidth && len(colWidths) > 0 {
		colWidths[len(colWidths)-1] += (totalWidth - sum)
	}
	return colWidths
}

func formatCol(text string, colWidth int, align string) string {
	if len(text) > colWidth {
		return text[:colWidth]
	}
	pad := colWidth - len(text)
	switch align {
	case "right":
		return strings.Repeat(" ", pad) + text
	case "center":
		leftPad := pad / 2
		rightPad := pad - leftPad
		return strings.Repeat(" ", leftPad) + text + strings.Repeat(" ", rightPad)
	default: // "left"
		return text + strings.Repeat(" ", pad)
	}
}

func formatColumns(cols []string, colWidths []int, aligns []string) string {
	var sb strings.Builder
	for i, text := range cols {
		align := "left"
		if i < len(aligns) {
			align = aligns[i]
		}
		sb.WriteString(formatCol(text, colWidths[i], align))
	}
	return sb.String()
}

func formatTableHeader(headers []string, widths []float64, aligns []string, totalWidth int) string {
	colWidths := calculateColWidths(widths, totalWidth)
	return formatColumns(headers, colWidths, aligns)
}

func formatTableRow(row map[string]any, headers []string, widths []float64, aligns []string, totalWidth int) string {
	colWidths := calculateColWidths(widths, totalWidth)
	cols := make([]string, len(headers))
	for i, h := range headers {
		cols[i] = getRowValue(row, h)
	}
	return formatColumns(cols, colWidths, aligns)
}

func getRowValue(row map[string]any, header string) string {
	h := strings.ToLower(header)
	var keysToTry []string
	if strings.Contains(h, "cant") || strings.Contains(h, "qty") {
		keysToTry = []string{"cant", "cantidad", "qty", "cant."}
	} else if strings.Contains(h, "prod") || strings.Contains(h, "desc") || strings.Contains(h, "det") || strings.Contains(h, "item") {
		keysToTry = []string{"producto", "descripcion", "nombre", "nombre_producto", "item"}
	} else if strings.Contains(h, "tot") || strings.Contains(h, "prec") || strings.Contains(h, "sub") || strings.Contains(h, "s/") || strings.Contains(h, "val") {
		keysToTry = []string{"total", "precio", "subtotal", "valor"}
	}

	for _, k := range keysToTry {
		if val, ok := row[k]; ok {
			return fmt.Sprintf("%v", val)
		}
	}

	if val, ok := row[h]; ok {
		return fmt.Sprintf("%v", val)
	}

	return ""
}

// Legacy Comanda rendering functions (fallback)

func renderComandaText(payload string) ([]byte, error) {
	var doc comandaDoc
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return nil, fmt.Errorf("parsear payload de comanda: %w", err)
	}

	const width = 48
	now := time.Now()
	sep := strings.Repeat("-", width)

	center := func(s string) string {
		if len(s) >= width {
			return s
		}
		pad := (width - len(s)) / 2
		return strings.Repeat(" ", pad) + s
	}

	var sb strings.Builder
	sb.WriteString(center("RESTAURANTE USQAY") + "\n")
	sb.WriteString(center(fmt.Sprintf("Mesa #%d", doc.Mesa)) + "\n")
	sb.WriteString(sep + "\n")
	sb.WriteString(fmt.Sprintf("Fecha: %s\n", now.Format("02/01/2006")))
	sb.WriteString(fmt.Sprintf("Hora:  %s\n", now.Format("15:04:05")))
	sb.WriteString(sep + "\n")
	sb.WriteString(fmt.Sprintf("%-30s %3s %6s\n", "PRODUCTO", "CAN", "S/"))
	sb.WriteString(sep + "\n")

	var total float64
	for _, item := range doc.Items {
		subtotal := item.Precio * float64(item.Cantidad)
		total += subtotal
		if item.Precio > 0 {
			sb.WriteString(fmt.Sprintf("%-30s %3d %6.2f\n", item.Nombre, item.Cantidad, subtotal))
		} else {
			sb.WriteString(fmt.Sprintf("%-30s %3d\n", item.Nombre, item.Cantidad))
		}
	}

	sb.WriteString(sep + "\n")
	if total > 0 {
		sb.WriteString(center(fmt.Sprintf("TOTAL: S/ %.2f", total)) + "\n")
	}
	sb.WriteString("\n\n\n\f")

	return []byte(sb.String()), nil
}

func renderComanda(payload string) ([]byte, error) {
	var doc comandaDoc
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return nil, fmt.Errorf("parsear payload de comanda: %w", err)
	}

	const width = 32
	now := time.Now()

	b := escpos.New().
		Center().Bold(true).Line("RESTAURANTE USQAY").Bold(false).
		Center().Line(fmt.Sprintf("Mesa #%d", doc.Mesa)).
		Left().Separator(width).
		Left().Line(fmt.Sprintf("Fecha: %s", now.Format("02/01/2006"))).
		Left().Line(fmt.Sprintf("Hora:  %s", now.Format("15:04:05"))).
		Left().Separator(width).
		Bold(true).Line(fmt.Sprintf("%-20s %3s %5s", "PRODUCTO", "CAN", "S/")).Bold(false).
		Separator(width)

	var total float64
	for _, item := range doc.Items {
		subtotal := item.Precio * float64(item.Cantidad)
		total += subtotal
		if item.Precio > 0 {
			b.Line(fmt.Sprintf("%-20s %3d %5.2f", item.Nombre, item.Cantidad, subtotal))
		} else {
			b.Line(fmt.Sprintf("%-20s %3d", item.Nombre, item.Cantidad))
		}
	}

	b.Separator(width)
	if total > 0 {
		b.Right().Bold(true).Line(fmt.Sprintf("TOTAL: S/ %.2f", total)).Bold(false)
	}

	return b.Feed(3).Cut().Bytes(), nil
}
