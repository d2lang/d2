package labeling

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

func TestFixedCaptionReservesSpaceInEitherDeclarationOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		g := layoutgraph.NewGraph()
		lead := layoutgraph.NewNode(1, 80, 80)
		lead.SetShape(shape.PERSON_TYPE)
		lead.TopLeft = geo.NewPoint(15, 0)
		lead.Label = &layoutgraph.Label{Text: "Lead designer", Width: 99, Height: 21}
		dept := layoutgraph.NewNode(2, 110, 60)
		dept.TopLeft = geo.NewPoint(0, 115)
		dept.Label = &layoutgraph.Label{Text: "Engineering", Width: 85, Height: 21, Position: label.OutsideTopCenter}
		if reverse {
			g.AddNode(dept)
			g.AddNode(lead)
		} else {
			g.AddNode(lead)
			g.AddNode(dept)
		}
		Initialize(g)
		if err := Place(t.Context(), g); err != nil {
			t.Fatal(err)
		}
		if lead.Label.Position != label.OutsideTopCenter || dept.Label.Position != label.OutsideTopCenter {
			t.Errorf("reverse %t: lead=%s department=%s", reverse, lead.Label.Position, dept.Label.Position)
		}
	}
}

func TestPlaceNewEdgesAvoidsFixedNodeCaption(t *testing.T) {
	g := layoutgraph.NewGraph()
	add := func(id int64, x, y, w, h float64) *layoutgraph.Node {
		n := g.AddNode(layoutgraph.NewNode(id, w, h))
		n.TopLeft = geo.NewPoint(x, y)
		return n
	}
	from, to := add(1, 0, 0, 40, 40), add(2, 340, 0, 40, 40)
	e := g.Connect(from, to)
	e.Points = []*geo.Point{geo.NewPoint(40, 20), geo.NewPoint(340, 20)}
	e.Label = &layoutgraph.Label{Text: "new", Width: 60, Height: 16}
	if err := PlaceNewEdges(t.Context(), g, []*layoutgraph.Edge{e}); err != nil {
		t.Fatal(err)
	}
	initial := e.LabelTopLeft(e.Label.Position, e.Label.Width, e.Label.Height)
	// Place the node beyond the trial caption. Only its outside text
	// occupies the location the edge label would otherwise choose.
	n := add(3, initial.X+20, initial.Y+e.Label.Height+label.PADDING, 20, 20)
	n.Label = &layoutgraph.Label{Text: "fixed", Position: label.OutsideTopCenter, Width: 60, Height: 16}
	n.Label.FixPosition()
	if err := PlaceNewEdges(t.Context(), g, []*layoutgraph.Edge{e}); err != nil {
		t.Fatal(err)
	}
	box := geo.NewBox(e.LabelTopLeft(e.Label.Position, e.Label.Width, e.Label.Height), e.Label.Width, e.Label.Height)
	reserved := geo.NewBox(n.LabelTopLeft(label.OutsideTopCenter, 60, 16), 60, 16)
	if box.Overlaps(*reserved) || n.Label.Position != label.OutsideTopCenter {
		t.Fatalf("new edge label overlaps or moves fixed caption: %v", box)
	}
}
