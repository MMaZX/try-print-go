package ticketrender

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math"
	"os"
	"strings"

	_ "golang.org/x/image/bmp"

	"github.com/adcondev/poster/pkg/commands/bitimage"
	"github.com/adcondev/poster/pkg/commands/character"
	"github.com/adcondev/poster/pkg/composer"
	"github.com/adcondev/poster/pkg/graphics"
	"github.com/adcondev/poster/pkg/profile"
	"github.com/adcondev/poster/pkg/service"

	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/ticketimage"
)

// ============================================================================
// 1. ESQUEMA DE DATOS DEL PAYLOAD JSON (Print Payload Schema)
// ============================================================================

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
	Cut    bool `json:"cut"`    // Cortar papel al finalizar el ticket
	Drawer bool `json:"drawer"` // Abrir gaveta de dinero antes de imprimir
}

// PaperProperties define las dimensiones físicas en mm y márgenes de 4 lados.
type PaperProperties struct {
	Width   float64   `json:"width"`   // Ancho del papel en mm (ej. 58.0, 70.0, 80.0)
	Scale   float64   `json:"scale"`   // Multiplicador sobre la base visual (default 1.0)
	Padding []float64 `json:"padding"` // Márgenes [top, right, bottom, left] en mm
}

func (p PaperProperties) LeftMM() float64 {
	if len(p.Padding) > 3 {
		return p.Padding[3]
	}
	return 0.0
}

func (p PaperProperties) RightMM() float64 {
	if len(p.Padding) > 1 {
		return p.Padding[1]
	}
	return 0.0
}

const baseDefaultScale = 1.4

func (p PaperProperties) effectiveScale() float64 {
	scale := p.Scale
	if scale <= 0 {
		scale = 1.0
	}
	return baseDefaultScale * scale
}

// blockEnvelope lee el campo "type" sin deserializar el bloque completo.
type blockEnvelope struct {
	Type string `json:"type"`
}

// Bloques primitivos del cuerpo (body)
type textBlock struct {
	Value string `json:"value"`
	Align string `json:"align"`
	Bold  bool   `json:"bold"`
	Size  string `json:"size"` // "normal", "medium", "double"
}

type separatorBlock struct {
	Character string `json:"character"` // "-", "=", etc.
}

