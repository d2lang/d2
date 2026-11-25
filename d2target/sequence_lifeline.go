package d2target

import (
	"math"
	"sort"

	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// SequenceLabelGap clears a lifeline beneath a caption in diagram coordinates.
type SequenceLabelGap struct {
	geo.Box
	Opacity float64
}

// SequenceLifelineLabelGaps returns stroke masks keyed by connection index. Keeping
// the original route intact preserves the phase of its dashes and its animation.
func (d *Diagram) SequenceLifelineLabelGaps(sketch bool, paintVisible func(string) bool) map[int][]SequenceLabelGap {
	if paintVisible == nil {
		paintVisible = color.PaintVisibleThroughout
	}
	type segment struct {
		edge                           int
		top, bottom, maxBottom, radius float64
	}
	columns := make(map[float64][]segment)
	var xs []float64
	var maxRadius float64
	for i := range d.Connections {
		c := &d.Connections[i]
		if c.SequenceRole != "lifeline" || c.Opacity <= 0 || len(c.Route) != 2 || c.RenderedStrokeWidth() <= 0 || !paintVisible(c.Stroke) {
			continue
		}
		a, b := c.Route[0], c.Route[1]
		if a == nil || b == nil || a.X != b.X {
			continue
		}
		if _, ok := columns[a.X]; !ok {
			xs = append(xs, a.X)
		}
		radius := c.RenderedStrokeWidth() / 2
		if sketch {
			radius += sketchLineDeviation(math.Abs(b.Y-a.Y), c.Animated)
		}
		maxRadius = math.Max(maxRadius, radius)
		columns[a.X] = append(columns[a.X], segment{edge: i, top: math.Min(a.Y, b.Y), bottom: math.Max(a.Y, b.Y), radius: radius})
	}
	if len(xs) == 0 {
		return nil
	}
	sort.Float64s(xs)
	// Notes can split a single actor's lifeline into many disjoint segments.
	// Search by both column and row, including coincident columns on nested boards.
	for _, segments := range columns {
		sort.Slice(segments, func(i, j int) bool { return segments[i].top < segments[j].top })
		bottom := segments[0].bottom
		for i := range segments {
			bottom = math.Max(bottom, segments[i].bottom)
			segments[i].maxBottom = bottom
		}
	}
	gaps := make(map[int][]SequenceLabelGap)
	add := func(tl *geo.Point, text *Text, opacity float64, clip *MotionClip) {
		if tl == nil || text.LabelWidth <= 0 || text.LabelHeight <= 0 {
			return
		}
		const padding = 2
		left, top := tl.X-padding, tl.Y-padding
		right, bottom := tl.X+float64(text.LabelWidth)+padding, tl.Y+float64(text.LabelHeight)+padding
		if clip != nil {
			left, top = math.Max(left, float64(clip.X)), math.Max(top, float64(clip.Y))
			right, bottom = math.Min(right, float64(clip.X+clip.Width)), math.Min(bottom, float64(clip.Y+clip.Height))
			if left >= right || top >= bottom {
				return
			}
		}
		gap := SequenceLabelGap{Box: *geo.NewBox(geo.NewPoint(left, top), right-left, bottom-top), Opacity: opacity}
		for _, x := range xs[sort.SearchFloat64s(xs, left-maxRadius):] {
			if x-maxRadius >= right {
				break
			}
			segments := columns[x]
			start := sort.Search(len(segments), func(i int) bool { return segments[i].maxBottom > top })
			for _, s := range segments[start:] {
				if s.top >= bottom {
					break
				}
				if s.bottom > top && x+s.radius > left && x-s.radius < right {
					gaps[s.edge] = append(gaps[s.edge], gap)
				}
			}
		}
	}
	for i := range d.Shapes {
		s := &d.Shapes[i]
		if s.SequenceRole == "" || s.Label == "" || s.Opacity <= 0 || !paintVisible(s.GetFontColor()) {
			continue
		}
		position := label.FromString(s.LabelPosition)
		if position == label.Unset && (s.Language == "markdown" || s.Language == "latex") {
			position = label.InsideMiddleCenter
		}
		box := s.LabelBox(position, s.Geometry())
		add(s.LabelTopLeft(position, box), &s.Text, s.Opacity, s.MotionClip)
	}
	for i := range d.Connections {
		c := &d.Connections[i]
		if c.SequenceRole != "message" || c.Opacity <= 0 || len(c.Route) < 2 {
			continue
		}
		opacity := c.Opacity
		if c.MotionLabel != nil {
			opacity *= c.MotionLabel.Opacity
		}
		if c.Label != "" && opacity > 0 && paintVisible(c.GetFontColor()) {
			if tl := c.GetLabelTopLeft(); tl != nil {
				add(geo.NewPoint(math.Round(tl.X), math.Round(tl.Y)), &c.Text, opacity, c.MotionClip)
			}
		}
		for end, t := range []*Text{c.SrcLabel, c.DstLabel} {
			if t != nil && t.Label != "" && (t.Color == "" || paintVisible(t.Color)) {
				add(c.GetArrowheadLabelPosition(end == 1), t, c.Opacity, c.MotionClip)
			}
		}
	}
	return gaps
}
