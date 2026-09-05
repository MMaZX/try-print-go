package queue

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"math"
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

// drawerPulse es el pulso estándar ESC/POS de apertura de cajón (ESC p 0 25
// 250). poster no expone este comando en su EscposProtocol; es el único byte
// crudo que este renderer emite a mano, vía el escape hatch de bytes crudos
// que la propia librería documenta.
var drawerPulse = []byte{0x1B, 0x70, 0x00, 0x19, 0xFA}

// maxRasterChunkRows limita cada comando GS v 0 a 128 líneas de alto,
// bien por debajo del máximo de 2303 dots que permite el comando
// (PrintRasterBitImage), para compatibilidad amplia de buffer de firmware.
const maxRasterChunkRows = 128

// memoryConnector implementa connection.Connector de poster acumulando bytes
// en memoria. El transporte real hacia el hardware lo resuelve printer.Printer
// (spooler de Windows o `lp -o raw` en Linux) después de que RenderThermal
// devuelve los bytes — poster solo construye el protocolo, no lo transmite.
type memoryConnector struct {
	buf bytes.Buffer
}

func (m *memoryConnector) Write(p []byte) (int, error) { return m.buf.Write(p) }

func (m *memoryConnector) Close() error { return nil }

// thermalCtx agrupa la geometría ya resuelta del ticket para no repetir su
// cálculo en cada función de dibujo de bloque.
type thermalCtx struct {
	widthChars     int
	printWidthDots int
	dpi            int
	prof           printer.DeviceProfile
}

