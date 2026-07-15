package printer

// DeviceProfile holds the declarative capabilities and physical limits of a printer.
type DeviceProfile struct {
	WidthDots         int  `json:"width_dots"`
	DPI               int  `json:"dpi"`
	CharWidthDots     int  `json:"char_width_dots"`
	SupportsCut       bool `json:"supports_cut"`
	SupportsDrawer    bool `json:"supports_drawer"`
	SupportsQRNative  bool `json:"supports_qr_native"`
	SupportsPrintArea bool `json:"supports_print_area"`
	SupportsRaster    bool `json:"supports_raster"`
}

// Default58mmProfile returns a standard 58mm printer profile (384 dots printable area).
func Default58mmProfile() DeviceProfile {
	return DeviceProfile{
		WidthDots:         384,
		DPI:               203,
		CharWidthDots:     12,
		SupportsCut:       true,
		SupportsDrawer:    true,
		SupportsQRNative:  true,
		SupportsPrintArea: true,
		SupportsRaster:    true,
	}
}

// Default80mmProfile returns a standard 80mm printer profile (576 dots printable area).
func Default80mmProfile() DeviceProfile {
	return DeviceProfile{
		WidthDots:         576,
		DPI:               203,
		CharWidthDots:     12,
		SupportsCut:       true,
		SupportsDrawer:    true,
		SupportsQRNative:  true,
		SupportsPrintArea: true,
		SupportsRaster:    true,
	}
}

// DeriveProfileFromWidth derives a profile using linear interpolation between 58mm and 80mm.
func DeriveProfileFromWidth(widthMM float64) DeviceProfile {
	if widthMM <= 0 {
		return Default58mmProfile()
	}
	// Linear interpolation between (58mm, 384 dots) and (80mm, 576 dots)
	frac := (widthMM - 58.0) / (80.0 - 58.0)
	dots := 384.0 + frac*(576.0-384.0)
	widthDots := int(dots + 0.5) // round

	// Ensure safety limit
	if widthDots < 96 {
		widthDots = 96
	}

	return DeviceProfile{
		WidthDots:         widthDots,
		DPI:               203,
		CharWidthDots:     12,
		SupportsCut:       true,
		SupportsDrawer:    true,
		SupportsQRNative:  true,
		SupportsPrintArea: true,
		SupportsRaster:    true,
	}
}
