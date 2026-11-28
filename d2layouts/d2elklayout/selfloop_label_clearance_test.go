package d2elklayout

import (
	"math"
	"testing"

	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestLayeredSelfLoopLabelClearance(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, input string
		loops       int
	}{
		{"retry", "worker -> worker: retry", 1},
		{"horizontal layout", "direction: right\nworker -> worker: wait for delivery receipt", 1},
		{"parallel", "start -> job: submit\njob -> job: retry\njob -> job: reset\njob -> done: finish", 2},
		{"inside container", "outer: { width: 440; height: 240; worker -> worker: retry }", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := compileELKGoDiagram(t, ruler, tc.input)
			if err := DefaultLayout(t.Context(), g); err != nil {
				t.Fatal(err)
			}
			diagram, err := d2exporter.Export(t.Context(), g, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			shapes := make(map[string]d2target.Shape)
			for _, shape := range diagram.Shapes {
				shapes[shape.ID] = shape
			}
			loops := 0
			for i, conn := range diagram.Connections {
				if conn.Src != conn.Dst {
					if conn.LabelPosition != label.InsideMiddleCenter.String() || conn.LabelPercentage != 0 {
						t.Errorf("ordinary label %q moved to %s (%g)", conn.Label, conn.LabelPosition, conn.LabelPercentage)
					}
					continue
				}
				loops++
				mask := renderedLabelMaskBox(conn)
				for _, obj := range g.Objects {
					shape := shapes[obj.AbsID()]
					body := geo.NewBox(geo.NewPoint(float64(shape.Pos.X), float64(shape.Pos.Y)), float64(shape.Width), float64(shape.Height))
					bottomRight := geo.NewPoint(mask.TopLeft.X+mask.Width, mask.TopLeft.Y+mask.Height)
					owner := g.Edges[i].Src
					if owner.IsDescendantOf(obj) && obj != owner && body.Contains(mask.TopLeft) && body.Contains(bottomRight) {
						continue
					}
					if mask.Overlaps(*body) {
						t.Errorf("self-loop label %q overlaps node %s", conn.Label, obj.AbsID())
					}
				}
				for _, other := range diagram.Connections {
					if other.ID != conn.ID && mask.Overlaps(*renderedLabelMaskBox(other)) {
						t.Errorf("label %q overlaps label %q", conn.Label, other.Label)
					}
					if renderedPathEntersMask(other, shapes, mask, false) {
						t.Errorf("label %q masks the rendered path of %s", conn.Label, other.ID)
					}
				}
			}
			if loops != tc.loops {
				t.Fatalf("got %d loops, want %d", loops, tc.loops)
			}
		})
	}
}

// SVG rounds the label origin and extends its mask by two units on either side.
func renderedLabelMaskBox(c d2target.Connection) *geo.Box {
	p := c.GetLabelTopLeft()
	return geo.NewBox(geo.NewPoint(math.Round(p.X)-2, math.Round(p.Y)), float64(c.LabelWidth+4), float64(c.LabelHeight))
}

// Test the stroked centerline that SVG generates, including the rounded loop
// corners. The route's polyline alone misses the Bézier between two short legs.
func renderedPathEntersMask(c d2target.Connection, shapes map[string]d2target.Shape, mask *geo.Box, bendsOnly bool) bool {
	src, dst := c.ArrowheadAdjustments(shapes)
	stroke := float64(c.StrokeWidth) / 2
	box := geo.NewBox(geo.NewPoint(mask.TopLeft.X-stroke, mask.TopLeft.Y-stroke), mask.Width+2*stroke, mask.Height+2*stroke)
	left, top := box.TopLeft.X, box.TopLeft.Y
	right, bottom := left+box.Width, top+box.Height
	corners := [4]*geo.Point{box.TopLeft, geo.NewPoint(right, top), geo.NewPoint(right, bottom), geo.NewPoint(left, bottom)}
	var prev geo.Point
	for cmd := range c.PathCommands(src, dst) {
		if bendsOnly && cmd.Kind != 'C' && cmd.Kind != 'S' {
			prev = cmd.End
			continue
		}
		if box.Contains(&cmd.End) {
			return true
		}
		switch cmd.Kind {
		case 'L':
			if box.Intersects(geo.Segment{Start: &prev, End: &cmd.End}, 0) {
				return true
			}
		case 'C', 'S':
			curve := geo.NewBezierCurve([]*geo.Point{&prev, &cmd.Control1, &cmd.Control2, &cmd.End})
			for i, start := range corners {
				side := geo.Segment{Start: start, End: corners[(i+1)%len(corners)]}
				if len(curve.Intersections(side)) > 0 {
					return true
				}
			}
		}
		prev = cmd.End
	}
	return false
}

