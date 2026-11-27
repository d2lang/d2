package hierarchy

import (
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/xsort"
)

// Six rows bound the joint ordering search to 720 arrangements. Both columns
// share an order, so a primary connection always keeps both center ports.
const MaxHandoffRows = 6

type handoffRow struct {
	from, to *layoutgraph.Node
}

// uniqueHandoffRows finds a perfect matching by peeling forced rows. In a
// square graph with a unique perfect matching some row must have degree one:
// otherwise following an alternative edge and then a matched edge eventually
// closes an alternating cycle. The same argument applies after each removal.
func uniqueHandoffRows(from, to []*layoutgraph.Node) []handoffRow {
	available := make(map[*layoutgraph.Node]bool, len(to))
	for _, n := range to {
		available[n] = true
	}
	rows := make([]handoffRow, len(from))
	for range rows {
		found := false
		for i, n := range from {
			if rows[i].to != nil {
				continue
			}
			var only *layoutgraph.Node
			for _, e := range n.Edges {
				if !available[e.To] {
					continue
				}
				if only != nil {
					only = nil
					break
				}
				only = e.To
			}
			if only != nil {
				rows[i] = handoffRow{n, only}
				delete(available, only)
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return rows
}

func orderHandoffRows(ctx context.Context, rows []handoffRow, edges []*layoutgraph.Edge) []handoffRow {
	index := make(map[*layoutgraph.Node]int, len(rows)*2)
	for i, row := range rows {
		index[row.from], index[row.to] = i, i
	}
	connections := make([][2]int, len(edges))
	for i, e := range edges {
		connections[i] = [2]int{index[e.From], index[e.To]}
	}
	// Compare crossings first, then the total distance away from each primary
	// row, then displacement from source declaration order. Visit remaining
	// exact ties in declaration order.
	bestCost := [3]int{math.MaxInt, math.MaxInt, math.MaxInt}
	// Every off-row connection spans at least one row. If declaration order
	// attains that bound without crossings, it is already the optimal order.
	minCost := [3]int{0, len(edges) - len(rows), 0}
	var best []handoffRow
	order := make([]int, len(rows))
	position := make([]int, len(rows))
	var visit func(int, int)
	visit = func(depth, used int) {
		if ctx.Err() != nil || bestCost == minCost {
			return
		}
		if depth < len(rows) {
			for i := range rows {
				if used&(1<<i) == 0 {
					order[depth], position[i] = i, depth
					visit(depth+1, used|(1<<i))
				}
			}
			return
		}
		var cost [3]int
		for i, e := range connections {
			a, b := position[e[0]], position[e[1]]
			cost[1] += max(a-b, b-a)
			for _, other := range connections[:i] {
				if (a-position[other[0]])*(b-position[other[1]]) < 0 {
					cost[0]++
				}
			}
		}
		for i, at := range position {
			cost[2] += max(at-i, i-at)
		}
		if slices.Compare(cost[:], bestCost[:]) < 0 {
			bestCost = cost
			best = make([]handoffRow, len(rows))
			for i, at := range order {
				best[i] = rows[at]
			}
		}
	}
	visit(0, 0)
	return best
}

// PlaceHandoffs composes two shallow rectangular containers into parallel rows.
// The adapter supplies the measured, isolated candidate and checks authored
// geometry. Candidate geometry is committed together after all ports fit.
func PlaceHandoffs(ctx context.Context, g *layoutgraph.Graph, declaration map[layoutgraph.EntityID]int) (bool, error) {
	roots := g.Containers[nil]
	if len(roots) != 2 || len(g.Edges) == 0 || len(g.Edges) > MaxHandoffRows*(MaxHandoffRows+1)/2 ||
		!g.Direction(nil).IsHorizontal() {
		return false, nil
	}
	source, target := g.Edges[0].From.Container, g.Edges[0].To.Container
	if source == nil || target == nil || source == target || source.Container != nil || target.Container != nil {
		return false, nil
	}
	from, to := g.Containers[source], g.Containers[target]
	if len(from) != len(to) || len(from) < 2 || len(from) > MaxHandoffRows || len(g.Nodes) != len(from)*2+2 {
		return false, nil
	}
	seen := make(map[[2]*layoutgraph.Node]bool, len(g.Edges))
	for _, e := range g.Edges {
		pair := [2]*layoutgraph.Node{e.From, e.To}
		if seen[pair] || e.From.Container != source || e.To.Container != target || e.From.IsInvisible || e.To.IsInvisible ||
			e.HasSourceArrow() || !e.HasTargetArrow() || e.IsInvisible || e.IsCurve ||
			e.Label != nil || e.SourceArrowheadLabel != nil || e.TargetArrowheadLabel != nil {
			return false, nil
		}
		seen[pair] = true
	}
	from = slices.Clone(from)
	xsort.Func(from, func(a, b *layoutgraph.Node) int { return declaration[a.ID] - declaration[b.ID] })
	rows := uniqueHandoffRows(from, to)
	if rows == nil {
		return false, nil
	}
	rows = orderHandoffRows(ctx, rows, g.Edges)
	if err := ctx.Err(); err != nil {
		return false, err
	}

	const inset, corridor, rowGap = 24., 80., 26.
	widths := [2]float64{}
	var topTitle, bottomTitle float64
	for i, root := range []*layoutgraph.Node{source, target} {
		if root.Label != nil {
			widths[i] = root.Label.Width
			if root.Label.PositionFixed() && root.Label.Position == label.InsideBottomCenter {
				bottomTitle = max(bottomTitle, root.Label.Height)
			} else {
				topTitle = max(topTitle, root.Label.Height)
			}
		}
		for _, child := range g.Containers[root] {
			// Integer shape bounds need at least one interior pixel for
			// the primary port between the two corners of a vertical face.
			if child.Height < 2 {
				return false, nil
			}
			widths[i] = max(widths[i], child.Width)
			if l := child.Label; l != nil && (l.Position.IsOutside() || l.Width > child.Width || l.Height > child.Height) {
				return false, nil
			}
		}
		widths[i] += 2 * inset
	}
	y := topTitle + 20
	positions := make(map[*layoutgraph.Node]*geo.Point, len(g.Nodes))
	indices := make(map[*layoutgraph.Node]int, 2*len(rows))
	for i, row := range rows {
		h := max(row.from.Height, row.to.Height)
		positions[row.from] = geo.NewPoint(widths[0]-inset-row.from.Width, y+(h-row.from.Height)/2)
		positions[row.to] = geo.NewPoint(widths[0]+corridor+inset, y+(h-row.to.Height)/2)
		indices[row.from], indices[row.to] = i, i
		y += h + rowGap
	}
	height := y - rowGap + inset + bottomTitle
	positions[source], positions[target] = geo.NewPoint(0, 0), geo.NewPoint(widths[0]+corridor, 0)
	type ports struct{ from, to float64 }
	slots := make(map[*layoutgraph.Edge]ports, len(g.Edges))
	// Keep every secondary port on the side of its matching center port that
	// faces the other row. More distant rows get the outermost slots.
	for _, row := range rows {
		for _, n := range []*layoutgraph.Node{row.from, row.to} {
			var above, below []*layoutgraph.Edge
			delta := func(e *layoutgraph.Edge) int { return indices[e.From] + indices[e.To] - 2*indices[n] }
			for _, e := range n.Edges {
				switch d := delta(e); {
				case d < 0:
					above = append(above, e)
				case d > 0:
					below = append(below, e)
				}
			}
			for sign, group := range [][]*layoutgraph.Edge{above, below} {
				if len(group) == 0 {
					continue
				}
				pitch := min(12., math.Floor((n.Height/2-6)/float64(len(group))))
				if pitch < 10 {
					return false, nil
				}
				multiplier := 2*sign - 1
				xsort.Func(group, func(a, b *layoutgraph.Edge) int { return multiplier * (delta(a) - delta(b)) })
				for i, e := range group {
					offset := float64(multiplier*(i+1)) * pitch
					p := slots[e]
					if n == e.From {
						p.from = offset
					} else {
						p.to = offset
					}
					slots[e] = p
				}
			}
		}
	}
	flip := g.Direction(nil) == geo.Left
	totalWidth := widths[0] + corridor + widths[1]
	if totalWidth > limits.MaxGraphSize || height > limits.MaxGraphSize {
		return false, nil
	}
	routes := make([][]*geo.Point, len(g.Edges))
	for i, e := range g.Edges {
		p, q := positions[e.From], positions[e.To]
		a := geo.NewPoint(p.X+e.From.Width, p.Y+e.From.Height/2+slots[e].from)
		b := geo.NewPoint(q.X, q.Y+e.To.Height/2+slots[e].to)
		// Above this slope, separate rows begin reading like a vertical fan.
		if math.Abs(a.Y-b.Y) > 1.4*(b.X-a.X) {
			return false, nil
		}
		if flip {
			a.X, b.X = totalWidth-a.X, totalWidth-b.X
		}
		routes[i] = []*geo.Point{a, b}
	}
	source.Width, target.Width = widths[0], widths[1]
	source.Height, target.Height = height, height
	for _, root := range roots {
		if root.Label != nil && !root.Label.PositionFixed() {
			root.Label.Position = label.InsideTopCenter
		}
	}
	for n, p := range positions {
		if flip {
			p.X = totalWidth - p.X - n.Width
		}
		n.TopLeft = p
	}
	for i, e := range g.Edges {
		e.Points = routes[i]
	}
	return true, nil
}
