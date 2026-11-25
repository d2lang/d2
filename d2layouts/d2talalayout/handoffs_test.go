package d2talalayout

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/hierarchy"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/textmeasure"
)

const handoffFixture = `direction: right
events: Published events {
  placed: Order placed
  stock: Stock changed
  payment: Card captured
  refund: Refund issued
  disputed: Dispute opened
}
workers: Subscribers {
  dispatch: Fulfillment
  stock: Availability
  ledger: Sales ledger
  refund: Reimbursements
  disputes: Risk review
}
events.placed -> workers.dispatch
events.stock -> workers.stock
events.payment -> workers.ledger
events.refund -> workers.refund
events.disputed -> workers.disputes
events.placed -> workers.stock
events.payment -> workers.dispatch
events.refund -> workers.ledger
events.disputed -> workers.refund
`

func handoffGraph(t *testing.T, source string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("handoff.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, r, nil, nil); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestHandoffsPlaceMatchedPortsAtCenter(t *testing.T) {
	for _, direction := range []string{"right", "left"} {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reversed=%v", direction, reversed), func(t *testing.T) {
				source := strings.Replace(handoffFixture, "direction: right", "direction: "+direction, 1)
				if reversed {
					var lines []string
					for line := range strings.Lines(source) {
						from, to, ok := strings.Cut(strings.TrimSpace(line), " -> ")
						if ok {
							line = to + " <- " + from + "\n"
						}
						lines = append(lines, line)
					}
					source = strings.Join(lines, "")
				}
				g := handoffGraph(t, source)
				if err := Layout(t.Context(), g, nil); err != nil {
					t.Fatal(err)
				}
				if len(g.Objects) != 12 || len(g.Edges) != 9 {
					t.Fatal("lost handoff entity")
				}
				for i, e := range g.Edges {
					if len(e.Route) != 2 {
						t.Fatalf("%s has %d route points", e.AbsID(), len(e.Route))
					}
					a, b, p, q := e.Src, e.Dst, e.Route[0], e.Route[1]
					if reversed {
						a, b, p, q = b, a, q, p
					}
					start, end := a.TopLeft.X+a.Width, b.TopLeft.X
					if direction == "left" {
						start, end = a.TopLeft.X, b.TopLeft.X+b.Width
					}
					if math.Abs(start-p.X) > .001 || math.Abs(end-q.X) > .001 {
						t.Errorf("%s ports do not face across corridor: %v", e.AbsID(), e.Route)
					}
					atCenter := p.Y == a.TopLeft.Y+a.Height/2 && q.Y == b.TopLeft.Y+b.Height/2
					if (i < 5) != atCenter || (i < 5) != (p.Y == q.Y) {
						t.Errorf("%s got route %v, center=%t", e.AbsID(), e.Route, atCenter)
					}
				}
			})
		}
	}
}

func TestHandoffFallbacks(t *testing.T) {
	for _, tc := range []struct{ name, extra string }{
		{"root-hierarchy", "shape: hierarchy"},
		{"inner-hierarchy", "workers.shape: hierarchy"},
		{"inner-direction", "events.direction: down"},
		{"explicit-frame", "events.width: 550"},
		{"fixed-origin", "workers.top: 110\nworkers.left: 260"},
		{"caption", "(events.placed -> workers.dispatch)[0]: dispatch"},
		{"outside-caption", "events.stock.label.near: outside-top-center"},
		{"authored-title", "events.label.near: bottom-center"},
		{"decorated-port", "(events.placed -> workers.dispatch)[0].target-arrowhead: count"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := handoffGraph(t, handoffFixture+tc.extra+"\n")
			if supportsHandoffs(g) {
				t.Fatal("accepted constrained interface")
			}
		})
	}
	for _, tc := range []struct{ name, extra string }{
		{"ambiguous", "events.stock -> workers.dispatch"},
		{"duplicate", "events.stock -> workers.stock"},
		{"internal", "events.placed -> events.stock"},
		{"backward", "workers.stock -> events.placed"},
		{"invisible-endpoint", "events.refund.label: \"\"\nevents.refund.style.fill: transparent\nevents.refund.style.stroke: transparent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := handoffGraph(t, handoffFixture+tc.extra+"\n")
			input, err := newSeedInput(t.Context(), g)
			if err != nil {
				t.Fatal(err)
			}
			changed, err := hierarchy.PlaceHandoffs(t.Context(), input.graph, nil)
			if err != nil || changed {
				t.Fatalf("accepted=%v, error=%v", changed, err)
			}
		})
	}
}

func TestHandoffPreservesSavedPanelTitles(t *testing.T) {
	g := handoffGraph(t, handoffFixture)
	// A serialized graph can carry chosen title positions even when the
	// original source left both panels' label.near unset.
	bottom, top := label.InsideBottomCenter.String(), label.InsideTopCenter.String()
	g.Root.Children["events"].LabelPosition = &bottom
	g.Root.Children["workers"].LabelPosition = &top
	data, err := d2graph.SerializeGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	restored := d2graph.NewGraph()
	if err := d2graph.DeserializeGraph(data, restored); err != nil {
		t.Fatal(err)
	}
	if err := Layout(t.Context(), restored, nil); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"events": bottom, "workers": top} {
		panel := restored.Root.Children[id]
		if panel.LabelPosition == nil || *panel.LabelPosition != want {
			t.Fatalf("%s lost its saved %s title", id, want)
		}
		w, h := float64(panel.LabelDimensions.Width), float64(panel.LabelDimensions.Height)
		pos := label.FromString(want).GetPointOnBox(panel.Box, label.PADDING, w, h)
		for _, child := range panel.ChildrenArray {
			if pos.Y < child.TopLeft.Y+child.Height && child.TopLeft.Y < pos.Y+h {
				t.Errorf("%s overlaps %s's title band", child.AbsID(), id)
			}
		}
	}
	for _, e := range restored.Edges[:5] {
		if len(e.Route) != 2 || e.Route[0].Y != e.Route[1].Y ||
			e.Route[0].Y != e.Src.TopLeft.Y+e.Src.Height/2 || e.Route[1].Y != e.Dst.TopLeft.Y+e.Dst.Height/2 {
			t.Errorf("%s was not placed on its shared row: %v", e.AbsID(), e.Route)
		}
	}
}

func TestHandoffRejectsUnmatchedPanelSizes(t *testing.T) {
	for _, source := range []string{
		"direction: right\na -> b\na -> b",
		handoffFixture + "workers.extra\n",
		handoffFixture + "events.refund.nested\n",
	} {
		if supportsHandoffs(handoffGraph(t, source)) {
			t.Fatal("accepted objects outside two equal rows of leaves")
		}
	}
}

func TestHandoffPortNeedsInteriorOfRenderedSide(t *testing.T) {
	for _, direction := range []string{"right", "left"} {
		for _, leaf := range []string{"events.stock", "workers.disputes"} {
			for _, height := range []int{1, 2} {
				t.Run(fmt.Sprintf("%s/%s/%d", direction, leaf, height), func(t *testing.T) {
					source := strings.Replace(handoffFixture, "direction: right", "direction: "+direction, 1)
					source += fmt.Sprintf("%s.label: \"\"\n%s.height: %d\n", leaf, leaf, height)
					input, err := newSeedInput(t.Context(), handoffGraph(t, source))
					if err != nil {
						t.Fatal(err)
					}
					changed, err := hierarchy.PlaceHandoffs(t.Context(), input.graph, nil)
					if err != nil || changed != (height == 2) {
						t.Fatalf("height %d, placed=%v, err=%v", height, changed, err)
					}
				})
			}
		}
	}
}
