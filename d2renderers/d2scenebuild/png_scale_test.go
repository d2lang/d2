package d2scenebuild

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestPNGDeviceScaleOptions(t *testing.T) {
	for _, options := range []Options{{}, {PNGDeviceScale: PNGDeviceScale{MaxScale: 2, MaxDimension: 300}}} {
		data, err := json.Marshal(options)
		if err != nil {
			t.Fatal(err)
		}
		if options == (Options{}) && bytes.Contains(data, []byte("PNGDeviceScale")) {
			t.Fatalf("zero options unexpectedly serialize the PNG target: %s", data)
		}
		var decoded Options
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded != options {
			t.Fatalf("options changed during JSON round trip: %+v", decoded)
		}
	}
}

func TestPNGStrokeAlignmentUsesActualViewport(t *testing.T) {
	for _, tc := range []struct {
		name           string
		width, height  int
		logical        float64
		centered, want bool
	}{
		{"one physical pixel per unit", 100, 20, .5, true, true},
		{"actual size rounds to one despite nominal scale", 100, 20, .499, true, true},
		{"leading-aligned with one spare pixel", 101, 20, .5, false, true},
		{"centered with half-pixel origin", 101, 20, .5, true, false},
		{"odd dimensions magnify both axes", 101, 21, .5, false, false},
		{"default double resolution", 100, 20, 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := builder{diagram: &d2target.Diagram{}, options: Options{
				Scale: &tc.logical, Center: &tc.centered, PNGDeviceScale: PNGDeviceScale{MaxScale: 2},
			}}
			if got := b.canAlignPNGHairlines(tc.width, tc.height); got != tc.want {
				t.Errorf("canAlignPNGHairlines = %t, want %t", got, tc.want)
			}
		})
	}
}
