package loader

import "testing"

func TestIsNormalMapProperty(t *testing.T) {
	tests := []struct {
		name     string
		property string
		want     bool
	}{
		{"legacy NormalMap", "NormalMap", true},
		{"content NormalMapContent", "NormalMapContent", true},
		{"mixed case NORMALMAP", "NORMALMAP", true},
		{"mixed case normalmapcontent", "normalmapcontent", true},
		{"whitespace padded", "  NormalMap  ", true},
		{"ColorMap is not normal", "ColorMap", false},
		{"ColorMapContent is not normal", "ColorMapContent", false},
		{"MetalnessMapContent is not normal", "MetalnessMapContent", false},
		{"empty is not normal", "", false},
		{"substring NormalMapX is not normal", "NormalMapX", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNormalMapProperty(tt.property); got != tt.want {
				t.Errorf("IsNormalMapProperty(%q) = %v, want %v", tt.property, got, tt.want)
			}
		})
	}
}

func TestClassifyAsBC3_NormalMapContentForcesBC3(t *testing.T) {
	// Opaque-alpha source on a normal slot: without the NormalMapContent
	// fix, this would fall through to BC1 (0.5 B/px) instead of BC3
	// (1.0 B/px DXT5nm), understating VRAM by 2x.
	if !ClassifyAsBC3(false, 0, "NormalMapContent") {
		t.Errorf("ClassifyAsBC3 should return true for NormalMapContent slot")
	}
	if !ClassifyAsBC3(false, 0, "NormalMap") {
		t.Errorf("ClassifyAsBC3 should return true for legacy NormalMap slot")
	}
}
