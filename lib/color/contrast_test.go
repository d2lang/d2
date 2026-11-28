package color

import (
	"image/color"
	"testing"
)

func TestInverseContrast(t *testing.T) {
	for _, tc := range []struct{ background, want string }{
		{"#ffffff", "#000000"},
		{"#1e1e2e", "#e1e1d1"},
		{"#f2c36b", "#0d3c94"},
		{"#575757", "#a8a8a8"}, // just over 3:1
		{"#585858", "#ffffff"}, // just under 3:1
		{"#757575", "#ffffff"},
		{"#767676", "#000000"},
		{"#808080", "#000000"},
		{"#ff0000", "#00ffff"},
	} {
		bg, _ := ParseRGBA(tc.background)
		want, _ := ParseRGBA(tc.want)
		if got := InverseContrast(color.RGBA(bg)); got != color.RGBA(want) {
			t.Errorf("InverseContrast(%s) = %#v, want %s", tc.background, got, tc.want)
		}
	}
}
