package htmlrender

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"unicode/utf8"

	"github.com/adcondev/poster/pkg/commands/character"
	"github.com/adcondev/poster/pkg/composer"
	"github.com/adcondev/poster/pkg/graphics"
	"github.com/adcondev/poster/pkg/profile"
	"github.com/adcondev/poster/pkg/service"

	"usqay-print-client/internal/printer"
)

// RenderThermalNative traduce el payload JSON directo a comandos ESC/POS,
// sin pasar por Chromium: texto, tablas, columnas, QR y códigos de barra usan
// los comandos nativos de la impresora en vez de rasterizar el ticket entero
// como una imagen. Pensado para hardware modesto (CPU débil, HDD) donde el
// render con navegador es el cuello de botella real — activado por terminal
// vía "render_mode": "lite" en config.json (ver internal/config).
//
// Solo el bloque "image" sigue yendo por bitmap monocromático: no hay forma
// nativa de que la impresora dibuje un logo arbitrario. El resto del ticket
// se imprime con el font ROM de la impresora (casi instantáneo, sin la
// latencia de layout/paint de un motor de render HTML).
func RenderThermalNative(prof *printer.DeviceProfile, payloadJSON string) ([]byte, error) {
	var payload PrintPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return nil, fmt.Errorf("deserializar payload JSON: %w", err)
	}
	if len(payload.Body) == 0 {
		return nil, fmt.Errorf("el payload no contiene bloques en body")
	}
	payload.applyLegacyMargins()

	var activeProf printer.DeviceProfile
	if prof != nil {
		activeProf = *prof
	} else if payload.PaperProperties.Width > 0 {
		activeProf = printer.DeriveProfileFromWidth(payload.PaperProperties.Width)
	} else {
		activeProf = printer.Default58mmProfile()
	}

	charWidth := activeProf.CharWidthDots
	if charWidth <= 0 {
		charWidth = 12
	}
	cols := activeProf.WidthDots / charWidth
	if cols <= 0 {
		cols = 32
	}

	conn := &memoryConnector{}
	proto := composer.NewEscpos()
	posterProfile := &profile.Escpos{
		Model:            "usqay-thermal-native",
		PaperWidth:       payload.PaperProperties.Width,
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

	if payload.Options.Drawer && activeProf.SupportsDrawer {
		drawerPulse := []byte{0x1B, 0x70, 0x00, 0x19, 0xFA}
		if err := svc.Write(drawerPulse); err != nil {
			return nil, fmt.Errorf("abrir cajón: %w", err)
		}
	}

	for _, raw := range payload.Body {
		var env blockEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			continue
		}

		var blockErr error
		switch env.Type {
		case "text":
			var b textBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				blockErr = emitTextNative(svc, cols, b)
			}
		case "separator":
			var b separatorBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				blockErr = emitSeparatorNative(svc, cols, b)
			}
		case "spacer":
			var b spacerBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				blockErr = emitSpacerNative(svc, b)
			}
		case "columns":
			var b columnsBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				blockErr = emitColumnsNative(svc, cols, b)
			}
		case "table":
			var b tableBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				blockErr = emitTableNative(svc, cols, b)
			}
		case "qr":
			var b qrBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				blockErr = emitQRNative(svc, b)
			}
		case "barcode":
			var b barcodeBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				blockErr = emitBarcodeNative(svc, b)
			}
		case "image":
			var b imageBlock
			if err := json.Unmarshal(raw, &b); err == nil {
				blockErr = emitImageNative(svc, activeProf, b)
			}
		}
		if blockErr != nil {
			return nil, fmt.Errorf("bloque %q: %w", env.Type, blockErr)
		}
	}

	if payload.Options.Cut && activeProf.SupportsCut {
		if err := svc.PartialFeedAndCut(3); err != nil {
			return nil, fmt.Errorf("cortar papel: %w", err)
		}
	} else if err := svc.FeedLines(3); err != nil {
		return nil, fmt.Errorf("avanzar papel: %w", err)
	}

	return conn.buf.Bytes(), nil
}

