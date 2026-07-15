package printer

import "testing"

func TestDetectModeFromName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Térmicas con keyword de marca/serie
		{"Epson TM series", "EPSON TM-T20III Receipt", "escpos"},
		{"Star TSP", "Star TSP143IIIU", "escpos"},
		{"Xprinter", "Xprinter XP-58IIH", "escpos"},
		{"Bixolon", "BIXOLON SRP-350III", "escpos"},

		// Térmicas genéricas sin marca (clones chinos)
		{"POS80 sin separador", "POS80 Printer", "escpos"},
		{"POS-58 con guion", "POS-58", "escpos"},
		{"pos 80 con espacio", "pos 80 series", "escpos"},
		{"ancho 80mm en el nombre", "80mm Series Printer", "escpos"},
		{"ancho 58mm en el nombre", "Generic 58mm Thermal", "escpos"},
		{"driver ESCPOS", "ESCPOS Driver v2", "escpos"},
		{"driver ESC/POS", "Generic ESC/POS", "escpos"},
		{"Gprinter", "Gprinter GP-3120TU", "escpos"},
		{"Rongta", "Rongta RP328", "escpos"},

		// No térmicas: deben quedar en "text"
		{"HP laser", "HP LaserJet 1020", "text"},
		{"Canon inkjet", "Canon PIXMA G3110", "text"},
		{"Epson inkjet", "EPSON L3150 Series", "text"},
		{"Brother multifuncional", "Brother DCP-L2540DW", "text"},
		{"pos sin dígito no matchea", "Compositor Deluxe", "text"},
		{"PDF virtual", "Microsoft Print to PDF", "text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectModeFromName(tc.in); got != tc.want {
				t.Errorf("detectModeFromName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
