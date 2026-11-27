package d2talalayout

import (
	"math"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

// When independent tiles are packed around a pin, an unrelated tile can end
// up closer to the pin than its requested neighbor. Close the gap along their
// existing axis, provided all other tiles and their alignments can stay put.
func tightenPinnedNear(source *d2graph.Graph, result seedResult) {
	g := result.graph
	const maxTiles = 16 // Bound the extra candidate checks on independent boards.
	if g.IsRootHierarchy || len(source.Edges) != 0 || len(g.Edges) != 0 || len(g.Nodes) < 3 || len(g.Nodes) > maxTiles ||
		len(g.Containers[nil]) != len(g.Nodes) || len(source.Objects) != len(g.Nodes) {
		return
	}
	var attached *d2graph.Object
	for _, obj := range source.Objects {
		if obj.NearKey != nil {
			if attached != nil {
				return
			}
			attached = obj
		}
		if obj.Icon != nil || obj.Tooltip != nil || obj.Link != nil || obj.ContentAspectRatio != nil || obj.Language != "" ||
			len(obj.Classes) != 0 || (obj.Left == nil) != (obj.Top == nil) ||
			obj.Style != (d2graph.Style{FontSize: obj.Style.FontSize}) || obj.IsGridDiagram() {
			return
		}
	}
	if attached == nil {
		return
	}
	var pin, tile *layoutgraph.Node
	for _, n := range g.Nodes {
		if n.Shape.GetType() != shape.SQUARE_TYPE || n.Is3D || n.IsMultiple || n.IsInvisible || n.Label != nil && (n.Label.Position != label.InsideMiddleCenter || n.Label.Width > n.Width || n.Label.Height > n.Height) {
			return
		}
		if n.TopLeft.X != math.Trunc(n.TopLeft.X) || n.TopLeft.Y != math.Trunc(n.TopLeft.Y) ||
			n.Width != math.Trunc(n.Width) || n.Height != math.Trunc(n.Height) {
			return
		}
		if n.FixedTopLeft != nil {
			if pin != nil {
				return
			}
			pin = n
		}
		if n.ID == result.bindings.objectIDs[attached] {
			tile = n
		}
	}
	if tile == nil || pin == nil || tile == pin {
		return
	}
	if _, ok := tile.Nears[pin]; !ok {
		return
	}
	gap := tile.DistanceTo(pin, true)
	if gap <= layoutgraph.NodeGap {
		return
	}
	closest := math.Inf(1)
	for _, n := range g.Nodes {
		if n != tile && n != pin {
			closest = min(closest, n.DistanceTo(pin, true))
		}
	}
	if closest >= gap || closest < layoutgraph.NodeGap {
		return
	}
	p := *tile.TopLeft
	// Moving straight toward the pin keeps the orthogonal projection and
	// stays inside the existing drawing's bounds.
	dx := max(pin.TopLeft.X-p.X-tile.Width, p.X-pin.TopLeft.X-pin.Width)
	dy := max(pin.TopLeft.Y-p.Y-tile.Height, p.Y-pin.TopLeft.Y-pin.Height)
	switch {
	case dx > 0 && dy < 0:
		p.X += math.Copysign(gap-layoutgraph.NodeGap, pin.TopLeft.X-p.X)
	case dy > 0 && dx < 0:
		p.Y += math.Copysign(gap-layoutgraph.NodeGap, pin.TopLeft.Y-p.Y)
	default:
		return
	}
	// Check the whole move so an unrelated tile cannot be passed on the way.
	swept := &layoutgraph.Node{Box: tile.Box}
	swept.TopLeft = tile.TopLeft.Copy()
	swept.TopLeft.X, swept.TopLeft.Y = min(p.X, tile.TopLeft.X), min(p.Y, tile.TopLeft.Y)
	swept.Width += math.Abs(p.X - tile.TopLeft.X)
	swept.Height += math.Abs(p.Y - tile.TopLeft.Y)
	aligned := func(a, width, b, otherWidth float64) bool {
		return a == b || a+width/2 == b+otherWidth/2 || a+width == b+otherWidth
	}
	for _, n := range g.Nodes {
		if n == pin || n == tile {
			continue
		}
		if swept.DistanceTo(n, true) < layoutgraph.NodeGap {
			return
		}
		if p.X != tile.TopLeft.X && aligned(tile.TopLeft.X, tile.Width, n.TopLeft.X, n.Width) {
			return
		}
		if p.Y != tile.TopLeft.Y && aligned(tile.TopLeft.Y, tile.Height, n.TopLeft.Y, n.Height) {
			return
		}
	}
	tile.TopLeft = &p
}
