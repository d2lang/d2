package d2talalayout

import (
	"context"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// ContinueBoard keeps the geometry of familiar nodes and connections across
// one edit to a board. Both inputs have completed ordinary layout. If the edit
// needs more than one local placement, the current board keeps that layout.
func ContinueBoard(ctx context.Context, previous, current *d2graph.Graph) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if previous == nil || current == nil || previous == current || previous.Root == nil || current.Root == nil ||
		previous.Root.Direction.Value != current.Root.Direction.Value ||
		!reflect.DeepEqual(previous.Data["tala-seeds"], current.Data["tala-seeds"]) {
		return false, nil
	}
	small, large := previous, current
	if len(small.Objects) > len(large.Objects) {
		small, large = large, small
	}
	added := len(large.Objects) - len(small.Objects)
	if added > 1 || len(large.Edges)-len(small.Edges) != added ||
		!continuityBoardSupported(previous) || !continuityBoardSupported(current) {
		return false, nil
	}
	// Index by D2's actual identifiers. Declaration order may differ between
	// boards even when they contain exactly the same logical objects.
	oldEdges := continuityEdges(previous)
	smallerEdges := oldEdges
	if small == current {
		smallerEdges = continuityEdges(current)
	}
	var leaf *d2graph.Object
	for _, n := range large.Objects {
		other := small.Root.Children[strings.ToLower(n.ID)]
		if other == nil {
			if added == 0 || leaf != nil {
				return false, nil
			}
			leaf = n
		} else if other.ID != n.ID || other.Width != n.Width || other.Height != n.Height ||
			other.Label.Value != n.Label.Value || other.LabelDimensions != n.LabelDimensions {
			return false, nil
		}
	}
	var link *d2graph.Edge
	for _, e := range large.Edges {
		if smallerEdges[e.AbsID()] == nil {
			if leaf == nil || link != nil || e.Src == e.Dst || e.Src != leaf && e.Dst != leaf {
				return false, nil
			}
			link = e
		}
	}
	if (leaf == nil) != (link == nil) {
		return false, nil
	}

	// If ordinary layout already kept every old route and box up to a single
	// translation, keep its choice of ports and position for the new leaf too.
	dx, dy, translated := boardTranslation(previous, current, oldEdges)
	var box *geo.Box
	var route []*geo.Point
	if current == large && leaf != nil && !translated {
		host := link.Src
		if host == leaf {
			host = link.Dst
		}
		priorHost := previous.Root.Children[strings.ToLower(host.ID)]
		boxes := make([]*geo.Box, 0, len(previous.Objects))
		for _, n := range previous.Objects {
			boxes = append(boxes, n.Box)
		}
		routes := make([][]*geo.Point, 0, len(previous.Edges))
		for _, e := range previous.Edges {
			routes = append(routes, e.Route)
		}
		direction := parseDirection(current.Root.Direction.Value)
		if link.SrcArrow == link.DstArrow {
			direction = geo.NONE
		} else if link.SrcArrow {
			direction = direction.GetOpposite()
		}
		box, route = placeBoardLeaf(ctx, priorHost.Box, leaf.Box, boxes, routes, direction, link.Src == leaf)
		if box == nil {
			return false, ctx.Err()
		}
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}
	if translated {
		if dx != 0 || dy != 0 {
			for _, n := range current.Objects {
				n.Box = geo.NewBox(geo.NewPoint(n.TopLeft.X+dx, n.TopLeft.Y+dy), n.Width, n.Height)
			}
			for _, e := range current.Edges {
				e.Route = translatedBoardRoute(e.Route, dx, dy)
			}
		}
		return true, nil
	}
	for _, n := range current.Objects {
		if old := previous.Root.Children[strings.ToLower(n.ID)]; old != nil {
			n.Box = old.Box.Copy()
		} else {
			n.Box = box
		}
	}
	for _, e := range current.Edges {
		if old := oldEdges[e.AbsID()]; old != nil {
			e.Route = translatedBoardRoute(old.Route, 0, 0)
		} else {
			e.Route = route
		}
	}
	return true, nil
}

func translatedBoardRoute(route []*geo.Point, dx, dy float64) []*geo.Point {
	points := make([]*geo.Point, len(route))
	for i, p := range route {
		points[i] = geo.NewPoint(p.X+dx, p.Y+dy)
	}
	return points
}

func continuityEdges(g *d2graph.Graph) map[string]*d2graph.Edge {
	edges := make(map[string]*d2graph.Edge, len(g.Edges))
	for _, e := range g.Edges {
		edges[e.AbsID()] = e
	}
	return edges
}

