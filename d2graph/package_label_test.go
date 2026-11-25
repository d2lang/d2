package d2graph_test

import (
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/util-go/go2"
)

func TestPackageGetLabelTopLeft(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		position label.Position
		labelW   int
		labelH   int
		wantX    float64
		wantY    float64
	}{
		{
			name:     "top-left fits in tab",
			position: label.InsideTopLeft,
			labelW:   40,
			labelH:   20,
			wantX:    5,
			wantY:    5,
		},
		{
			name:     "top-center fits in tab",
			position: label.InsideTopCenter,
			labelW:   40,
			labelH:   20,
			wantX:    30,
			wantY:    5,
		},
		{
			name:     "top-left too wide for tab falls back below",
			position: label.InsideTopLeft,
			labelW:   95,
			labelH:   20,
			wantX:    5,
			wantY:    45,
		},
		{
			name:     "top-left too tall for tab falls back below",
			position: label.InsideTopLeft,
			labelW:   40,
			labelH:   32,
			wantX:    5,
			wantY:    45,
		},
		{
			name:     "top-right stays in main body",
			position: label.InsideTopRight,
			labelW:   40,
			labelH:   20,
			wantX:    155,
			wantY:    45,
		},
		{
			name:     "center-center stays in main body",
			position: label.InsideMiddleCenter,
			labelW:   40,
			labelH:   20,
			wantX:    80,
			wantY:    110,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			obj := &d2graph.Object{
				Box: geo.NewBox(geo.NewPoint(0, 0), 200, 200),
				Attributes: d2graph.Attributes{
					Shape: d2graph.Scalar{Value: d2target.ShapePackage},
					LabelDimensions: d2target.TextDimensions{
						Width:  tc.labelW,
						Height: tc.labelH,
					},
				},
				LabelPosition: go2.Pointer(tc.position.String()),
			}
			got := obj.GetLabelTopLeft()
			if got == nil {
				t.Fatalf("GetLabelTopLeft() = nil")
			}
			if got.X != tc.wantX || got.Y != tc.wantY {
				t.Fatalf("GetLabelTopLeft() = (%v, %v), want (%v, %v)", got.X, got.Y, tc.wantX, tc.wantY)
			}
		})
	}
}
