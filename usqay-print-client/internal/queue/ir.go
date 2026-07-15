package queue

import (
	"strings"

	"usqay-print-client/internal/escpos"
)

// IRBlock represents a measured layout block that can render itself to ESC/POS.
type IRBlock interface {
	Measure(widthChars int)
	Render(b *escpos.Builder, widthChars int)
}

// IRText is a block of simple aligned text.
type IRText struct {
	Value string
	Align string
	Bold  bool
	Size  string
}

func (t *IRText) Measure(widthChars int) {}

func (t *IRText) Render(b *escpos.Builder, widthChars int) {
	applyAlign(b, t.Align)
	b.Bold(t.Bold).Size(t.Size).Line(t.Value)
	b.Bold(false).Size("normal")
}

// IRSeparator is a horizontal line of repeating characters.
type IRSeparator struct {
	Character string
}

func (s *IRSeparator) Measure(widthChars int) {}

func (s *IRSeparator) Render(b *escpos.Builder, widthChars int) {
	char := orDefault(s.Character, "-")
	b.Left().Line(strings.Repeat(char, widthChars))
}

// IRSpacer represents vertical whitespace spacing.
type IRSpacer struct {
	Lines int
}

func (s *IRSpacer) Measure(widthChars int) {}

func (s *IRSpacer) Render(b *escpos.Builder, widthChars int) {
	b.Feed(positiveOrDefault(s.Lines, 1))
}

// IRTable represents a structured table.
type IRTable struct {
	Columns []TableColumn
	Rows    []TableRow
}

func (t *IRTable) Measure(widthChars int) {}

func (t *IRTable) Render(b *escpos.Builder, widthChars int) {
	renderTableESCPOS(b, TableBlock{Columns: t.Columns, Rows: t.Rows}, widthChars)
}

// IRColumns represents a side-by-side columns block.
type IRColumns struct {
	Columns []ColumnsCell
}

func (c *IRColumns) Measure(widthChars int) {}

func (c *IRColumns) Render(b *escpos.Builder, widthChars int) {
	renderColumnsESCPOS(b, ColumnsBlock{Columns: c.Columns}, widthChars)
}

// IRQR represents a QR code block.
type IRQR struct {
	Value      string
	Align      string
	Size       int
	NativeQR   bool
}

func (q *IRQR) Measure(widthChars int) {}

func (q *IRQR) Render(b *escpos.Builder, widthChars int) {
	applyAlign(b, q.Align)
	if q.NativeQR {
		b.QR(q.Value, positiveOrDefault(q.Size, 6))
	} else {
		b.Line("[QR: " + q.Value + "]")
	}
	b.Left()
}

// IRBarcode represents a 1D barcode block.
type IRBarcode struct {
	Symbology string
	Value     string
	Align     string
	Height    int
	Width     int
	HRI       string
}

func (bc *IRBarcode) Measure(widthChars int) {}

func (bc *IRBarcode) Render(b *escpos.Builder, widthChars int) {
	applyAlign(b, bc.Align)
	b.Barcode(bc.Symbology, bc.Value, bc.Height, bc.Width, bc.HRI)
	b.Left()
}

// IRImage represents a raster image block.
type IRImage struct {
	Data           string
	Align          string
	Width          int
	SupportsRaster bool
}

func (img *IRImage) Measure(widthChars int) {}

func (img *IRImage) Render(b *escpos.Builder, widthChars int) {
	// Se implementará en la Fase 3
}
