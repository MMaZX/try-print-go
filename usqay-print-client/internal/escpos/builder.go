// Package escpos constructs ESC/POS byte sequences for thermal printers.
package escpos

import "strings"

// Standard ESC/POS command bytes.
var (
	cmdInit        = []byte{0x1B, 0x40}            // ESC @ — initialize printer
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

// Bytes returns the accumulated ESC/POS byte sequence.
func (b *Builder) Bytes() []byte { return b.buf }
