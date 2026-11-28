package d2lib

import (
	"cmp"
	"context"
	"image"
	"math"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/xsort"
)

// Lay out an automatic caption on the selected wire. A static labeler can
// alternate between distant clear segments for a single sample. Compare its
// choices across time with the actual held captions as immutable endpoints.
func stabilizeTweenEdgeCaptions(ctx context.Context, board *d2target.Diagram, graph *d2graph.Graph, budget *int) error {
	if graph == nil || len(board.TweenFrames) < 2 || len(graph.TweenFrames) != len(board.TweenFrames) {
		return nil
	}
	panels := append(append([]*d2target.Diagram(nil), board.TweenFrames...), board)
	frames := make([]tweenCaptionFrame, len(panels))
	edges := make([]map[string]*d2target.Connection, len(panels))
	for i, panel := range panels {
		cost := gridFrameElements(panel)
		if cost > *budget {
			return nil
		}
		*budget -= cost
		frames[i] = tweenCaptionFrame{board: panel, shapes: gridEdgeShapeMap(panel.Shapes)}
		edges[i] = make(map[string]*d2target.Connection, len(panel.Connections))
		g := graph
		if i < len(graph.TweenFrames) {
			g = graph.TweenFrames[i]
		}
		for _, e := range g.Edges {
			if e.Attributes.LabelPosition == nil && e.Label.Value != "" {
				edges[i][e.AbsID()] = nil
			}
		}
		for j := range panel.Connections {
			e := &panel.Connections[j]
			if _, automatic := edges[i][e.ID]; automatic && e.MotionLabel == nil && e.MotionClip == nil &&
				e.MotionTip == "" && !e.IsCurve && e.Icon == nil && validTweenRoute(e.Route) {
				edges[i][e.ID] = e
			}
		}
	}
	for i := range board.Connections {
		if err := ctx.Err(); err != nil {
			return err
		}
		if *budget <= 0 {
			break
		}
		end := &board.Connections[i]
		track := make([]*d2target.Connection, 0, len(frames))
		for _, frame := range edges {
			e := frame[end.ID]
			if e == nil || e.Label != end.Label || e.Src != end.Src || e.Dst != end.Dst || e.GetLabelTopLeft() == nil {
				break
			}
			track = append(track, e)
		}
		if len(track) == len(frames) {
			chooseEdgeCaptionTrack(track, frames, budget)
		}
	}
	return ctx.Err()
}

type edgeCaptionAnchor struct {
	position string
	fraction float64
}

type edgeCaptionChoice struct {
	anchor edgeCaptionAnchor
	point  geo.Point
}

func captionDistance2(a, b geo.Point) float64 {
	x, y := a.X-b.X, a.Y-b.Y
	return x*x + y*y
}

func chooseEdgeCaptionTrack(track []*d2target.Connection, frames []tweenCaptionFrame, budget *int) {
	n := len(track)
	choices := make([][]edgeCaptionChoice, n)
	unstable := false
	obscured := make([]bool, n)
	for i, e := range track {
		point := *e.GetLabelTopLeft()
		choices[i] = []edgeCaptionChoice{{anchor: edgeCaptionAnchor{e.LabelPosition, e.LabelPercentage}, point: point}}
		if i > 0 && captionDistance2(point, choices[i-1][0].point) > math.Pow(2*float64(max(1, e.LabelHeight)), 2) {
			unstable = true
		}
		if i > 0 && i < n-1 && e.Opacity > 0 {
			box := tweenRouteTextBox(&point, e.LabelWidth, e.LabelHeight)
			obscured[i] = !frames[i].clearBox(nil, e, box, budget) || !frames[i].ownStrokeClear(e, box, label.FromString(e.LabelPosition))
			unstable = unstable || obscured[i]
		}
	}
	if !unstable || *budget <= 0 {
		return
	}
	for i := 1; i < n-1; i++ {
		e := track[i]
		if e.Opacity <= 0 {
			continue
		}
		add := func(anchor edgeCaptionAnchor) {
			v := *e
			v.LabelPosition, v.LabelPercentage = anchor.position, anchor.fraction
			point := v.GetLabelTopLeft()
			if !tweenFinitePoint(point) {
				return
			}
			for _, old := range choices[i] {
				if captionDistance2(*point, old.point) < 1 {
					return
				}
			}
			box := tweenRouteTextBox(point, e.LabelWidth, e.LabelHeight).Inset(-label.PADDING)
			if frames[i].clearBox(nil, e, box, budget) && frames[i].ownStrokeClear(e, box.Inset(label.PADDING), label.FromString(anchor.position)) {
				choices[i] = append(choices[i], edgeCaptionChoice{anchor: anchor, point: *point})
			}
		}
		for _, j := range []int{i - 1, i + 1, 0, n - 1} {
			add(choices[j][0].anchor)
		}
		// A changing path length can move the same percentage across a bend.
		// Project the neighboring caption centers back onto this frame's route.
		for _, j := range []int{i - 1, i + 1} {
			projectEdgeCaption(e, choices[j][0].point, add)
		}
		// Two distant native positions can both be clear on a long run. Give
		// intermediate samples an anchor along that run if the text fits there.
		lo, hi := max(0, i-2), min(n-1, i+2)
		before, after := choices[lo][0].point, choices[hi][0].point
		t := float64(i-lo) / float64(hi-lo)
		projectEdgeCaption(e, geo.Point{X: before.X + t*(after.X-before.X), Y: before.Y + t*(after.Y-before.Y)}, add)
		// After choosing the neighboring wires together, the labeler's original
		// anchor can land on one of them. Search additional fractions only in
		// that case; leave the held boards and all authored anchors alone.
		if obscured[i] {
			for _, p := range edgeCaptionSides(e) {
				for _, fraction := range []float64{.25, .5, .75, .125, .375, .625, .875} {
					add(edgeCaptionAnchor{p.String(), fraction})
				}
			}
		}
		if *budget <= 0 {
			return
		}
	}
	for i, blocked := range obscured {
		if !blocked || len(choices[i]) < 2 {
			continue
		}
		native := choices[i][0]
		choices[i] = choices[i][1:]
		xsort.StableFunc(choices[i], func(a, b edgeCaptionChoice) int {
			return cmp.Compare(captionDistance2(a.point, native.point), captionDistance2(b.point, native.point))
		})
	}
	selected := chooseCaptionAnchors(choices, track)
	for i := 1; i < n-1; i++ {
		c := choices[i][selected[i]]
		track[i].LabelPosition, track[i].LabelPercentage = c.anchor.position, c.anchor.fraction
	}
}