type spacerBlock struct {
	Lines int `json:"lines"` // Cantidad de líneas en blanco
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

// ============================================================================
// 2. CONTEXTO GEOMÉTRICO Y RESOLUCIÓN FÍSICA
// ============================================================================

type renderContext struct {
	widthChars     int
	printWidthDots int
	totalPaperDots int
	leftMarginDots int
	dpi            int
	scale          float64
	profile        printer.DeviceProfile
}

func resolveRenderContext(prof *printer.DeviceProfile, paper PaperProperties) renderContext {
	var activeProf printer.DeviceProfile
	if prof != nil {
		activeProf = *prof
	} else if paper.Width > 0 {
		activeProf = printer.DeriveProfileFromWidth(paper.Width)
	} else {
		activeProf = printer.Default58mmProfile()
	}

	dots := activeProf.WidthDots
	scale := paper.effectiveScale()

	charWidth := int(math.Round(float64(activeProf.CharWidthDots) * scale))
	if charWidth <= 0 {
		charWidth = 12
	}

	totalPaperDots := dots
	if paper.Width > 0 {
		t := (paper.Width - 58.0) / (80.0 - 58.0)
		d := 384.0 + t*(576.0-384.0)
		totalPaperDots = int(math.Round(d))
	}

	leftMarginDots := 0
	printWidthDots := dots
	if totalPaperDots > dots {
		leftMarginDots = (totalPaperDots - dots) / 2
	}

	leftPadDots := int(math.Round(paper.LeftMM() * 8.0))
	rightPadDots := int(math.Round(paper.RightMM() * 8.0))
	if leftPadDots > 0 || rightPadDots > 0 {
		leftMarginDots += leftPadDots
		printWidthDots -= (leftPadDots + rightPadDots)
		if printWidthDots < 96 {
			printWidthDots = 96
		}
	}

	widthChars := printWidthDots / charWidth
	if widthChars < 16 {
		widthChars = 16
	}

	dpi := activeProf.DPI
	if dpi <= 0 {
		dpi = 203
	}

	return renderContext{
		widthChars:     widthChars,
		printWidthDots: printWidthDots,
		totalPaperDots: totalPaperDots,
		leftMarginDots: leftMarginDots,
		dpi:            dpi,
		scale:          scale,
		profile:        activeProf,
	}
}

func newCanvasEngine(ctx renderContext) (*ticketimage.Engine, error) {
	cfg := ticketimage.Config{
		PaperPxWidth:            ctx.printWidthDots,
		DPI:                     ctx.dpi,
		AutoAdjustCursorOnScale: false,
	}
	if ctx.scale > 0 && ctx.scale != 1.0 {
		baseAWidth := float64(ctx.profile.CharWidthDots)
		if baseAWidth <= 0 {
			baseAWidth = 12.0
		}
		cfg.FontAWidth = baseAWidth * ctx.scale
		cfg.FontAHeight = (baseAWidth * 2.0) * ctx.scale
		cfg.FontBWidth = (baseAWidth * 0.75) * ctx.scale
		cfg.FontBHeight = (baseAWidth * 2.0) * ctx.scale
	}
	return ticketimage.NewEngine(cfg)
}

// ============================================================================
// 3. PUNTOS DE ENTRADA PÚBLICOS (API UNIFICADA)
// ============================================================================

// RenderToImage toma un JSON de impresión y lo renderiza como una imagen image.Image
// en memoria con exactitud milimétrica respecto al ticket físico.
func RenderToImage(prof *printer.DeviceProfile, payloadJSON string) (image.Image, error) {
	var p PrintPayload
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return nil, fmt.Errorf("deserializar payload JSON: %w", err)
	}
	if len(p.Body) == 0 {
		return nil, fmt.Errorf("el payload no contiene bloques en body")
	}
	p.applyLegacyMargins()

	ctx := resolveRenderContext(prof, p.PaperProperties)
	eng, err := newCanvasEngine(ctx)
	if err != nil {
		return nil, fmt.Errorf("inicializar lienzo de ticket: %w", err)
	}

	for i, raw := range p.Body {
		var env blockEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			slog.Warn("skip bloque con JSON corrupto", "indice", i, "err", err)
			continue
		}
		if err := renderBlock(eng, env.Type, raw, ctx); err != nil {
			return nil, fmt.Errorf("renderizar bloque %q (índice %d): %w", env.Type, i, err)
		}
	}

	return eng.Render(), nil
}

// RenderToPNG renderiza el payload JSON a formato PNG directamente sobre un io.Writer.
func RenderToPNG(prof *printer.DeviceProfile, payloadJSON string, w io.Writer) error {
	img, err := RenderToImage(prof, payloadJSON)
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}

// RenderToPNGFile renderiza el payload JSON y lo guarda en el archivo destino especificado.
func RenderToPNGFile(prof *printer.DeviceProfile, payloadJSON string, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("crear archivo PNG %q: %w", outputPath, err)
	}
	defer f.Close()
	return RenderToPNG(prof, payloadJSON, f)
}

// RenderPayloadToImage es alias de RenderToImage.
func RenderPayloadToImage(prof *printer.DeviceProfile, payloadJSON string) (image.Image, error) {
	return RenderToImage(prof, payloadJSON)
}

// RenderPayloadToPNG es alias de RenderToPNG.
func RenderPayloadToPNG(prof *printer.DeviceProfile, payloadJSON string, w io.Writer) error {
	return RenderToPNG(prof, payloadJSON, w)
}

// RenderPayloadToPNGFile es alias de RenderToPNGFile.
func RenderPayloadToPNGFile(prof *printer.DeviceProfile, payloadJSON string, outputPath string) error {
	return RenderToPNGFile(prof, payloadJSON, outputPath)
}