func continuityBoardSupported(g *d2graph.Graph) bool {
	r := g.Root
	if !g.IsWholeLayout() || g.Legend != nil || len(g.Objects) == 0 || len(g.Objects) > 256 || len(g.Edges) > 256 ||
		r.Shape.Value != "" && r.Shape.Value != d2target.ShapeRectangle || r.GridRows != nil || r.GridColumns != nil ||
		r.WidthAttr != nil || r.HeightAttr != nil || r.Top != nil || r.Left != nil || r.NearKey != nil ||
		len(r.Children) != len(g.Objects) {
		return false
	}
	for _, n := range g.Objects {
		if n == nil || n.Parent != r || r.Children[strings.ToLower(n.ID)] != n || n.IsContainer() ||
			n.Shape.Value != "" && n.Shape.Value != d2target.ShapeRectangle && n.Shape.Value != d2target.ShapeSquare ||
			n.Left != nil || n.Top != nil || n.NearKey != nil || n.GridRows != nil || n.GridColumns != nil ||
			n.Direction.Value != "" || n.Language != "" || n.ZIndex != 0 || continuityHidden(n.Style) ||
			n.Box == nil || !boardLeafPoint(n.TopLeft) || !boardLeafDimension(n.Width) || !boardLeafDimension(n.Height) ||
			n.LabelPosition != nil && label.FromString(*n.LabelPosition) != label.InsideMiddleCenter ||
			float64(n.LabelDimensions.Width) > n.Width || float64(n.LabelDimensions.Height) > n.Height {
			return false
		}
		if unpaintedBody(n, g) {
			_, alpha := paintAlphaRange(n.Style.FontColor)
			if alpha == 0 || strings.TrimSpace(n.Label.Value) == "" {
				return false
			}
		}
	}
	points := 0
	for _, e := range g.Edges {
		if e == nil || e.Src == nil || e.Dst == nil || r.Children[strings.ToLower(e.Src.ID)] != e.Src ||
			r.Children[strings.ToLower(e.Dst.ID)] != e.Dst || e.SrcArrowhead != nil || e.DstArrowhead != nil ||
			e.Label.Value != "" || e.IsCurve || continuityHidden(e.Style) || len(e.Route) < 2 || len(e.Route) > 4096-points {
			return false
		}
		if e.Style.StrokeWidth != nil && e.Style.StrokeWidth.TweenOffset != 0 {
			return false
		}
		if _, alpha := paintAlphaRange(e.Style.Stroke); alpha == 0 {
			return false
		}
		points += len(e.Route)
		for _, p := range e.Route {
			if !boardLeafPoint(p) {
				return false
			}
		}
	}
	return supportsRefinementStyles(g)
}

func continuityHidden(style d2graph.Style) bool {
	if style.Opacity == nil {
		return false
	}
	opacity, err := strconv.ParseFloat(style.Opacity.Value, 64)
	return err != nil || opacity <= 0
}

func boardTranslation(previous, current *d2graph.Graph, oldEdges map[string]*d2graph.Edge) (dx, dy float64, ok bool) {
	for _, n := range current.Objects {
		old := previous.Root.Children[strings.ToLower(n.ID)]
		if old == nil {
			continue
		}
		x, y := old.TopLeft.X-n.TopLeft.X, old.TopLeft.Y-n.TopLeft.Y
		if ok && (x != dx || y != dy) || x != math.Trunc(x) || y != math.Trunc(y) {
			return 0, 0, false
		}
		dx, dy, ok = x, y, true
	}
	for _, e := range current.Edges {
		if old := oldEdges[e.AbsID()]; old != nil {
			if !slices.EqualFunc(e.Route, old.Route, func(a, b *geo.Point) bool {
				return a.X+dx == b.X && a.Y+dy == b.Y
			}) {
				return 0, 0, false
			}
		}
	}
	// Shape coordinates are truncated to integers at export. A fractional
	// position crossing zero can otherwise change its port by one pixel.
	for _, n := range current.Objects {
		p := n.TopLeft
		if !boardLeafPoint(geo.NewPoint(p.X+dx, p.Y+dy)) ||
			float64(int(p.X+dx)-int(p.X)) != dx || float64(int(p.Y+dy)-int(p.Y)) != dy {
			return 0, 0, false
		}
	}
	if dx != 0 || dy != 0 {
		for _, e := range current.Edges {
			for _, p := range e.Route {
				if !boardLeafPoint(geo.NewPoint(p.X+dx, p.Y+dy)) {
					return 0, 0, false
				}
			}
		}
	}
	return dx, dy, ok
}