// RenderThermal convierte un PrintPayload JSON en bytes ESC/POS para
// impresoras térmicas. El texto, tablas, columnas y separadores se dibujan
// como imagen con una fuente TrueType propia (internal/ticketimage, fuente
// Cascadia Code) en vez de comandos de texto ESC/POS — así el ticket no
// depende de la fuente de firmware ni de su code page. QR e imágenes se
// componen en el mismo lienzo. Solo el código de barras (que poster no sabe
// generar como imagen) y los comandos de máquina (margen, corte, cajón) usan
// el protocolo ESC/POS nativo de poster.
func RenderThermal(prof *printer.DeviceProfile, payloadJSON string) ([]byte, error) {
	var p PrintPayload
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return nil, fmt.Errorf("parsear payload JSON: %w", err)
	}
	if len(p.Body) == 0 {
		return nil, fmt.Errorf("payload sin bloques en body")
	}

	activeProf := resolveActiveProfile(prof, p.Margins)
	dots := activeProf.WidthDots
	charWidth := activeProf.CharWidthDots
	if charWidth <= 0 {
		charWidth = 12
	}

	anchoPapelMM := p.Margins.DimensionPapel
	if anchoPapelMM <= 0 {
		anchoPapelMM = p.Margins.AnchoDimension
	}
	totalPaperDots := dots
	if payloadDots, _ := paperGeometry(anchoPapelMM); payloadDots > 0 {
		totalPaperDots = payloadDots
	}

	leftMarginDots := 0
	printWidthDots := dots
	if totalPaperDots > dots {
		leftMarginDots = (totalPaperDots - dots) / 2
	}
	if p.Margins.Padding > 0 {
		paddingDots := int(math.Round(p.Margins.Padding * 8.0))
		leftMarginDots += paddingDots
		printWidthDots -= 2 * paddingDots
		if printWidthDots < 96 {
			printWidthDots = 96
		}
	}
	ctx := thermalCtx{
		widthChars:     printWidthDots / charWidth,
		printWidthDots: printWidthDots,
		dpi:            activeProf.DPI,
		prof:           activeProf,
	}

	conn := &memoryConnector{}
	proto := composer.NewEscpos()
	posterProfile := toPosterProfile(activeProf, anchoPapelMM)

	svc, err := service.NewPrinter(proto, posterProfile, conn)
	if err != nil {
		return nil, fmt.Errorf("crear printer poster: %w", err)
	}
	if err := svc.Initialize(); err != nil {
		return nil, fmt.Errorf("inicializar impresora: %w", err)
	}

	if activeProf.SupportsPrintArea {
		if err := svc.Write(proto.LeftMargin(uint16(leftMarginDots))); err != nil {
			return nil, fmt.Errorf("fijar margen izquierdo: %w", err)
		}
		if err := svc.Write(proto.PrintWidth(uint16(printWidthDots))); err != nil {
			return nil, fmt.Errorf("fijar ancho de área de impresión: %w", err)
		}
	}

	if p.Options.Drawer && activeProf.SupportsDrawer {
		if err := svc.Write(drawerPulse); err != nil {
			return nil, fmt.Errorf("abrir cajón: %w", err)
		}
	}

	eng, err := newTicketEngine(ctx)
	if err != nil {
		return nil, fmt.Errorf("crear motor de render: %w", err)
	}

	for _, raw := range p.Body {
		var env blockEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			slog.Warn("skip bloque con JSON inválido", "err", err)
			continue
		}

		if env.Type == "barcode" {
			// El código de barras no se puede componer en el lienzo (poster
			// no lo genera como imagen, solo como comando nativo) — se corta
			// el segmento de imagen acumulado hasta acá, se emite el
			// comando nativo, y se abre un lienzo nuevo para lo que sigue.
			if err := flushTicketImage(svc, proto, eng); err != nil {
				return nil, fmt.Errorf("volcar segmento de imagen: %w", err)
			}
			var block BarcodeBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				slog.Warn("skip bloque barcode inválido", "err", err)
			} else if err := emitBarcodeNative(svc, block); err != nil {
				return nil, fmt.Errorf("renderizar bloque \"barcode\": %w", err)
			}
			eng, err = newTicketEngine(ctx)
			if err != nil {
				return nil, fmt.Errorf("crear motor de render: %w", err)
			}
			continue
		}

		if err := drawThermalBlock(eng, env.Type, raw, ctx); err != nil {
			return nil, fmt.Errorf("renderizar bloque %q: %w", env.Type, err)
		}
	}

	if err := flushTicketImage(svc, proto, eng); err != nil {
		return nil, fmt.Errorf("volcar segmento de imagen: %w", err)
	}

	if p.Options.Cut && activeProf.SupportsCut {
		if err := svc.PartialFeedAndCut(3); err != nil {
			return nil, fmt.Errorf("cortar papel: %w", err)
		}
	} else if err := svc.FeedLines(3); err != nil {
		return nil, fmt.Errorf("avanzar papel: %w", err)
	}

	return conn.buf.Bytes(), nil
}

// toPosterProfile mapea nuestro printer.DeviceProfile (config declarada por
// impresora, ver docs/print_payload_schema.md) al profile.Escpos que poster
// necesita para las capacidades del comando nativo de barcode y de máquina
// (corte, cajón). El dimensionamiento en dots/píxeles lo seguimos calculando
// nosotros (paperGeometry, arriba) y se lo pasamos explícito a cada comando.
func toPosterProfile(prof printer.DeviceProfile, anchoPapelMM float64) *profile.Escpos {
	if anchoPapelMM <= 0 {
		anchoPapelMM = anchoRef1MM
	}
	return &profile.Escpos{
		Model:            "usqay-thermal",
		PaperWidth:       anchoPapelMM,
		DPI:              prof.DPI,
		DotsPerLine:      prof.WidthDots,
		SupportsGraphics: prof.SupportsRaster,
		SupportsBarcode:  true,
		HasQR:            prof.SupportsQRNative,
		SupportsCutter:   prof.SupportsCut,
		SupportsDrawer:   prof.SupportsDrawer,
		CodeTable:        character.PC850,
	}
}

// newTicketEngine crea un lienzo nuevo del ancho de impresión ya resuelto.
func newTicketEngine(ctx thermalCtx) (*ticketimage.Engine, error) {
	dpi := ctx.dpi
	if dpi <= 0 {
		dpi = 203
	}
	return ticketimage.NewEngine(ticketimage.Config{
		PaperPxWidth:            ctx.printWidthDots,
		DPI:                     dpi,
		AutoAdjustCursorOnScale: true,
	})
}