// escposSegment es un fragmento de texto con su propio estilo de negrita,
// usado para componer una línea con varias celdas (columns/table) que no
// comparten la misma alineación global de la impresora.
type escposSegment struct {
	text string
	bold bool
}

func writeSegmentsNative(svc *service.Printer, segments []escposSegment) error {
	for _, seg := range segments {
		if seg.bold {
			if err := svc.EnableBold(); err != nil {
				return err
			}
		}
		if err := safePrint(svc, seg.text); err != nil {
			return err
		}
		if seg.bold {
			if err := svc.DisableBold(); err != nil {
				return err
			}
		}
	}
	return svc.FeedLines(1)
}

// printLineNative imprime text seguido de salto de línea. Print.Text rechaza
// un string vacío ("can't print an empty buffer"), así que una línea vacía
// se resuelve como un simple avance de papel.
func printLineNative(svc *service.Printer, text string) error {
	if text == "" {
		return svc.FeedLines(1)
	}
	return safePrintLine(svc, text)
}

// asciiFallbacks mapea símbolos Unicode comunes en payloads reales (notas de
// comanda, viñetas, tipografía "smart") que no existen en los codepages
// ESC/POS típicos (PC850, etc.) a un equivalente ASCII legible.
var asciiFallbacks = map[rune]string{
	'↳': ">", '→': "->", '←': "<-", '↑': "^", '↓': "v",
	'•': "*", '·': "*", '◦': "*",
	'–': "-", '—': "-",
	'‘': "'", '’': "'", '“': "\"", '”': "\"",
	'…': "...",
	'✓': "OK", '✔': "OK", '✗': "X", '✘': "X",
}

// isEncodingError detecta el error que devuelve golang.org/x/text/encoding
// cuando el codepage configurado (PC850 por defecto) no tiene un glyph para
// alguna runa del texto — la librería poster no hace fallback automático.
func isEncodingError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "rune not supported")
}

// sanitizeForCodeTable reemplaza símbolos Unicode conocidos sin equivalente
// en el codepage por su versión ASCII, y cualquier otra runa no-ASCII
// desconocida por "?" — nunca deja pasar algo que vuelva a romper el encode.
func sanitizeForCodeTable(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 128:
			b.WriteRune(r)
		default:
			if repl, ok := asciiFallbacks[r]; ok {
				b.WriteString(repl)
			} else {
				b.WriteRune('?')
			}
		}
	}
	return b.String()
}

// safePrint/safePrintLine intentan imprimir el texto tal cual (los acentos
// latinos normales los soporta el codepage); si el codepage rechaza alguna
// runa, reintentan una vez con sanitizeForCodeTable en vez de perder el
// ticket entero por un solo carácter no soportado.
func safePrint(svc *service.Printer, text string) error {
	err := svc.Print(text)
	if isEncodingError(err) {
		return svc.Print(sanitizeForCodeTable(text))
	}
	return err
}

func safePrintLine(svc *service.Printer, text string) error {
	err := svc.PrintLine(text)
	if isEncodingError(err) {
		return svc.PrintLine(sanitizeForCodeTable(text))
	}
	return err
}

// applySizeNative fija el tamaño de fuente y devuelve el divisor de ancho
// efectivo (2 para "double", que también duplica el ancho de carácter).
func applySizeNative(svc *service.Printer, size string) (int, error) {
	switch sanitizeSize(size) {
	case "size-medium":
		return 1, svc.CustomSize(1, 2)
	case "size-double":
		return 2, svc.DoubleSize()
	default:
		return 1, svc.SingleSize()
	}
}