// A caption beside a wire can straddle a different bend of that same wire.
func (f *tweenCaptionFrame) ownStrokeClear(edge *d2target.Connection, box image.Rectangle, pos label.Position) bool {
	if pos.IsOnEdge() {
		return true
	}
	for i := range f.wires {
		wire := &f.wires[i]
		if wire.edge == edge {
			stroke, _ := wire.ink.overlap(box, -1)
			return stroke == 0
		}
	}
	return true
}

// Find the closest rendered caption on each side of the wire. Sampling the
// midpoint of a straight segment gives its exact label offset, including the
// renderer's normal convention and text dimensions.
func projectEdgeCaption(e *d2target.Connection, target geo.Point, add func(edgeCaptionAnchor)) {
	length := geo.Route(e.Route).Length()
	if length <= 0 {
		return
	}
	for _, pos := range edgeCaptionSides(e) {
		travel, best := 0., math.Inf(1)
		var anchor edgeCaptionAnchor
		for i, a := range e.Route[:len(e.Route)-1] {
			b := e.Route[i+1]
			dx, dy := b.X-a.X, b.Y-a.Y
			run := math.Hypot(dx, dy)
			if run <= .01 {
				travel += run
				continue
			}
			probe := *e
			probe.Route = e.Route[i : i+2]
			probe.LabelPosition, probe.LabelPercentage = pos.String(), .5
			p := probe.GetLabelTopLeft()
			t := max(.001, min(.999, .5+((target.X-p.X)*dx+(target.Y-p.Y)*dy)/(run*run)))
			point := geo.Point{X: p.X + (t-.5)*dx, Y: p.Y + (t-.5)*dy}
			if d := captionDistance2(point, target); d < best {
				best, anchor = d, edgeCaptionAnchor{probe.LabelPosition, (travel + t*run) / length}
			}
			travel += run
		}
		if !math.IsInf(best, 1) {
			add(anchor)
		}
	}
}

func edgeCaptionSides(e *d2target.Connection) []label.Position {
	if label.FromString(e.LabelPosition).IsOnEdge() {
		return []label.Position{label.UnlockedMiddle}
	}
	return []label.Position{label.UnlockedTop, label.UnlockedBottom}
}

// Select one already-clear pose for each sample. Charge squared distance at
// the visibility shared by adjacent poses. A hidden caption's change of anchor
// costs nothing. The ordinary pose is always first, wins ties, and receives a
// modest preference so small improvements do not churn native geometry.
// Both held samples have exactly one choice.
func chooseCaptionAnchors(choices [][]edgeCaptionChoice, track []*d2target.Connection) []int {
	parents := make([][]int, len(choices))
	previous := []float64{0}
	for i := 1; i < len(choices); i++ {
		visible := min(track[i-1].Opacity, track[i].Opacity)
		current := make([]float64, len(choices[i]))
		parents[i] = make([]int, len(current))
		for j, c := range choices[i] {
			best := math.Inf(1)
			for k, p := range choices[i-1] {
				if cost := previous[k] + visible*captionDistance2(c.point, p.point); cost < best {
					best, parents[i][j] = cost, k
				}
			}
			current[j] = best + .1*track[i].Opacity*captionDistance2(c.point, choices[i][0].point)
		}
		previous = current
	}
	path := make([]int, len(choices))
	for i := len(path) - 1; i > 0; i-- {
		path[i-1] = parents[i][path[i]]
	}
	return path
}