func TestLayeredSelfLoopKeepsRoomyInlineLabel(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	g := compileELKGoDiagram(t, ruler, "worker.height: 360\nworker -> worker: retry")
	if err := DefaultLayout(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	edge := g.Edges[0]
	if *edge.LabelPosition != label.InsideMiddleCenter.String() || edge.LabelPercentage != nil {
		t.Errorf("roomy loop label moved to %s (%v)", *edge.LabelPosition, edge.LabelPercentage)
	}
}

func TestBoxedInSelfLoopKeepsItsPreviousLabel(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	g := compileELKGoDiagram(t, ruler, "worker -> worker: retry\nwest\neast\nnorth\nsouth")
	if err := DefaultLayout(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	edge := g.Edges[0]
	// A previous label pass can select a percentage other than the midpoint.
	position, percentage := label.UnlockedMiddle.String(), 0.4
	edge.LabelPosition, edge.LabelPercentage = &position, &percentage
	text := edgeLabelBox(edge, label.UnlockedMiddle, percentage)
	x, y := text.TopLeft.X-3, text.TopLeft.Y-1
	w, h := text.Width+6, text.Height+2
	// Four siblings leave only the original label rectangle available.
	const wall = 1000
	for id, box := range map[string]*geo.Box{
		"west":  geo.NewBox(geo.NewPoint(x-wall, y-wall), wall, 2*wall+h),
		"east":  geo.NewBox(geo.NewPoint(x+w, y-wall), wall, 2*wall+h),
		"north": geo.NewBox(geo.NewPoint(x, y-wall), w, wall),
		"south": geo.NewBox(geo.NewPoint(x, y+h), w, wall),
	} {
		g.Root.Children[id].Box = box
	}
	clearSelfLoopLabelBends(g)
	if *edge.LabelPosition != label.UnlockedMiddle.String() || edge.LabelPercentage == nil || *edge.LabelPercentage != 0.4 {
		t.Errorf("boxed-in label lost its previous placement: %s (%v)", *edge.LabelPosition, edge.LabelPercentage)
	}
}

func TestSelfLoopLabelCanMoveOneSibling(t *testing.T) {
	// Three nested west-facing loops from the Force session example. The
	// middle text sits on a long straight return, immediately outside the
	// rounded inner loop. The outermost loop bounds the space on its left.
	for _, ordinary := range []bool{false, true} {
		name := "movable sibling"
		if ordinary {
			name = "ordinary edge label stays fixed"
		}
		t.Run(name, func(t *testing.T) {
			g := d2graph.NewGraph()
			owner := &d2graph.Object{ID: "open", Parent: g.Root, Box: geo.NewBox(geo.NewPoint(298, 295), 83, 160)}
			g.Objects = []*d2graph.Object{owner}
			addLoop := func(labelWidth int, x, y1, y2 float64) *d2graph.Edge {
				edge := newLabeledSelfLoop(owner, labelWidth, 21, [][2]float64{{298, y1}, {x, y1}, {x, y2}, {298, y2}})
				g.Edges = append(g.Edges, edge)
				return edge
			}
			inner := addLoop(67, 248, 364, 387)
			middle := addLoop(86, 162, 342, 410)
			outer := addLoop(59, 80, 319, 433)
			if ordinary {
				// The adjacent label still blocks the preferred inner position,
				// but it belongs to an ordinary edge and cannot be reassigned.
				middle.Dst = &d2graph.Object{ID: "neighbor", Parent: g.Root, Box: geo.NewBox(geo.NewPoint(286, 402), 8, 16)}
				middle.Route[len(middle.Route)-1].X = middle.Dst.TopLeft.X
				g.Objects = append(g.Objects, middle.Dst)
			}
			shapes := map[string]d2target.Shape{}
			connection := func(e *d2graph.Edge) d2target.Connection { return *loopLabelConnection(g, e) }
			innerBefore, middleBefore := connection(inner), connection(middle)
			if !renderedPathEntersMask(innerBefore, shapes, renderedLabelMaskBox(innerBefore), true) ||
				renderedPathEntersMask(middleBefore, shapes, renderedLabelMaskBox(middleBefore), true) {
				t.Fatal("fixture must mask the inner bend while leaving the adjacent label on a clear straight run")
			}
			clearSelfLoopLabelBends(g)
			if ordinary {
				if *middle.LabelPosition != label.InsideMiddleCenter.String() || middle.LabelPercentage != nil {
					t.Fatal("moving a loop label reassigned the ordinary edge label")
				}
				if *inner.LabelPosition != label.InsideMiddleCenter.String() || inner.LabelPercentage != nil {
					t.Fatal("inner loop moved into the space occupied by the ordinary label")
				}
				return
			}
			innerAfter, middleAfter := connection(inner), connection(middle)
			if label.FromString(innerAfter.LabelPosition).IsOnEdge() || *renderedLabelMaskBox(middleBefore).TopLeft == *renderedLabelMaskBox(middleAfter).TopLeft {
				t.Fatalf("the inner bend and adjacent text require a paired move: %s, %s (%g)", innerAfter.LabelPosition, middleAfter.LabelPosition, middleAfter.LabelPercentage)
			}
			if *outer.LabelPosition != label.InsideMiddleCenter.String() || outer.LabelPercentage != nil {
				t.Fatal("the unrelated outermost label moved")
			}
			for i, e := range g.Edges {
				c := connection(e)
				box := renderedLabelMaskBox(c)
				if box.Overlaps(*owner.Box) {
					t.Fatalf("label %d overlaps its owner", i)
				}
				for j, other := range g.Edges {
					target := connection(other)
					if i != j && box.Overlaps(*renderedLabelMaskBox(target)) {
						t.Fatalf("labels %d and %d overlap", i, j)
					}
					// An inline label can cover its own straight line. No label
					// may cover its own rounded turn or any other path.
					allowOwnLine := i == j && label.FromString(c.LabelPosition).IsOnEdge()
					if renderedPathEntersMask(target, shapes, box, allowOwnLine) {
						t.Fatalf("label %d obscures path %d", i, j)
					}
				}
			}
		})
	}
}

func TestSelfLoopLabelIconGutter(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		properties string
		iconSize   int
	}{
		{"icon: ./image.svg", 32},
		{"icon.shape: circle", 10},
		{"icon: ./image.svg; icon.style.animated: 3/s", 0},
		{"icon.style.animated: true", 0},
	} {
		g := compileELKGoDiagram(t, ruler, "worker -> worker: retry {"+tc.properties+"}")
		if err := DefaultLayout(t.Context(), g); err != nil {
			t.Fatal(err)
		}
		c := loopLabelConnection(g, g.Edges[0])
		box := loopLabelBounds(c)
		gutter := c.GetLabelTopLeft().X - 2 - box.minX
		want := float64(tc.iconSize)
		if want != 0 {
			want += d2target.CONNECTION_ICON_LABEL_GAP
		}
		if math.Abs(gutter-want) > 1e-8 {
			t.Errorf("%s: icon gutter = %g, want %g", tc.properties, gutter, want)
		}
	}
}
