package escpos

import (
	"bytes"
	"testing"
)

func TestNewSelectsCodePage(t *testing.T) {
	got := New().Bytes()
	want := []byte{0x1B, 0x40, 0x1B, 0x74, 0x02} // ESC @ + ESC t 2 (PC850)
	if !bytes.Equal(got, want) {
		t.Errorf("New() = % X, want % X", got, want)
	}
}

func TestTextTranscodesCP850(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []byte
	}{
		{"minúsculas acentuadas", "áéíóúñü", []byte{0xA0, 0x82, 0xA1, 0xA2, 0xA3, 0xA4, 0x81}},
		{"mayúsculas acentuadas", "ÁÉÍÓÚÑ", []byte{0xB5, 0x90, 0xD6, 0xE0, 0xE9, 0xA5}},
		{"signos del español", "¿¡º", []byte{0xA8, 0xAD, 0xA7}},
		{"ascii pasa directo", "Total: S/ 25.00", []byte("Total: S/ 25.00")},
		{"runa sin mapeo cae a '?'", "€", []byte{'?'}},
		{"mezcla", "Añejo 3", []byte{'A', 0xA4, 'e', 'j', 'o', ' ', '3'}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := New().Text(tc.in).Bytes()
			if !bytes.HasSuffix(got, tc.want) {
				t.Errorf("Text(%q) tail = % X, want suffix % X", tc.in, got, tc.want)
			}
		})
	}
}

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
