package d2lib

import (
	"math"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// A rectangle that survives both boards can change from a summary cell to a
// grid of details. Keep the summary in place and use its growing interior as
// the children's viewport. Closing is exactly the same reveal played backwards.
type gridFold struct {
	closed, open *d2target.Shape
	opening      bool
	horizontal   bool
	detailStart  int
}

type gridFoldDetail struct {
	fold *gridFold
	rank float64
}

func planGridFolds(old, next map[string]*gridNode) (map[string]*gridFold, map[string]gridFoldDetail) {
	var parents map[string]*gridFold
	var details map[string]gridFoldDetail
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
		f := &gridFold{closed: closed.shape, open: open.shape, opening: opening,
			horizontal: open.shape.Width-closed.shape.Width > open.shape.Height-closed.shape.Height}
		if parents == nil {
			parents = make(map[string]*gridFold)
			details = make(map[string]gridFoldDetail)
		}
		parents[id] = f
		var visit func(*gridNode, gridFoldDetail)
		visit = func(n *gridNode, detail gridFoldDetail) {
			details[n.shape.ID] = detail
			for _, child := range n.children {
				visit(child, detail)
			}
		}
		// Uncover details from the old edge towards the first row or column.
		f.detailStart = math.MaxInt
		for i, child := range open.children {
			c := child.shape
			start := c.Pos.Y - open.shape.Pos.Y
			if f.horizontal {
				start = c.Pos.X - open.shape.Pos.X
			}
			f.detailStart = min(f.detailStart, start-(c.StrokeWidth+1)/2-2)
			visit(child, gridFoldDetail{f, float64(i) / float64(max(1, len(open.children)-1))})
		}
	}
	return parents, details
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
	t = gridEase(f.progress(t))
	s.MotionLabelTopLeft = &d2target.Point{X: int(math.Round(start.X + (end.X-start.X)*t)), Y: int(math.Round(start.Y + (end.Y-start.Y)*t))}
}

func (d gridFoldDetail) sample(s d2target.Shape, fraction float64) (d2target.Shape, float64) {
	f := d.fold
	progress := f.progress(fraction)
	if progress >= 1 {
		return s, 1
	}
	visible := gridEase(gridPhase(progress, .18+.14*d.rank, .78+.14*d.rank))
	if visible == 0 {
		return s, 0
	}
	t := gridEase(progress)
	pos := d2target.Point{X: gridLerpInt(f.closed.Pos.X, f.open.Pos.X, t), Y: gridLerpInt(f.closed.Pos.Y, f.open.Pos.Y, t)}
	w, h := gridLerpInt(f.closed.Width, f.open.Width, t), gridLerpInt(f.closed.Height, f.open.Height, t)
	inset := (max(f.closed.StrokeWidth, f.open.StrokeWidth) + 1) / 2
	left, top := inset, inset
	if f.horizontal {
		left = max(inset, gridLerpInt(f.closed.Width, f.detailStart, t))
	} else {
		top = max(inset, gridLerpInt(f.closed.Height, f.detailStart, t))
	}
	if w <= left+inset || h <= top+inset {
		return s, 0
	}
	s.MotionClip = &d2target.MotionClip{Point: d2target.Point{X: pos.X + left, Y: pos.Y + top}, Width: w - left - inset, Height: h - top - inset}
	s.Pos.X += pos.X - f.open.Pos.X
	s.Pos.Y += pos.Y - f.open.Pos.Y
	offset := gridLerpInt(-10, 0, visible)
	if f.horizontal {
		s.Pos.X += offset
	} else {
		s.Pos.Y += offset
	}
	s.Opacity *= visible
	return s, visible
}