// RenderThermal es alias de RenderToESC para compatibilidad con firmas previas.
func RenderThermal(prof *printer.DeviceProfile, payloadJSON string) ([]byte, error) {
	return RenderToESC(prof, payloadJSON)
}

// RenderToESC convierte el payload JSON en bytes ESC/POS crudos (raster GS v 0)
// para enviar directamente al spooler de la impresora térmica física.
func RenderToESC(prof *printer.DeviceProfile, payloadJSON string) ([]byte, error) {
	var p PrintPayload
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return nil, fmt.Errorf("deserializar payload JSON: %w", err)
	}
	if len(p.Body) == 0 {
		return nil, fmt.Errorf("payload sin bloques en body")
	}
	p.applyLegacyMargins()

	ctx := resolveRenderContext(prof, p.PaperProperties)
	activeProf := ctx.profile

	conn := &memoryConnector{}
	proto := composer.NewEscpos()
	posterProfile := &profile.Escpos{
		Model:            "usqay-thermal",
		PaperWidth:       p.PaperProperties.Width,
		DPI:              activeProf.DPI,
		DotsPerLine:      activeProf.WidthDots,
		SupportsGraphics: activeProf.SupportsRaster,
		SupportsBarcode:  true,
		HasQR:            activeProf.SupportsQRNative,
		SupportsCutter:   activeProf.SupportsCut,
		SupportsDrawer:   activeProf.SupportsDrawer,
		CodeTable:        character.PC850,
	}

	svc, err := service.NewPrinter(proto, posterProfile, conn)
	if err != nil {
		return nil, fmt.Errorf("crear printer poster: %w", err)
	}
	if err := svc.Initialize(); err != nil {
		return nil, fmt.Errorf("inicializar impresora: %w", err)
	}

	if activeProf.SupportsPrintArea {
		if err := svc.Write(proto.LeftMargin(uint16(ctx.leftMarginDots))); err != nil {
			return nil, fmt.Errorf("fijar margen izquierdo: %w", err)
		}
		if err := svc.Write(proto.PrintWidth(uint16(ctx.printWidthDots))); err != nil {
			return nil, fmt.Errorf("fijar ancho de impresión: %w", err)
		}
	}

	// Apertura de gaveta
	if p.Options.Drawer && activeProf.SupportsDrawer {
		drawerPulse := []byte{0x1B, 0x70, 0x00, 0x19, 0xFA}
		if err := svc.Write(drawerPulse); err != nil {
			return nil, fmt.Errorf("abrir cajón: %w", err)
		}
	}

	// Renderizar la imagen continua del ticket
	img, err := RenderToImage(prof, payloadJSON)
	if err != nil {
		return nil, fmt.Errorf("renderizar ticket a imagen: %w", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() > 0 && bounds.Dy() > 0 {
		pipeline := graphics.NewPipeline(&graphics.ImgOptions{
			PixelWidth:     bounds.Dx(),
			Threshold:      128,
			Dithering:      graphics.Threshold,
			Scaling:        graphics.BiLinear,
			PreserveAspect: true,
		})
		bmp, err := pipeline.Process(img)
		if err != nil {
			return nil, fmt.Errorf("binarizar imagen: %w", err)
		}
		if err := writeRasterChunked(svc, proto, bmp); err != nil {
			return nil, fmt.Errorf("escribir tiras raster: %w", err)
		}
	}

	// Corte de papel o avance final
	if p.Options.Cut && activeProf.SupportsCut {
		if err := svc.PartialFeedAndCut(3); err != nil {
			return nil, fmt.Errorf("cortar papel: %w", err)
		}
	} else if err := svc.FeedLines(3); err != nil {
		return nil, fmt.Errorf("avanzar papel: %w", err)
	}

	return conn.buf.Bytes(), nil
}

// ============================================================================
// 4. DESPACHADOR CENTRAL DE BLOQUES (Body Router)
// ============================================================================

func renderBlock(eng *ticketimage.Engine, blockType string, raw json.RawMessage, ctx renderContext) error {
	switch blockType {
	case "text":
		var block textBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque text inválido", "err", err)
			return nil
		}
		renderTextBlock(eng, block)
		return nil

	case "separator":
		var block separatorBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque separator inválido", "err", err)
			return nil
		}
		renderSeparatorBlock(eng, block, ctx.widthChars)
		return nil

	case "spacer":
		var block spacerBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque spacer inválido", "err", err)
			return nil
		}
		renderSpacerBlock(eng, block)
		return nil

	case "columns":
		var block columnsBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque columns inválido", "err", err)
			return nil
		}
		renderColumnsBlock(eng, block, ctx.widthChars)
		return nil

	case "table":
		var block tableBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque table inválido", "err", err)
			return nil
		}
		renderTableBlock(eng, block, ctx)
		return nil

	case "image":
		var block imageBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque image inválido", "err", err)
			return nil
		}
		return renderImageBlock(eng, block, ctx)

	case "qr":
		var block qrBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque qr inválido", "err", err)
			return nil
		}
		return renderQRBlock(eng, block, ctx.printWidthDots)

	case "barcode":
		var block barcodeBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque barcode inválido", "err", err)
			return nil
		}
		return renderBarcodeBlock(eng, block, ctx)

	default:
		slog.Warn("tipo de bloque desconocido, saltando", "type", blockType)
		return nil
	}
}

