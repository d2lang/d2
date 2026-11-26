package d2grid

import (
	"context"
	"math"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

// A loop needs space outside its owner, even on a grid with no gutter.
// Measure the routed loops first, then route the remaining links between
// the final cell positions with the loop paths already in place.
func (gd *gridDiagram) routeCells(ctx context.Context, g *d2graph.Graph, direct []*d2graph.Edge, router d2graph.RouteEdges) error {
	var loops []*d2graph.Edge
	for _, e := range direct {
		if e.Src == e.Dst {
			loops = append(loops, e)
		}
	}
	if len(loops) == 0 || len(gd.objects) == 1 {
		return router(ctx, g, direct)
	}

	// A standalone router requires a complete route for each unselected
	// edge in its graph. The ordinary links have no routes yet.
	err := func() error {
		edges := g.Edges
		defer func() { g.Edges = edges }()
		g.Edges = loops
		return router(ctx, g, loops)
	}()
	if err != nil {
		return err
	}
	gd.spaceLoops(loops)
	var links []*d2graph.Edge
	for _, e := range direct {
		if e.Src != e.Dst {
			links = append(links, e)
		}
	}
	if len(links) == 0 {
		return nil
	}
	return router(ctx, g, links)
}

func (gd *gridDiagram) spaceLoops(loops []*d2graph.Edge) {
	margins := make(map[*d2graph.Object]geo.Spacing, len(gd.objects))
	for _, o := range gd.objects {
		margins[o] = o.GetMargin()
	}
	for _, e := range loops {
		x1, y1, x2, y2 := routeBounds(e)
		o := e.Src
		m := margins[o]
		m.Left, m.Right = max(m.Left, o.TopLeft.X-x1), max(m.Right, x2-o.TopLeft.X-o.Width)
		m.Top, m.Bottom = max(m.Top, o.TopLeft.Y-y1), max(m.Bottom, y2-o.TopLeft.Y-o.Height)
		margins[o] = m
	}
	tracks := gd.tracks
	if tracks == nil {
		// Fixed grids are stored in declaration order.
		stride := gd.rows
		if gd.rowDirected {
			stride = gd.columns
		}
		for remaining := gd.objects; len(remaining) != 0; {
			n := min(stride, len(remaining))
			tracks = append(tracks, remaining[:n])
			remaining = remaining[n:]
		}
	}
	// Within a fixed grid the cells at each index share a single column
	// (or row). In a dynamic grid each track spaces its own sequence.
	offsets := make(map[*d2graph.Object]geo.Point, len(gd.objects))
	spaceTracks := func(groups [][]*d2graph.Object, horizontal bool) float64 {
		far, shift := math.Inf(-1), 0.
		for _, group := range groups {
			near, end := math.Inf(1), math.Inf(-1)
			for _, o := range group {
				m := margins[o]
				a, b := o.TopLeft.Y-m.Top, o.TopLeft.Y+o.Height+m.Bottom
				if horizontal {
					a, b = o.TopLeft.X-m.Left, o.TopLeft.X+o.Width+m.Right
				}
				near, end = min(near, a), max(end, b)
			}
			shift = max(shift, far-near)
			far = end + shift
			for _, o := range group {
				d := offsets[o]
				if horizontal {
					d.X = shift
				} else {
					d.Y = shift
				}
				offsets[o] = d
			}
		}
		return shift
	}
	across := spaceTracks(tracks, !gd.rowDirected)
	var along float64
	if gd.tracks == nil {
		n := len(tracks[0])
		byIndex := make([][]*d2graph.Object, n)
		for _, track := range tracks {
			for i, o := range track {
				byIndex[i] = append(byIndex[i], o)
			}
		}
		along = spaceTracks(byIndex, gd.rowDirected)
	} else {
		for _, track := range tracks {
			cells := make([][]*d2graph.Object, len(track))
			for i := range track {
				cells[i] = track[i : i+1]
			}
			along = max(along, spaceTracks(cells, gd.rowDirected))
		}
	}
	if gd.rowDirected {
		gd.width += along
		gd.height += across
	} else {
		gd.width += across
		gd.height += along
	}
	for _, o := range gd.objects {
		d := offsets[o]
		o.MoveWithDescendants(d.X, d.Y)
	}
	for _, e := range loops {
		d := offsets[e.Src]
		e.Move(d.X, d.Y)
	}
}
