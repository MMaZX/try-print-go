// Package escpos constructs ESC/POS byte sequences for thermal printers.
package escpos

import (
	"strings"
)

// Standard ESC/POS command bytes.
var (
	cmdInit        = []byte{0x1B, 0x40}             // ESC @ — initialize printer
	cmdBoldOn      = []byte{0x1B, 0x45, 0x01}       // ESC E 1 — bold on
	cmdBoldOff     = []byte{0x1B, 0x45, 0x00}       // ESC E 0 — bold off
	cmdAlignLeft   = []byte{0x1B, 0x61, 0x00}       // ESC a 0 — left
	cmdAlignCenter = []byte{0x1B, 0x61, 0x01}       // ESC a 1 — center
	cmdAlignRight  = []byte{0x1B, 0x61, 0x02}       // ESC a 2 — right
	cmdCutPartial  = []byte{0x1D, 0x56, 0x42, 0x00} // GS V 66 0 — partial cut
)

// Builder accumulates ESC/POS commands into a byte slice.
// Start with New(), chain commands, and call Bytes() to get the final stream.
type Builder struct {
	buf []byte
}

// New returns a Builder pre-seeded with the ESC @ initialize command.
func New() *Builder {
	b := &Builder{}
	b.buf = append(b.buf, cmdInit...)
	return b
}

func (b *Builder) raw(c []byte) *Builder {
	b.buf = append(b.buf, c...)
	return b
}

// Bold enables (true) or disables (false) bold text.
func (b *Builder) Bold(on bool) *Builder {
	if on {
		return b.raw(cmdBoldOn)
	}
	return b.raw(cmdBoldOff)
}

// Left sets left text alignment.
func (b *Builder) Left() *Builder { return b.raw(cmdAlignLeft) }

// Center sets center text alignment.
func (b *Builder) Center() *Builder { return b.raw(cmdAlignCenter) }

// Right sets right text alignment.
func (b *Builder) Right() *Builder { return b.raw(cmdAlignRight) }

// Text appends s without a newline.
func (b *Builder) Text(s string) *Builder {
	b.buf = append(b.buf, []byte(s)...)
	return b
}

// Line appends s followed by a newline.
func (b *Builder) Line(s string) *Builder {
	return b.Text(s + "\n")
}

// Feed appends n blank lines.
func (b *Builder) Feed(n int) *Builder {
	for range n {
		b.buf = append(b.buf, '\n')
	}
	return b
}

// Separator appends a horizontal rule of dashes followed by a newline.
func (b *Builder) Separator(width int) *Builder {
	return b.Line(strings.Repeat("-", width))
}

// Cut appends a partial paper cut command.
func (b *Builder) Cut() *Builder { return b.raw(cmdCutPartial) }

// PrintAreaWidth constrains the physical printable area to widthDots, measured
// from a left margin of 0 (GS L) to widthDots (GS W). This is what actually
// tells the printer hardware how wide the paper is — without it, the printer
// always uses its full head width regardless of the configured paper size.
// Needed when a wider print head (e.g. 80mm) has narrower paper installed
// (e.g. 58mm): without this, content is laid out for 58mm in software but the
// printer still prints across its full 80mm head.
func (b *Builder) PrintAreaWidth(widthDots int) *Builder {
	nL, nH := lowHighBytes(widthDots)
	b.raw([]byte{0x1D, 0x4C, 0x00, 0x00})    // GS L 0 0 — left margin = 0
	return b.raw([]byte{0x1D, 0x57, nL, nH}) // GS W nL nH — print area width
}

// lowHighBytes splits n into the little-endian (nL, nH) byte pair used by
// two-byte ESC/POS numeric parameters.
func lowHighBytes(n int) (nL, nH byte) {
	return byte(n & 0xFF), byte((n >> 8) & 0xFF)
}

// Drawer sends a cash drawer opening pulse.
func (b *Builder) Drawer() *Builder {
	return b.raw([]byte{0x1B, 0x70, 0x00, 0x19, 0xFA})
}

// Size sets character size. Supported values: "normal", "medium" (double height), "double" (double height + width).
func (b *Builder) Size(size string) *Builder {
	switch size {
	case "double":
		return b.raw([]byte{0x1D, 0x21, 0x11})
	case "medium":
		return b.raw([]byte{0x1D, 0x21, 0x01})
	default:
		return b.raw([]byte{0x1D, 0x21, 0x00})
	}
}

// QR prints a native ESC/POS QR code.
func (b *Builder) QR(data string, size int) *Builder {
	if data == "" {
		return b
	}
	if size < 1 {
		size = 6
	} else if size > 16 {
		size = 16
	}
	// 1. Select Model 2
	b.raw([]byte{0x1D, 0x28, 0x6B, 0x04, 0x00, 0x31, 0x41, 0x32, 0x00})
	// 2. Set size
	b.raw([]byte{0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x43, byte(size)})
	// 3. Set EC level M
	b.raw([]byte{0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x44, 49})
	// 4. Store data
	length := len(data) + 3
	pL := byte(length & 0xFF)
	pH := byte((length >> 8) & 0xFF)
	b.raw([]byte{0x1D, 0x28, 0x6B, pL, pH, 0x31, 0x50, 48})
	b.raw([]byte(data))
	// 5. Print
	b.raw([]byte{0x1D, 0x28, 0x6B, 0x03, 0x00, 0x31, 0x51, 48})
	return b
}

// Barcode prints a 1D barcode using GS k (new format).
// Supported symbologies: CODE128, CODE39, EAN13, EAN8, UPCA, UPCE, ITF.
// height is bar height in dots (default 80), width is module width 1–6 (default 2).
// hri controls the Human Readable Interpretation position: "below" (default), "above", "both", "none".
func (b *Builder) Barcode(symbology, value string, height, width int, hri string) *Builder {
	if value == "" {
		return b
	}

	// GS h n — set barcode height
	if height <= 0 {
		height = 80
	}
	b.raw([]byte{0x1D, 0x68, byte(height)})

	// GS w n — set barcode module width
	if width < 1 || width > 6 {
		width = 2
	}
	b.raw([]byte{0x1D, 0x77, byte(width)})

	// GS H n — set HRI position (0=none, 1=above, 2=below, 3=both)
	hriPos := byte(2)
	switch hri {
	case "none":
		hriPos = 0
	case "above":
		hriPos = 1
	case "both":
		hriPos = 3
	}
	b.raw([]byte{0x1D, 0x48, hriPos})

	// GS k m n d1...dn — print barcode (new format, m >= 65)
	var m byte
	switch strings.ToUpper(symbology) {
	case "UPCA":
		m = 65
	case "UPCE":
		m = 66
	case "EAN13":
		m = 67
	case "EAN8":
		m = 68
	case "CODE39":
		m = 69
	case "ITF":
		m = 70
	default: // CODE128 y cualquier otro
		m = 73
	}

	data := []byte(value)
	b.raw([]byte{0x1D, 0x6B, m, byte(len(data))})
	b.raw(data)
	b.raw([]byte{'\n'})
	return b
}

// Bytes returns the accumulated ESC/POS byte sequence.
func (b *Builder) Bytes() []byte { return b.buf }