// ============================================================================
// 5. RENDERIZADORES DE CADA BLOQUE PRIMITIVO
// ============================================================================

// 5.1. TEXTO
func renderTextBlock(eng *ticketimage.Engine, block textBlock) {
	eng.SetAlign(orDefault(block.Align, "left"))
	if block.Bold {
		eng.SetBold(true)
	}
	w, h := sizeMultipliers(block.Size)
	if h > 1 {
		eng.Feed(h - 1)
	}
	eng.SetSize(w, h)
	eng.PrintWrapped(block.Value)
	eng.SetSize(1, 1)
	if block.Bold {
		eng.SetBold(false)
	}
}

// 5.2. SEPARADOR
func renderSeparatorBlock(eng *ticketimage.Engine, block separatorBlock, widthChars int) {
	char := orDefault(block.Character, "-")
	eng.AlignLeft()
	eng.Separator(char, widthChars)
}

// 5.3. ESPACIADOR
func renderSpacerBlock(eng *ticketimage.Engine, block spacerBlock) {
	lines := block.Lines
	if lines <= 0 {
		lines = 1
	}
	eng.Feed(lines)
}

// 5.4. COLUMNAS
func renderColumnsBlock(eng *ticketimage.Engine, block columnsBlock, widthChars int) {
	colWidths := calculateColumnWidths(block.Columns, widthChars)
	eng.AlignLeft()

	maxScaleH := 1
	for _, col := range block.Columns {
		_, h := sizeMultipliers(col.Size)
		if h > maxScaleH {
			maxScaleH = h
		}
	}
	if maxScaleH > 1 {
		eng.Feed(maxScaleH - 1)
	}

	x := 0.0
	for i, col := range block.Columns {
		align := orDefault(col.Align, "left")
		eng.SetBold(col.Bold)

		w, h := sizeMultipliers(col.Size)
		eng.SetSize(w, h)
		if col.Size != "" && col.Size != "normal" {
			x = eng.PrintSegmentAt(col.Text, x)
		} else {
			x = eng.PrintSegmentAt(formatColumnText(col.Text, colWidths[i], align), x)
		}
		eng.SetSize(1, 1)
	}
	eng.SetBold(false)

	if maxScaleH > 1 {
		eng.SetSize(1, maxScaleH)
		eng.NewLine()
		eng.SetSize(1, 1)
	} else {
		eng.NewLine()
	}
}