// flushTicketImage binariza el lienzo acumulado en eng y lo emite como uno o
// más comandos GS v 0. Un lienzo vacío (p. ej. un ticket que arranca con un
// bloque barcode) no emite nada.
func flushTicketImage(svc *service.Printer, proto *composer.EscposProtocol, eng *ticketimage.Engine) error {
	img := eng.Render()
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return nil
	}

	pipeline := graphics.NewPipeline(&graphics.ImgOptions{
		PixelWidth:     bounds.Dx(),
		Threshold:      128,
		Dithering:      graphics.Threshold,
		Scaling:        graphics.BiLinear,
		PreserveAspect: true,
	})
	bitmap, err := pipeline.Process(img)
	if err != nil {
		return fmt.Errorf("binarizar ticket: %w", err)
	}
	return writeRasterChunked(svc, proto, bitmap)
}

// writeRasterChunked trocea el bitmap en tiras de máximo
// maxRasterChunkRows líneas de alto y emite un comando GS v 0 por tira.
func writeRasterChunked(svc *service.Printer, proto *composer.EscposProtocol, bmp *graphics.MonochromeBitmap) error {
	rowBytes := bmp.GetWidthBytes()
	raster := bmp.GetRasterData()

	for y := 0; y < bmp.Height; y += maxRasterChunkRows {
		chunkH := maxRasterChunkRows
		if y+chunkH > bmp.Height {
			chunkH = bmp.Height - y
		}
		start := y * rowBytes
		end := start + chunkH*rowBytes

		cmd, err := proto.BitImage.PrintRasterBitImage(bitimage.Normal, uint16(rowBytes), uint16(chunkH), raster[start:end])
		if err != nil {
			return fmt.Errorf("componer tira raster: %w", err)
		}
		if err := svc.Write(cmd); err != nil {
			return fmt.Errorf("escribir tira raster: %w", err)
		}
	}
	return nil
}

// sizeMultipliers traduce nuestro campo de schema "size" (normal/medium/double,
// ver docs/print_payload_schema.md §3.1) a los multiplicadores ancho×alto que
// espera Engine.SetSize: normal=1x1, medium=1x2 (alto doble, ancho normal),
// double=2x2 (alto y ancho dobles).
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

// drawThermalBlock despacha un bloque del body según su "type" hacia la
// función de dibujo correspondiente sobre el lienzo eng. "barcode" no pasa
// por acá — lo maneja RenderThermal aparte porque no se dibuja en el lienzo.
func drawThermalBlock(eng *ticketimage.Engine, blockType string, raw json.RawMessage, ctx thermalCtx) error {
	switch blockType {
	case "text":
		var block TextBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque text inválido", "err", err)
			return nil
		}
		drawText(eng, block)
		return nil

	case "separator":
		var block SeparatorBlock
		json.Unmarshal(raw, &block) //nolint:errcheck
		char := orDefault(block.Character, "-")
		eng.AlignLeft()
		eng.Separator(char, ctx.widthChars)
		return nil

	case "spacer":
		var block SpacerBlock
		json.Unmarshal(raw, &block) //nolint:errcheck
		eng.Feed(positiveOrDefault(block.Lines, 1))
		return nil

	case "table":
		var block TableBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque table inválido", "err", err)
			return nil
		}
		drawTable(eng, block, ctx.widthChars)
		return nil

	case "columns":
		var block ColumnsBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque columns inválido", "err", err)
			return nil
		}
		drawColumns(eng, block, ctx.widthChars)
		return nil

	case "qr":
		var block QRBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque qr inválido", "err", err)
			return nil
		}
		return drawQR(eng, block, ctx.printWidthDots)

	case "image":
		var block ImageBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			slog.Warn("skip bloque image inválido", "err", err)
			return nil
		}
		return drawImage(eng, block, ctx)

	default:
		slog.Warn("tipo de bloque desconocido, saltando", "type", blockType)
		return nil
	}
}

