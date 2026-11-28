package d2lib

import (
	"math"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

const gridFoldFade = .28 // Fraction of a step reserved for revealing or hiding details.

// A rectangle that survives both boards can change from a summary cell to a
// grid of details. Keep the summary in place and use its growing interior as
// the children's viewport. Closing is exactly the same reveal played backwards.
type gridFold struct {
	closed, open *d2target.Shape
	opening      bool
}

type gridFoldDetail struct {
	fold        *gridFold
	endpoint    *d2target.Shape
	revealStart float64
}

func (m *gridMotion) planGridFolds(old, next map[string]*gridNode) {
	m.foldEnd = 1
	for id, a := range old {
		b := next[id]
		if b == nil || !a.gridCell || !b.gridCell || a.shape.Type != d2target.ShapeRectangle || b.shape.Type != d2target.ShapeRectangle {
			continue
		}
		closed, open := a, b
		opening := len(a.children) == 0
		if !opening {
			closed, open = b, a
		}
		if len(closed.children) != 0 || !open.isGrid || len(open.children) == 0 ||
			(open.shape.Width <= closed.shape.Width && open.shape.Height <= closed.shape.Height) {
			continue
		}
		if m.folds == nil {
			m.folds = make(map[string]*gridFold)
			m.details = make(map[string]gridFoldDetail)
		}
		m.folds[id] = &gridFold{closed: closed.shape, open: open.shape, opening: opening}
		if opening {
			m.foldEnd = 1 - gridFoldFade
		} else {
			m.foldStart = gridFoldFade
		}
	}

	// The shell and its neighbors share one layout clock. When a step closes
	// one cell and opens another, fade the old details, reflow, then reveal.
	for id, f := range m.folds {
		open := next[id]
		start, end := m.foldStart, m.foldEnd
		if !f.opening {
			open = old[id]
			start, end = 1-end, 1-start
		}
		var visit func(*gridNode, float64)
		visit = func(n *gridNode, revealStart float64) {
			m.details[n.shape.ID] = gridFoldDetail{fold: f, endpoint: n.shape, revealStart: revealStart}
			for _, child := range n.children {
				visit(child, revealStart)
			}
		}
		for _, child := range open.children {
			// Use the child's actual slot, so cards in the same row appear
			// together and deeper rows wait until the shell can contain them.
			s := child.shape
			pad := (s.StrokeWidth + f.open.StrokeWidth + 2) / 2
			required := func(closed, expanded, farEdge int) float64 {
				if expanded <= closed {
					return 0
				}
				return gridPhase(float64(farEdge+pad), float64(closed), float64(expanded))
			}
			fit := max(required(f.closed.Width, f.open.Width, s.Pos.X-f.open.Pos.X+s.Width),
				required(f.closed.Height, f.open.Height, s.Pos.Y-f.open.Pos.Y+s.Height))
			// From its midpoint onward ease-in-out is at least its input.
			// At this time the entire card fits, including its stroke.
			visit(child, start+(end-start)*max(.5, fit))
		}
	}
}

func (f *gridFold) progress(fraction float64) float64 {
	if !f.opening {
		return 1 - fraction
	}
	return fraction
}

// Only interpolate plain, inside captions whose text persists. Border labels,
// rich content and arbitrary label edits keep their usual rendering rules.
func (f *gridFold) caption(s *d2target.Shape, t float64) {
	if f.open.Label == "" || f.open.Label != f.closed.Label || f.open.Language != "" || f.closed.Language != "" ||
		f.open.LabelPosition == f.closed.LabelPosition || s.Icon != nil {
		return
	}
	a, b := label.FromString(f.closed.LabelPosition), label.FromString(f.open.LabelPosition)
	if a.IsOutside() || b.IsOutside() || a.IsBorder() || b.IsBorder() || !a.IsShapePosition() || !b.IsShapePosition() {
		return
	}
	point := func(s *d2target.Shape, p label.Position) *geo.Point {
		box := geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
		return p.GetPointOnBox(box, label.PADDING, float64(s.LabelWidth), float64(s.LabelHeight))
	}
	start, end := point(f.closed, a), point(f.open, b)
	t = f.progress(t)
	if t == 0 {
		s.LabelPosition = f.closed.LabelPosition
		return
	}
	if t == 1 {
		s.LabelPosition = f.open.LabelPosition
		return
	}
	s.MotionLabelTopLeft = &d2target.Point{X: int(math.Round(start.X + (end.X-start.X)*t)), Y: int(math.Round(start.Y + (end.Y-start.Y)*t))}
}

func (d gridFoldDetail) sample(s d2target.Shape, fraction, travel float64) (d2target.Shape, float64) {
	f := d.fold
	visible := gridEase(gridPhase(f.progress(fraction), d.revealStart, d.revealStart+gridFoldFade))
	if visible == 0 {
		return s, 0
	}

	// A sampled paint frame can have its own intervening grid layout. Use its
	// paint, but register all the content to the authored open board's slots.
	t := f.progress(travel)
	pos := d2target.Point{X: gridLerpInt(f.closed.Pos.X, f.open.Pos.X, t), Y: gridLerpInt(f.closed.Pos.Y, f.open.Pos.Y, t)}
	s.Pos = d2target.Point{X: pos.X + (d.endpoint.Pos.X - f.open.Pos.X), Y: pos.Y + (d.endpoint.Pos.Y - f.open.Pos.Y)}
	s.Width, s.Height = d.endpoint.Width, d.endpoint.Height
	s.Opacity *= visible
	if t < 1 {
		inset := (max(f.closed.StrokeWidth, f.open.StrokeWidth) + 1) / 2
		w := gridLerpInt(f.closed.Width, f.open.Width, t) - 2*inset
		h := gridLerpInt(f.closed.Height, f.open.Height, t) - 2*inset
		if w <= 0 || h <= 0 {
			return s, 0
		}
		s.MotionClip = &d2target.MotionClip{Point: d2target.Point{X: pos.X + inset, Y: pos.Y + inset}, Width: w, Height: h}
	}
	return s, visible
}