// 5.5. TABLA
func renderTableBlock(eng *ticketimage.Engine, block tableBlock, ctx renderContext) {
	widthChars := ctx.widthChars
	colWidths, colAligns := calculateTableWidths(block, widthChars)
	pxPerChar := float64(ctx.printWidthDots) / float64(widthChars)

	// Encabezados
	if hasHeaders(block.Columns) {
		headerRow := tableRow{Cells: make([]tableCell, len(block.Columns))}
		for i, col := range block.Columns {
			if col.Header != nil {
				headerRow.Cells[i] = tableCell{Text: *col.Header}
			}
		}
		cells, maxLines := wrapRowCells(headerRow, colWidths, colAligns)
		eng.AlignLeft()
		eng.SetBold(true)
		for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
			eng.PrintLine(formatRowLine(cells, colWidths, lineIdx))
		}
		eng.SetBold(false)
		eng.PrintLine(strings.Repeat("-", widthChars))
	}

	// Filas
	for _, row := range block.Rows {
		if row.Merge {
			renderMergedRow(eng, row, widthChars)
			continue
		}
		renderStandardRow(eng, row, colWidths, colAligns, pxPerChar)
	}
}

func renderMergedRow(eng *ticketimage.Engine, row tableRow, totalWidth int) {
	if len(row.Cells) == 0 {
		return
	}
	cell := row.Cells[0]
	align := orDefault(cell.Align, "left")
	effectiveBold := row.Bold || (cell.Bold != nil && *cell.Bold)
	cellSize := orDefault(cell.Size, "normal")

	eng.AlignLeft()
	if effectiveBold {
		eng.SetBold(true)
	}
	w, h := sizeMultipliers(cellSize)
	if h > 1 {
		eng.Feed(h - 1)
	}
	eng.SetSize(w, h)
	for _, line := range wrapTextLines(cell.Text, cellWrapWidth(totalWidth, cellSize)) {
		eng.PrintLine(formatColumnText(line, totalWidth, align))
	}
	eng.SetSize(1, 1)
	if effectiveBold {
		eng.SetBold(false)
	}
}

func renderStandardRow(eng *ticketimage.Engine, row tableRow, colWidths []int, colAligns []string, pxPerChar float64) {
	needsPerCell := false
	for _, cell := range row.Cells {
		if cell.Bold != nil || (cell.Size != "" && cell.Size != "normal") {
			needsPerCell = true
			break
		}
	}

	eng.AlignLeft()
	cells, maxLines := wrapRowCells(row, colWidths, colAligns)

	if !needsPerCell {
		if row.Bold {
			eng.SetBold(true)
		}
		for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
			eng.PrintLine(formatRowLine(cells, colWidths, lineIdx))
		}
		if row.Bold {
			eng.SetBold(false)
		}
		return
	}

	maxScaleH := 1
	for _, c := range cells {
		_, h := sizeMultipliers(c.size)
		if h > maxScaleH {
			maxScaleH = h
		}
	}

	// Avanzar cursor para evitar solapamiento hacia arriba de glifos grandes
	if maxScaleH > 1 {
		eng.Feed(maxScaleH - 1)
	}

	for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
		x := 0.0
		for i, c := range cells {
			line := ""
			if lineIdx < len(c.lines) {
				line = c.lines[lineIdx]
			}
			colEndX := x + float64(colWidths[i])*pxPerChar

			if line != "" {
				eng.SetBold(c.bold)
				w, h := sizeMultipliers(c.size)
				eng.SetSize(w, h)

				if c.size != "normal" {
					eng.PrintSegmentAt(line, x)
				} else {
					eng.PrintSegmentAt(formatColumnText(line, colWidths[i], c.align), x)
				}
				eng.SetSize(1, 1)
			}
			x = colEndX
		}
		eng.SetBold(false)
		if maxScaleH > 1 {
			eng.SetSize(1, maxScaleH)
			eng.NewLine()
			eng.SetSize(1, 1)
		} else {
			eng.NewLine()
		}
	}
}