func drawText(eng *ticketimage.Engine, block TextBlock) {
	eng.SetAlign(orDefault(block.Align, "left"))
	if block.Bold {
		eng.SetBold(true)
	}
	w, h := sizeMultipliers(block.Size)
	eng.SetSize(w, h)
	// PrintWrapped en vez de PrintLine: con fuentes grandes (size
	// "medium"/"double") el texto puede no entrar en una sola línea del
	// ancho de papel — antes se cortaba en el borde, ahora salta de línea
	// sin afectar el resto del ticket (no toca tablas/columnas).
	eng.PrintWrapped(block.Value)
	eng.SetSize(1, 1)
	if block.Bold {
		eng.SetBold(false)
	}
}

func drawTable(eng *ticketimage.Engine, block TableBlock, widthChars int) {
	colWidths, colAligns := resolveTableLayout(block, widthChars)

	if hasTableHeaders(block.Columns) {
		headers := make([]string, len(block.Columns))
		for i, col := range block.Columns {
			if col.Header != nil {
				headers[i] = *col.Header
			}
		}
		eng.AlignLeft()
		eng.SetBold(true)
		eng.PrintLine(formatColumns(headers, colWidths, colAligns))
		eng.SetBold(false)
		eng.PrintLine(strings.Repeat("-", widthChars))
	}

	for _, row := range block.Rows {
		if row.Merge {
			drawMergeRow(eng, row, widthChars)
			continue
		}
		drawTableRow(eng, row, colWidths, colAligns)
	}
}

func drawMergeRow(eng *ticketimage.Engine, row TableRow, totalWidth int) {
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
	eng.SetSize(w, h)
	eng.PrintLine(formatCol(cell.Text, totalWidth, align))
	eng.SetSize(1, 1)
	if effectiveBold {
		eng.SetBold(false)
	}
}

// drawTableRow imprime una fila normal. Cuando ninguna celda pide negrita o
// tamaño distinto de la fila, arma la línea completa como un solo string
// (una sola llamada a PrintLine, que sí resuelve alineación/ancho sola). Si
// hay mezcla de estilos por celda, va celda por celda con PrintSegmentAt
// concatenando manualmente en x — PrintLine/Print normales no acumulan
// sobre una posición anterior (recalculan su propio x según alineación),
// así que no sirven para eso.
func drawTableRow(eng *ticketimage.Engine, row TableRow, colWidths []int, colAligns []string) {
	needsPerCell := false
	for _, cell := range row.Cells {
		if cell.Bold != nil || (cell.Size != "" && cell.Size != "normal") {
			needsPerCell = true
			break
		}
	}

	eng.AlignLeft()

	if !needsPerCell {
		var sb strings.Builder
		for i, cell := range row.Cells {
			if i >= len(colWidths) {
				break
			}
			align := resolveAlign(cell.Align, colAlignAt(colAligns, i))
			sb.WriteString(formatCol(cell.Text, colWidths[i], align))
		}
		if row.Bold {
			eng.SetBold(true)
		}
		eng.PrintLine(sb.String())
		if row.Bold {
			eng.SetBold(false)
		}
		return
	}

	x := 0.0
	for i, cell := range row.Cells {
		if i >= len(colWidths) {
			break
		}
		align := resolveAlign(cell.Align, colAlignAt(colAligns, i))

		cellBold := row.Bold
		if cell.Bold != nil {
			cellBold = *cell.Bold
		}
		eng.SetBold(cellBold)

		cellSize := orDefault(cell.Size, "normal")
		w, h := sizeMultipliers(cellSize)
		eng.SetSize(w, h)

		// Sin padding en tamaños no estándar: el ancho de columna en
		// caracteres normales no aplica al doble/medio ancho de fuente.
		if cellSize != "normal" {
			x = eng.PrintSegmentAt(cell.Text, x)
		} else {
			x = eng.PrintSegmentAt(formatCol(cell.Text, colWidths[i], align), x)
		}
		eng.SetSize(1, 1)
	}
	eng.SetBold(false)
	eng.NewLine()
}

