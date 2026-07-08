package escpos

import (
	"bytes"
	"testing"
)

func TestPrintAreaWidth(t *testing.T) {
	tests := []struct {
		name      string
		widthDots int
		want      []byte
	}{
		{"58mm (384 dots)", 384, []byte{0x1D, 0x4C, 0x00, 0x00, 0x1D, 0x57, 0x80, 0x01}},
		{"80mm (576 dots)", 576, []byte{0x1D, 0x4C, 0x00, 0x00, 0x1D, 0x57, 0x40, 0x02}},
		{"zero width", 0, []byte{0x1D, 0x4C, 0x00, 0x00, 0x1D, 0x57, 0x00, 0x00}},
		{"crosses byte boundary", 256, []byte{0x1D, 0x4C, 0x00, 0x00, 0x1D, 0x57, 0x00, 0x01}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := New().PrintAreaWidth(tc.widthDots)
			got := b.Bytes()
			if !bytes.HasSuffix(got, tc.want) {
				t.Errorf("PrintAreaWidth(%d) tail = %x, want suffix %x", tc.widthDots, got, tc.want)
			}
		})
	}
}