// 5.6. IMAGEN
func renderImageBlock(eng *ticketimage.Engine, block imageBlock, ctx renderContext) error {
	clean := strings.ReplaceAll(strings.ReplaceAll(block.Data, "\n", ""), "\r", "")
	clean = strings.TrimSpace(clean)

	data, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return fmt.Errorf("decodificar base64: %w", err)
	}

	srcImg, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decodificar formato de imagen: %w", err)
	}

	targetW := block.Width
	if targetW <= 0 || targetW > ctx.printWidthDots {
		targetW = ctx.printWidthDots
	}

	align := orDefault(block.Align, "center")
	return eng.PrintImageAligned(srcImg, targetW, align)
}

// 5.7. CÓDIGO QR
func renderQRBlock(eng *ticketimage.Engine, block qrBlock, printWidthDots int) error {
	opts := graphics.DefaultQROptions()
	size := block.Size
	if size <= 0 && block.PixelWidth > 0 {
		size = block.PixelWidth
	}
	if size > 0 {
		opts.PixelWidth = size
	}
	if opts.PixelWidth > printWidthDots {
		opts.PixelWidth = printWidthDots
	}
	qrContent := block.Value
	if qrContent == "" {
		qrContent = block.Data
	}
	img, err := graphics.ProcessQRImage(qrContent, opts)
	if err != nil {
		return fmt.Errorf("generar QR: %w", err)
	}

	return eng.PrintImageAligned(img, opts.PixelWidth, orDefault(block.Align, "center"))
}

// 5.8. CÓDIGO DE BARRAS (Code-128 integrado como imagen)
func renderBarcodeBlock(eng *ticketimage.Engine, block barcodeBlock, ctx renderContext) error {
	modWidth := block.Width
	if modWidth <= 0 {
		modWidth = 2
	}
	barHeight := block.Height
	if barHeight <= 0 {
		barHeight = 60
	}

	barcodeImg, err := generateCode128Image(block.Value, modWidth, barHeight)
	if err != nil {
		return fmt.Errorf("generar barcode: %w", err)
	}

	if barcodeImg.Bounds().Dx() > ctx.printWidthDots && modWidth > 1 {
		barcodeImg, err = generateCode128Image(block.Value, 1, barHeight)
		if err != nil {
			return fmt.Errorf("generar barcode comprimido: %w", err)
		}
	}

	align := orDefault(block.Align, "center")
	hri := orDefault(block.HRI, "below")

	if hri == "above" {
		eng.SetAlign(align)
		eng.PrintLine(block.Value)
	}
	if err := eng.PrintImageAligned(barcodeImg, barcodeImg.Bounds().Dx(), align); err != nil {
		return fmt.Errorf("dibujar barcode: %w", err)
	}
	if hri == "below" {
		eng.SetAlign(align)
		eng.PrintLine(block.Value)
	}
	return nil
}

// ============================================================================
// 6. HELPERS INTERNOS DE LAYOUT Y HARDWARE
// ============================================================================

func orDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

func sizeMultipliers(size string) (int, int) {
	switch size {
	case "double":
		return 2, 2
	case "medium":
		return 1, 2
	default:
		return 1, 1
	}
}

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

func formatColumnText(text string, width int, align string) string {
	runes := []rune(text)
	if len(runes) >= width {
		return string(runes[:width])
	}
	pad := width - len(runes)
	switch align {
	case "right":
		return strings.Repeat(" ", pad) + text
	case "center":
		l := pad / 2
		r := pad - l
		return strings.Repeat(" ", l) + text + strings.Repeat(" ", r)
	default:
		return text + strings.Repeat(" ", pad)
	}
}