func drawColumns(eng *ticketimage.Engine, block ColumnsBlock, widthChars int) {
	colWidths := layoutColumnWidths(block.Columns, widthChars)
	eng.AlignLeft()

	x := 0.0
	for i, col := range block.Columns {
		align := orDefault(col.Align, "left")
		eng.SetBold(col.Bold)

		if col.Size != "" && col.Size != "normal" {
			w, h := sizeMultipliers(col.Size)
			eng.SetSize(w, h)
			x = eng.PrintSegmentAt(col.Text, x)
			eng.SetSize(1, 1)
		} else {
			x = eng.PrintSegmentAt(formatCol(col.Text, colWidths[i], align), x)
		}
	}
	eng.SetBold(false)
	eng.NewLine()
}

func drawQR(eng *ticketimage.Engine, block QRBlock, printWidthDots int) error {
	opts := graphics.DefaultQROptions()
	// "size" es el ancho final del QR en píxeles (ver
	// docs/print_payload_schema.md §3.5).
	if block.Size > 0 {
		opts.PixelWidth = block.Size
	}
	if opts.PixelWidth > printWidthDots {
		opts.PixelWidth = printWidthDots
	}
	opts.MaxPixelWidth = printWidthDots

	img, err := graphics.ProcessQRImage(block.Value, opts)
	if err != nil {
		return fmt.Errorf("generar imagen QR: %w", err)
	}
	if err := eng.PrintImageAligned(img, img.Bounds().Dx(), orDefault(block.Align, "center")); err != nil {
		return fmt.Errorf("dibujar QR: %w", err)
	}
	return nil
}

func drawImage(eng *ticketimage.Engine, block ImageBlock, ctx thermalCtx) error {
	if !ctx.prof.SupportsRaster {
		eng.SetAlign(orDefault(block.Align, "center"))
		eng.PrintLine("[IMAGEN]")
		return nil
	}
	if block.Data == "" {
		return nil
	}

	b64Data := block.Data
	if idx := strings.Index(b64Data, ";base64,"); idx != -1 {
		b64Data = b64Data[idx+8:]
	}
	b64Data = strings.TrimSpace(b64Data)

	imgBytes, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return fmt.Errorf("decodificar imagen base64: %w", err)
	}
	srcImg, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return fmt.Errorf("decodificar formato de imagen: %w", err)
	}

	targetW := block.Width
	if targetW <= 0 || targetW > ctx.printWidthDots {
		targetW = ctx.printWidthDots
	}

	if err := eng.PrintImageAligned(srcImg, targetW, orDefault(block.Align, "center")); err != nil {
		return fmt.Errorf("dibujar imagen: %w", err)
	}
	return nil
}

// emitBarcodeNative imprime un código de barras con el comando ESC/POS
// nativo de poster (GS k). No se puede componer como imagen en el lienzo:
// poster no trae un generador de barcode a imagen, solo el comando de
// protocolo — ver graphics/barcode.go en el módulo vendorizado.
func emitBarcodeNative(svc *service.Printer, block BarcodeBlock) error {
	cfg := graphics.DefaultBarcodeConfig()
	if sym, err := graphics.MapSymbology(block.Symbology); err == nil {
		cfg.Symbology = sym
	} else {
		slog.Warn("simbología de barcode desconocida, usando CODE128", "symbology", block.Symbology)
	}
	if block.Height > 0 {
		cfg.Height = graphics.IntToBarcodeHeight(block.Height)
	}
	if block.Width > 0 {
		cfg.Width = graphics.IntToBarcodeModuleWidth(block.Width)
	}
	if hri, err := graphics.MapHriPosition(orDefault(block.HRI, "below")); err == nil {
		cfg.HRIPosition = hri
	}

	if err := svc.SetAlignment(orDefault(block.Align, "center")); err != nil {
		return fmt.Errorf("alinear barcode: %w", err)
	}
	if err := svc.PrintBarcode(cfg, []byte(block.Value)); err != nil {
		return fmt.Errorf("imprimir barcode: %w", err)
	}
	return svc.AlignLeft()
}
