package d2talalayout

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// Compare all serialized layout outputs, including box dimensions, world
// coordinates, label placement and automatically selected font metadata.
func TestPinnedTweenMatchesFullLayout(t *testing.T) {
	for _, tc := range []struct {
		name                                             string
		labeled, explicitCenter, overlap, circular, fast bool
	}{
		{name: "plain", fast: true}, {name: "marker over rail", overlap: true, circular: true, fast: true},
		{name: "default labels", labeled: true, fast: true}, {name: "locked center", labeled: true, explicitCenter: true, fast: true},
		{name: "overlapping captions", labeled: true, overlap: true},
		{name: "elliptical interior label", labeled: true, circular: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph := func(mark bool) *d2graph.Graph {
				g := d2graph.NewGraph()
				for i := range 3 {
					n := appendD2Object(g.Root, "n"+strconv.Itoa(i))
					x := 3 + 200*i
					if tc.overlap {
						x = 3 + 15*i
					}
					n.Left, n.Top = &d2graph.Scalar{Value: strconv.Itoa(x), TweenedPin: mark && i == 0}, &d2graph.Scalar{Value: "15"}
					n.Box = geo.NewBox(geo.NewPoint(0, 0), 105, 43)
					if tc.circular && i == 1 {
						n.Shape.Value = d2target.ShapeCircle
						n.Height = n.Width
					}
					n.WidthAttr, n.HeightAttr = &d2graph.Scalar{Value: "105"}, &d2graph.Scalar{Value: strconv.Itoa(int(n.Height))}
					if tc.labeled {
						n.Label.Value = "Sample"
						n.LabelDimensions = d2target.TextDimensions{Width: 43, Height: 15}
					}
					if tc.explicitCenter {
						n.LabelPosition = new(label.InsideMiddleCenter.String())
					}
				}
				return g
			}
			got, want := graph(true), graph(false)
			input, err := newSeedInput(t.Context(), got)
			if err != nil {
				t.Fatal(err)
			}
			if used := placePinnedSample(got, input); used != tc.fast {
				t.Fatalf("shortcut eligibility = %t", used)
			}
			for _, g := range []*d2graph.Graph{got, want} {
				if err := DefaultLayout(t.Context(), g); err != nil {
					t.Fatal(err)
				}
			}
			got.Objects[0].Left.TweenedPin = false
			got.Objects[0].MotionOffset = nil
			a, err := d2graph.SerializeGraph(got)
			if err != nil {
				t.Fatal(err)
			}
			b, err := d2graph.SerializeGraph(want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) {
				t.Fatalf("layout mismatch:\ngot %s\nwant %s", a, b)
			}
		})
	}
}

func TestPinnedSampleValidatesLayoutOptions(t *testing.T) {
	g := d2graph.NewGraph()
	n := appendD2Object(g.Root, "sample")
	n.Left, n.Top = &d2graph.Scalar{Value: "10", TweenedPin: true}, &d2graph.Scalar{Value: "0"}
	n.WidthAttr, n.HeightAttr = &d2graph.Scalar{Value: "10"}, &d2graph.Scalar{Value: "10"}
	original := *n.TopLeft
	err := Layout(t.Context(), g, &Options{MaxConcurrency: -1})
	if err == nil || !strings.Contains(err.Error(), "MaxConcurrency") || *n.TopLeft != original {
		t.Fatalf("invalid options changed pinned sample: %v, position %v", err, n.TopLeft)
	}
}