func calculateColumnWidths(cols []columnsCell, totalWidth int) []int {
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

func calculateTableWidths(block tableBlock, totalWidth int) ([]int, []string) {
	if len(block.Columns) == 0 {
		n := 1
		if len(block.Rows) > 0 && len(block.Rows[0].Cells) > 0 {
			n = len(block.Rows[0].Cells)
		}
		w := totalWidth / n
		rem := totalWidth - w*n
		colW := make([]int, n)
		colA := make([]string, n)
		for i := 0; i < n; i++ {
			width := w
			if i == n-1 {
				width += rem
			}
			colW[i] = width
			colA[i] = "left"
		}
		return colW, colA
	}

	colW := make([]int, len(block.Columns))
	colA := make([]string, len(block.Columns))
	sum := 0
	for i, col := range block.Columns {
		colA[i] = orDefault(col.Align, "left")
		w := int(col.Width * float64(totalWidth))
		if w < 1 {
			w = 1
		}
		colW[i] = w
		sum += w
	}
	if sum != totalWidth && len(colW) > 0 {
		colW[len(colW)-1] += totalWidth - sum
	}
	return colW, colA
}

func hasHeaders(cols []tableColumn) bool {
	for _, col := range cols {
		if col.Header != nil && *col.Header != "" {
			return true
		}
	}
	return false
}

type wrappedCellData struct {
	lines []string
	align string
	bold  bool
	size  string
}

func wrapRowCells(row tableRow, colWidths []int, colAligns []string) ([]wrappedCellData, int) {
	cells := make([]wrappedCellData, len(colWidths))
	maxLines := 1
	for i := range colWidths {
		var cell tableCell
		if i < len(row.Cells) {
			cell = row.Cells[i]
		}
		bold := row.Bold
		if cell.Bold != nil {
			bold = *cell.Bold
		}
		size := orDefault(cell.Size, "normal")
		align := cell.Align
		if align == "" && i < len(colAligns) {
			align = colAligns[i]
		}
		if align == "" {
			align = "left"
		}

		lines := wrapTextLines(cell.Text, cellWrapWidth(colWidths[i], size))
		cells[i] = wrappedCellData{lines: lines, align: align, bold: bold, size: size}
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	return cells, maxLines
}

func wrapTextLines(text string, width int) []string {
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

func formatRowLine(cells []wrappedCellData, colWidths []int, lineIdx int) string {
	var sb strings.Builder
	for i, c := range cells {
		line := ""
		if lineIdx < len(c.lines) {
			line = c.lines[lineIdx]
		}
		sb.WriteString(formatColumnText(line, colWidths[i], c.align))
	}
	return sb.String()
}

type memoryConnector struct {
	buf bytes.Buffer
}

func (m *memoryConnector) Write(p []byte) (int, error) { return m.buf.Write(p) }
func (m *memoryConnector) Close() error                { return nil }

func writeRasterChunked(svc *service.Printer, proto *composer.EscposProtocol, bmp *graphics.MonochromeBitmap) error {
	rowBytes := bmp.GetWidthBytes()
	raster := bmp.GetRasterData()
	const maxRows = 128

	for y := 0; y < bmp.Height; y += maxRows {
		chunkH := maxRows
		if y+chunkH > bmp.Height {
			chunkH = bmp.Height - y
		}
		start := y * rowBytes
		end := start + chunkH*rowBytes

		cmd, err := proto.BitImage.PrintRasterBitImage(bitimage.Normal, uint16(rowBytes), uint16(chunkH), raster[start:end])
		if err != nil {
			return fmt.Errorf("componer raster: %w", err)
		}
		if err := svc.Write(cmd); err != nil {
			return fmt.Errorf("escribir raster: %w", err)
		}
	}
	return nil
}

// Generador Code-128
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

func generateCode128Image(text string, moduleWidth, height int) (image.Image, error) {
	if text == "" {
		return nil, fmt.Errorf("texto vacío")
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

	totalWidth := totalModules * moduleWidth
	img := image.NewRGBA(image.Rect(0, 0, totalWidth, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	x := 0
	for _, sym := range symbols {
		isBar := true
		for _, ch := range code128Patterns[sym] {
			w := int(ch-'0') * moduleWidth
			if isBar {
				for bx := 0; bx < w; bx++ {
					for by := 0; by < height; by++ {
						img.Set(x+bx, by, color.Black)
					}
				}
			}
			x += w
			isBar = !isBar
		}
	}
	return img, nil
}
