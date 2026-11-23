package ranklabel

import (
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestLabelClearsAnotherConnection(t *testing.T) {
	pos := label.UnlockedMiddle.String()
	e := &d2graph.Edge{
		Route:         []*geo.Point{geo.NewPoint(0, 50), geo.NewPoint(200, 50)},
		LabelPosition: &pos,
	}
	e.Label.Value = "resume"
	e.LabelDimensions = d2target.TextDimensions{Width: 48, Height: 20}
	play := &d2graph.Edge{Route: []*geo.Point{geo.NewPoint(100, 0), geo.NewPoint(100, 100)}}
	ClearOverlaps(&d2graph.Graph{Edges: []*d2graph.Edge{e, play}})
	if e.LabelPercentage == nil {
		t.Fatal("label stayed on the crossing")
	}
	pt, _ := label.FromString(*e.LabelPosition).GetPointOnRoute(e.Route, 2, *e.LabelPercentage, 48, 20)
	if pt.X <= 100 && pt.X+48 >= 100 {
		t.Fatalf("label at %v still covers the play route", pt)
	}
}
