package shape

import (
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestPackageGetInsideLabelBox(t *testing.T) {
	t.Parallel()

	s := NewPackage(geo.NewBox(geo.NewPoint(10, 20), 200, 200))
	// topWidth = 100, topHeight = 40; innerBox starts at (10, 60) with 200x160.

	tests := []struct {
		name     string
		position label.Position
		w, h     float64
		wantBox  *geo.Box
	}{
		{
			name:     "InsideTopLeft fits exactly in tab",
			position: label.InsideTopLeft,
			w:        90,
			h:        30,
			wantBox:  geo.NewBox(geo.NewPoint(10, 20), 100, 40),
		},
		{
			name:     "InsideTopCenter fits in tab",
			position: label.InsideTopCenter,
			w:        50,
			h:        20,
			wantBox:  geo.NewBox(geo.NewPoint(10, 20), 100, 40),
		},
		{
			name:     "InsideTopLeft 1px wider than tab falls back to inner box",
			position: label.InsideTopLeft,
			w:        91,
			h:        30,
			wantBox:  geo.NewBox(geo.NewPoint(10, 60), 200, 160),
		},
		{
			name:     "InsideTopCenter 1px taller than tab falls back to inner box",
			position: label.InsideTopCenter,
			w:        50,
			h:        31,
			wantBox:  geo.NewBox(geo.NewPoint(10, 60), 200, 160),
		},
		{
			name:     "InsideTopRight stays in inner box",
			position: label.InsideTopRight,
			w:        40,
			h:        20,
			wantBox:  geo.NewBox(geo.NewPoint(10, 60), 200, 160),
		},
		{
			name:     "InsideMiddleCenter stays in inner box",
			position: label.InsideMiddleCenter,
			w:        40,
			h:        20,
			wantBox:  geo.NewBox(geo.NewPoint(10, 60), 200, 160),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := GetInsideLabelBox(s, tc.position, label.PADDING, tc.w, tc.h)
			if *got.TopLeft != *tc.wantBox.TopLeft || got.Width != tc.wantBox.Width || got.Height != tc.wantBox.Height {
				t.Fatalf("GetInsideLabelBox() = (%v, %vx%v), want (%v, %vx%v)",
					got.TopLeft, got.Width, got.Height, tc.wantBox.TopLeft, tc.wantBox.Width, tc.wantBox.Height)
			}
		})
	}
}