func emitTextNative(svc *service.Printer, cols int, b textBlock) error {
	if err := svc.SetAlignment(sanitizeAlign(b.Align)); err != nil {
		return err
	}
	divisor, err := applySizeNative(svc, b.Size)
	if err != nil {
		return err
	}
	width := cols / divisor
	if width <= 0 {
		width = 1
	}
	if b.Bold {
		if err := svc.EnableBold(); err != nil {
			return err
		}
	}
	for _, line := range wrapTextNative(b.Value, width) {
		if err := printLineNative(svc, line); err != nil {
			return err
		}
	}
	if b.Bold {
		if err := svc.DisableBold(); err != nil {
			return err
		}
	}
	if divisor != 1 {
		if err := svc.SingleSize(); err != nil {
			return err
		}
	}
	return svc.AlignLeft()
}

func emitSeparatorNative(svc *service.Printer, cols int, b separatorBlock) error {
	ch := strings.TrimSpace(b.Character)
	if ch == "" {
		ch = "-"
	}
	r := []rune(ch)
	if err := svc.AlignLeft(); err != nil {
		return err
	}
	return safePrintLine(svc, strings.Repeat(string(r[0]), cols))
}

func emitSpacerNative(svc *service.Printer, b spacerBlock) error {
	lines := b.Lines
	if lines <= 0 {
		lines = 1
	}
	if lines > 255 {
		lines = 255
	}
	return svc.FeedLines(byte(lines))
}

func emitColumnsNative(svc *service.Printer, cols int, b columnsBlock) error {
	if len(b.Columns) == 0 {
		return nil
	}
	weights := make([]float64, len(b.Columns))
	for i, c := range b.Columns {
		weights[i] = normalizeWidthPercent(c.Width) / 100.0
	}
	widths := distributeCols(cols, weights)

	segments := make([]escposSegment, 0, len(b.Columns))
	for i, c := range b.Columns {
		w := widths[i]
		if w <= 0 {
			continue
		}
		text := padAlignNative(truncateRunes(c.Text, w), w, sanitizeAlign(c.Align))
		segments = append(segments, escposSegment{text: text, bold: c.Bold})
	}
	return writeSegmentsNative(svc, segments)
}

func emitTableNative(svc *service.Printer, cols int, b tableBlock) error {
	if len(b.Columns) == 0 && len(b.Rows) == 0 {
		return nil
	}

	numCols := len(b.Columns)
	if numCols == 0 {
		for _, row := range b.Rows {
			if len(row.Cells) > numCols {
				numCols = len(row.Cells)
			}
		}
	}
	if numCols == 0 {
		return nil
	}

	weights := make([]float64, numCols)
	aligns := make([]string, numCols)
	for i := 0; i < numCols; i++ {
		if i < len(b.Columns) {
			weights[i] = normalizeWidthPercent(b.Columns[i].Width) / 100.0
			aligns[i] = sanitizeAlign(b.Columns[i].Align)
		} else {
			aligns[i] = "left"
		}
	}
	widths := distributeCols(cols, weights)

	hasHeaders := false
	for _, c := range b.Columns {
		if c.Header != nil && *c.Header != "" {
			hasHeaders = true
			break
		}
	}
	if hasHeaders {
		segments := make([]escposSegment, 0, numCols)
		for i := 0; i < numCols; i++ {
			header := ""
			if i < len(b.Columns) && b.Columns[i].Header != nil {
				header = *b.Columns[i].Header
			}
			text := padAlignNative(truncateRunes(header, widths[i]), widths[i], aligns[i])
			segments = append(segments, escposSegment{text: text, bold: true})
		}
		if err := writeSegmentsNative(svc, segments); err != nil {
			return err
		}
	}

	for _, row := range b.Rows {
		if row.Merge && len(row.Cells) > 0 {
			if err := emitMergedRowNative(svc, cols, row); err != nil {
				return err
			}
			continue
		}

		// El tamaño de fuente ("size") por celda no se soporta en modo lite
		// para filas normales: mezclar anchos de carácter distintos dentro de
		// la misma fila de columnas fijas requeriría recalcular el layout por
		// celda. Se soporta en filas "merge" (una sola celda, ancho completo).
		cellLines := make([][]string, numCols)
		maxLines := 1
		for i := 0; i < numCols; i++ {
			text := ""
			if i < len(row.Cells) {
				text = row.Cells[i].Text
			}
			lines := wrapTextNative(text, widths[i])
			cellLines[i] = lines
			if len(lines) > maxLines {
				maxLines = len(lines)
			}
		}
		for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
			segments := make([]escposSegment, 0, numCols)
			for i := 0; i < numCols; i++ {
				line := ""
				if lineIdx < len(cellLines[i]) {
					line = cellLines[i][lineIdx]
				}
				bold := row.Bold
				align := aligns[i]
				if i < len(row.Cells) {
					if row.Cells[i].Bold != nil {
						bold = *row.Cells[i].Bold
					}
					if row.Cells[i].Align != "" {
						align = sanitizeAlign(row.Cells[i].Align)
					}
				}
				text := padAlignNative(line, widths[i], align)
				segments = append(segments, escposSegment{text: text, bold: bold})
			}
			if err := writeSegmentsNative(svc, segments); err != nil {
				return err
			}
		}
	}
	return nil
}

func emitMergedRowNative(svc *service.Printer, cols int, row tableRow) error {
	cell := row.Cells[0]
	bold := row.Bold
	if cell.Bold != nil {
		bold = *cell.Bold
	}
	divisor, err := applySizeNative(svc, cell.Size)
	if err != nil {
		return err
	}
	width := cols / divisor
	if width <= 0 {
		width = 1
	}
	if err := svc.SetAlignment(sanitizeAlign(cell.Align)); err != nil {
		return err
	}
	if bold {
		if err := svc.EnableBold(); err != nil {
			return err
		}
	}
	for _, line := range wrapTextNative(cell.Text, width) {
		if err := printLineNative(svc, line); err != nil {
			return err
		}
	}
	if bold {
		if err := svc.DisableBold(); err != nil {
			return err
		}
	}
	if divisor != 1 {
		if err := svc.SingleSize(); err != nil {
			return err
		}
	}
	return svc.AlignLeft()
}

func emitQRNative(svc *service.Printer, b qrBlock) error {
	val := b.Value
	if val == "" {
		val = b.Data
	}
	if val == "" {
		return nil
	}

	opts := graphics.DefaultQROptions()
	if b.PixelWidth > 0 {
		opts.PixelWidth = b.PixelWidth
	} else if b.Size > 0 {
		opts.PixelWidth = b.Size * 30
	}
	if opts.PixelWidth < 96 {
		opts.PixelWidth = 120
	}

	if err := svc.SetAlignment(sanitizeAlign(b.Align)); err != nil {
		return err
	}
	if err := svc.PrintQR(val, opts); err != nil {
		return err
	}
	return svc.AlignLeft()
}

func emitBarcodeNative(svc *service.Printer, b barcodeBlock) error {
	if b.Value == "" {
		return nil
	}
	sym, err := graphics.MapSymbology(b.Symbology)
	if err != nil {
		return fmt.Errorf("simbología de código de barras: %w", err)
	}

	cfg := graphics.DefaultBarcodeConfig()
	cfg.Symbology = sym
	if b.Height > 0 {
		cfg.Height = graphics.IntToBarcodeHeight(b.Height)
	}
	if b.Width > 0 {
		cfg.Width = graphics.IntToBarcodeModuleWidth(b.Width)
	}
	hriStr := strings.ToLower(strings.TrimSpace(b.HRI))
	if hriStr == "" {
		hriStr = "below"
	}
	if hriPos, err := graphics.MapHriPosition(hriStr); err == nil {
		cfg.HRIPosition = hriPos
	}

	if err := svc.SetAlignment(sanitizeAlign(b.Align)); err != nil {
		return err
	}
	if err := svc.PrintBarcode(cfg, []byte(b.Value)); err != nil {
		return err
	}
	return svc.AlignLeft()
}

func emitImageNative(svc *service.Printer, prof printer.DeviceProfile, b imageBlock) error {
	data := strings.TrimSpace(b.Data)
	if data == "" {
		return nil
	}
	if strings.HasPrefix(data, "data:") {
		if idx := strings.Index(data, ","); idx != -1 {
			data = data[idx+1:]
		}
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("decodificar base64 de imagen: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decodificar imagen: %w", err)
	}

	if err := svc.SetAlignment(sanitizeAlign(b.Align)); err != nil {
		return err
	}

	pixelWidth := prof.WidthDots
	if b.Width > 0 && b.Width < pixelWidth {
		pixelWidth = b.Width
	}
	pipeline := graphics.NewPipeline(&graphics.ImgOptions{
		PixelWidth:     pixelWidth,
		Threshold:      128,
		Dithering:      graphics.Threshold,
		Scaling:        graphics.BiLinear,
		PreserveAspect: true,
	})
	bmp, err := pipeline.Process(img)
	if err != nil {
		return fmt.Errorf("binarizar imagen: %w", err)
	}
	if err := svc.PrintBitmap(bmp); err != nil {
		return fmt.Errorf("imprimir bitmap: %w", err)
	}
	return svc.AlignLeft()
}

// distributeCols reparte cols entre pesos proporcionales (normalizados a 1.0
// si vienen todos en 0), asignando el remanente de redondeo a la última
// columna para que la suma exacta siempre sea cols.
func distributeCols(cols int, weights []float64) []int {
	n := len(weights)
	widths := make([]int, n)
	if n == 0 {
		return widths
	}
	sum := 0.0
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if sum <= 0 {
		sum = float64(n)
		for i := range weights {
			weights[i] = 1
		}
	}
	used := 0
	for i, w := range weights {
		if w <= 0 {
			w = sum / float64(n)
		}
		widths[i] = int(float64(cols) * (w / sum))
		used += widths[i]
	}
	widths[n-1] += cols - used
	if widths[n-1] < 0 {
		widths[n-1] = 0
	}
	return widths
}

// wrapTextNative envuelve value en líneas de a lo sumo width runas, respetando
// saltos de línea explícitos y partiendo a la fuerza palabras más largas que
// una línea completa.
func wrapTextNative(value string, width int) []string {
	if width <= 0 {
		width = 32
	}
	var out []string
	for _, paragraph := range strings.Split(value, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		lineLen := 0
		for _, w := range words {
			wLen := utf8.RuneCountInString(w)
			if wLen > width {
				if lineLen > 0 {
					out = append(out, line.String())
					line.Reset()
					lineLen = 0
				}
				runes := []rune(w)
				for len(runes) > width {
					out = append(out, string(runes[:width]))
					runes = runes[width:]
				}
				line.WriteString(string(runes))
				lineLen = len(runes)
				continue
			}
			addLen := wLen
			if lineLen > 0 {
				addLen++
			}
			if lineLen+addLen > width {
				out = append(out, line.String())
				line.Reset()
				line.WriteString(w)
				lineLen = wLen
				continue
			}
			if lineLen > 0 {
				line.WriteString(" ")
			}
			line.WriteString(w)
			lineLen += addLen
		}
		out = append(out, line.String())
	}
	return out
}

// padAlignNative rellena s con espacios hasta width runas según align. Si s ya
// alcanza o supera width, se devuelve sin cambios (no trunca).
func padAlignNative(s string, width int, align string) string {
	l := utf8.RuneCountInString(s)
	if l >= width {
		return s
	}
	pad := width - l
	switch align {
	case "center":
		left := pad / 2
		right := pad - left
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
	case "right":
		return strings.Repeat(" ", pad) + s
	default:
		return s + strings.Repeat(" ", pad)
	}
}

// truncateRunes corta s a lo sumo a width runas.
func truncateRunes(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
}
