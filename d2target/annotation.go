package d2target

import (
	"hash/fnv"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/d2lang/d2/lib/geo"
)

// The marks an annotation can draw. A trace follows a connection; the others
// surround a shape, and a cross can strike out either.
const (
	AnnotationCircle    = "circle"
	AnnotationBox       = "box"
	AnnotationUnderline = "underline"
	AnnotationBrace     = "brace"
	AnnotationCross     = "cross"
	AnnotationTrace     = "trace"
	AnnotationHighlight = "highlight"
	AnnotationArrow     = "arrow"
	AnnotationLasso     = "lasso"
)

const (
	// DefaultAnnotationStroke is a pen color that stays legible over both
	// light and dark themes, the way a marker stands apart from the page.
	DefaultAnnotationStroke = "#E8590C"
	AnnotationStrokeWidth   = 3
	// A trace is a highlighter: wider than the connection it follows, and
	// translucent so the connection reads through it.
	annotationTraceExtraWidth = 8
	AnnotationTraceOpacity    = 0.38

	// DefaultHighlightStroke is the yellow of a highlighter. A label reads
	// through it at AnnotationHighlightOpacity.
	DefaultHighlightStroke     = "#FFD43B"
	AnnotationHighlightOpacity = 0.45
	// annotationArrowReach is how far from its target an arrow's note stands.
	annotationArrowReach = 36

	annotationLabelGap = 8
	// CaptionGap separates a board's caption from the lowest ink above it.
	CaptionGap = 28

	// AnnotationFontSize is the size of the note beside a mark, and
	// CaptionFontSize the size of a board's caption.
	AnnotationFontSize = 16
	CaptionFontSize    = 24
)

// Annotation is a mark and a short Markdown note drawn over a shape or a
// connection for the length of its board. On a diagram's root it is the
// board's caption. It takes no part in layout.
type Annotation struct {
	Label       string `json:"label,omitempty"`
	LabelWidth  int    `json:"labelWidth,omitempty"`
	LabelHeight int    `json:"labelHeight,omitempty"`
	// Shape is the resolved mark. A caption has none.
	Shape string `json:"shape,omitempty"`
	// Position is an authored side for the note. Empty picks the side with
	// the most room on each board.
	Position string `json:"position,omitempty"`
	Stroke   string `json:"stroke,omitempty"`
	// FontColor is an authored color for the text alone. Empty, the note is
	// written in the pen.
	FontColor string `json:"fontColor,omitempty"`
	// Members are the IDs of the other shapes a lasso holds, besides the one
	// that carries it.
	Members []string `json:"members,omitempty"`
	// Cues are the IDs of the shapes a caption names, in reading order.
	Cues []string `json:"cues,omitempty"`
	// Anchor pins a caption's top center. Animations set it so that every
	// frame shares one lower third. Unset, the caption follows its own board.
	Anchor *Point `json:"anchor,omitempty"`

	// Drawn is the part of the mark a sampled frame shows, as fractions of
	// its stroke length. Nil is the whole mark.
	Drawn *[2]float64 `json:"drawn,omitempty"`
}

// GetStroke returns the paint of the mark: the authored pen, or the default
// for the kind of mark it is.
func (a *Annotation) GetStroke() string {
	if a.Stroke != "" {
		return a.Stroke
	}
	if a.Shape == AnnotationHighlight {
		return DefaultHighlightStroke
	}
	return DefaultAnnotationStroke
}

// notePaint returns the paint of the note. It is written in the pen that
// drew the mark, except beside a highlight: a highlighter is too pale to
// write with, so that note is in the default pen.
func (a *Annotation) notePaint() string {
	if a.FontColor != "" {
		return a.FontColor
	}
	if a.Shape == AnnotationHighlight {
		return DefaultAnnotationStroke
	}
	return a.GetStroke()
}

// CaptionPaint returns the paint for a caption's text. A caption is set in
// the board's own text color unless its author gave it a color or a pen.
func (a *Annotation) CaptionPaint(text string) string {
	if a.FontColor != "" {
		return a.FontColor
	}
	if a.Stroke != "" {
		return a.Stroke
	}
	return text
}

func (a *Annotation) drawn() (from, to float64) {
	if a.Drawn == nil {
		return 0, 1
	}
	return a.Drawn[0], a.Drawn[1]
}

// noteOpacity brings the note in as the pen finishes and takes it out as
// soon as the eraser starts.
func (a *Annotation) noteOpacity() float64 {
	from, to := a.drawn()
	return clampUnit((to-0.5)/0.4) * (1 - clampUnit(from*3))
}

// CaptionOpacity cross-fades one board's caption into the next.
func (a *Annotation) CaptionOpacity() float64 {
	from, to := a.drawn()
	return clampUnit(to) * (1 - clampUnit(from))
}

func clampUnit(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// dist is the length of (dx, dy). math.Hypot can differ in its last bit from
// one architecture to the next, and these lengths feed comparisons.
func dist(dx, dy float64) float64 {
	return math.Sqrt(float64(dx*dx) + float64(dy*dy))
}

// AnnotationMark is one annotation resolved to ink for a board: polylines in
// diagram coordinates and, when it has a note, where the note goes.
type AnnotationMark struct {
	// ID names the mark after its target, for element IDs.
	ID      string
	Strokes [][]geo.Point
	Paint   string
	Width   float64
	Opacity float64
	// Flat ends the strokes square, as a highlighter does. Other marks end round.
	Flat bool

	Label string
	// NotePaint is the paint of the note, which can differ from the mark's.
	// The note is set in the board's font, which is the font it was measured in.
	NotePaint    string
	LabelBox     *geo.Box
	LabelOpacity float64
}

// Bounds returns the extent of the ink, including half its stroke width. A
// mark with no ink has no extent.
func (m AnnotationMark) Bounds() (x1, y1, x2, y2 float64) {
	points := 0
	for _, stroke := range m.Strokes {
		points += len(stroke)
	}
	if points == 0 {
		return 0, 0, 0, 0
	}
	x1, y1 = math.Inf(1), math.Inf(1)
	x2, y2 = math.Inf(-1), math.Inf(-1)
	for _, stroke := range m.Strokes {
		for _, p := range stroke {
			x1, y1 = math.Min(x1, p.X), math.Min(y1, p.Y)
			x2, y2 = math.Max(x2, p.X), math.Max(y2, p.Y)
		}
	}
	half := m.Width / 2
	return x1 - half, y1 - half, x2 + half, y2 + half
}

// AnnotationMarks resolves every annotation on the board, in drawing order.
// A sampled frame can carry the previous board's marks while they are erased.
func (diagram *Diagram) AnnotationMarks() []AnnotationMark {
	return diagram.annotationMarks(true)
}

// annotationMarks optionally leaves each mark whole, which is what the
// viewport reserves room for however much of it a frame has drawn.
func (diagram *Diagram) annotationMarks(cut bool) []AnnotationMark {
	has := false
	for i := range diagram.Shapes {
		if diagram.Shapes[i].Annotation != nil || diagram.Shapes[i].LeavingAnnotation != nil {
			has = true
			break
		}
	}
	for i := range diagram.Connections {
		if has {
			break
		}
		has = diagram.Connections[i].Annotation != nil || diagram.Connections[i].LeavingAnnotation != nil
	}
	if !has {
		return nil
	}

	// A note is placed against the whole of its mark, before the mark is cut
	// for a sampled frame, so that it holds still while the pen moves. All
	// the ink is laid down first: a note also keeps clear of other marks.
	type pending struct {
		annotation *Annotation
		// anchor is what the note sits beside, and target what it describes.
		anchor, target *geo.Box
		sides          []string
		// aim is what an arrow points at. Its strokes start at the note, so
		// they are drawn once the note has a place.
		aim  *geo.Box
		seed float64
		// leaving marks the previous board's annotation while it is erased.
		leaving bool
	}
	var marks []AnnotationMark
	var notes []pending
	add := func(m AnnotationMark, a *Annotation, leaving bool, anchor, target, aim *geo.Box, sides []string) {
		seed := markSeed(m.ID)
		if leaving {
			m.ID += ":leaving"
		}
		if a.Position != "" && a.Shape != AnnotationBrace {
			sides = []string{a.Position}
		}
		m.NotePaint = a.notePaint()
		marks = append(marks, m)
		notes = append(notes, pending{a, anchor, target, sides, aim, seed, leaving})
	}
	// An arrow reads best when it comes in at an angle.
	arrowSides := []string{
		"top-right", "top-left", "bottom-right", "bottom-left",
		"center-right", "top-center", "bottom-center", "center-left",
	}
	var boxes map[string]*geo.Box
	shapeBox := func(id string) *geo.Box {
		if boxes == nil {
			boxes = make(map[string]*geo.Box, len(diagram.Shapes))
			for i := range diagram.Shapes {
				s := &diagram.Shapes[i]
				boxes[s.ID] = geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
			}
		}
		return boxes[id]
	}

	for i := range diagram.Shapes {
		s := &diagram.Shapes[i]
		for j, a := range []*Annotation{s.LeavingAnnotation, s.Annotation} {
			if a == nil {
				continue
			}
			box := geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
			m := AnnotationMark{ID: s.ID, Paint: a.GetStroke(), Width: AnnotationStrokeWidth, Opacity: 1}
			var aim *geo.Box
			switch a.Shape {
			case AnnotationHighlight:
				band, lines := shapeHighlightBand(s, box)
				m.Strokes, m.Width = highlightStrokes(band, lines, markSeed(s.ID))
				m.Opacity, m.Flat = AnnotationHighlightOpacity, true
			case AnnotationLasso:
				ids := append([]string{s.ID}, a.Members...)
				held := []*geo.Box{box}
				for _, id := range a.Members {
					if other := shapeBox(id); other != nil && id != s.ID {
						held = append(held, other)
					}
				}
				// Every other shape is something to go round, apart from what
				// is inside a held shape and the containers a held shape is in.
				var others []*geo.Box
				for k := range diagram.Shapes {
					other := diagram.Shapes[k].ID
					related := false
					for _, id := range ids {
						if other == id || strings.HasPrefix(other, id+".") || strings.HasPrefix(id, other+".") {
							related = true
							break
						}
					}
					if !related {
						others = append(others, shapeBox(other))
					}
				}
				m.Strokes = [][]geo.Point{lassoStroke(held, others, markSeed(s.ID))}
			case AnnotationArrow:
				aim = box
			default:
				m.Strokes = shapeMarkStrokes(s, a, box)
			}
			// Try the four sides first, then above and below flush with an
			// edge, then the corners. The first with nothing in the way wins.
			sides := []string{
				"center-right", "top-center", "bottom-center", "center-left",
				"top-start", "top-end", "bottom-start", "bottom-end",
				"top-left", "top-right", "bottom-left", "bottom-right",
			}
			switch a.Shape {
			case AnnotationBrace:
				// A brace's note belongs under its point. It can slide to
				// either end of the brace when something is in the way.
				sides = []string{braceSide(a.Position)}
				switch sides[0] {
				case "bottom-center":
					sides = append(sides, "bottom-start", "bottom-end")
				case "top-center":
					sides = append(sides, "top-start", "top-end")
				}
			case AnnotationUnderline:
				sides = []string{"bottom-center", "center-right", "top-center", "center-left"}
			case AnnotationArrow:
				sides = arrowSides
			}
			x1, y1, x2, y2 := m.Bounds()
			anchor := geo.NewBox(geo.NewPoint(x1, y1), x2-x1, y2-y1)
			switch a.Shape {
			case AnnotationArrow:
				anchor = growBox(box, annotationArrowReach)
			case AnnotationHighlight:
				// The note goes beside the shape, not beside the label inside it.
				anchor = unionBox(anchor, box)
			}
			add(m, a, j == 0, anchor, box, aim, sides)
		}
	}
	for i := range diagram.Connections {
		c := &diagram.Connections[i]
		for j, a := range []*Annotation{c.LeavingAnnotation, c.Annotation} {
			if a == nil || len(c.Route) < 2 {
				continue
			}
			m := AnnotationMark{ID: c.ID, Paint: a.GetStroke(), Width: AnnotationStrokeWidth, Opacity: 1}
			route := flattenRoute(c)
			mid := pointAlong(route, 0.5)
			// The note sits beside the middle of the route, not beside all of it.
			var anchor, aim *geo.Box
			sides := []string{
				"top-center", "center-right", "bottom-center", "center-left",
				"top-right", "bottom-right", "top-left", "bottom-left",
			}
			band := connectionLabelBox(c)
			switch {
			case a.Shape == AnnotationCross:
				m.Strokes = routeCrossStrokes(route, markSeed(c.ID))
				x1, y1, x2, y2 := m.Bounds()
				anchor = geo.NewBox(geo.NewPoint(x1, y1), x2-x1, y2-y1)
			case a.Shape == AnnotationArrow:
				aim = geo.NewBox(geo.NewPoint(mid.X-1, mid.Y-1), 2, 2)
				anchor = growBox(aim, annotationArrowReach)
				sides = arrowSides
			case a.Shape == AnnotationHighlight && band != nil:
				m.Strokes, m.Width = highlightStrokes(band, 1+strings.Count(c.Label, "\n"), markSeed(c.ID))
				m.Opacity, m.Flat = AnnotationHighlightOpacity, true
				x1, y1, x2, y2 := m.Bounds()
				anchor = geo.NewBox(geo.NewPoint(x1, y1), x2-x1, y2-y1)
			default:
				m.Width = c.RenderedStrokeWidth() + annotationTraceExtraWidth
				m.Opacity = AnnotationTraceOpacity
				m.Strokes = [][]geo.Point{route}
				anchor = geo.NewBox(geo.NewPoint(mid.X-m.Width/2, mid.Y-m.Width/2), m.Width, m.Width)
			}
			add(m, a, j == 0, anchor, nil, aim, sides)
		}
	}

	// What is being erased and what is being drawn are placed apart, each as
	// it sits on its own board. Otherwise a note would reserve room beside the
	// one it replaces, at a place it never appears.
	var placed [2][]*geo.Box
	inkBoxes := make([]*geo.Box, len(marks))
	inkBox := func(j int) *geo.Box {
		if inkBoxes[j] == nil {
			x1, y1, x2, y2 := marks[j].Bounds()
			inkBoxes[j] = geo.NewBox(geo.NewPoint(x1, y1), x2-x1, y2-y1)
		}
		return inkBoxes[j]
	}
	for i, note := range notes {
		a := note.annotation
		generation := 0
		if note.leaving {
			generation = 1
		}
		w, h := float64(a.LabelWidth), float64(a.LabelHeight)
		labelled := a.Label != "" && w > 0 && h > 0
		if !labelled {
			if note.aim == nil {
				continue
			}
			// An arrow with nothing to say still has to come from somewhere.
			w, h = 0, 0
		}
		best, bestScore := (*geo.Box)(nil), math.Inf(1)
		for _, side := range note.sides {
			box := noteBox(note.anchor, side, w, h)
			score := diagram.noteCollisions(box, note.target, placed[generation])
			for j := range marks {
				if j != i && notes[j].leaving == note.leaving && overlapArea(inkBox(j), box) > 0 {
					score += inkInside(marks[j].Strokes, box)
				}
			}
			if score < bestScore {
				best, bestScore = box, score
			}
			if score == 0 {
				// Nothing in the way: no later side can do better.
				break
			}
		}
		if labelled {
			marks[i].Label, marks[i].LabelBox, marks[i].LabelOpacity = a.Label, best, a.noteOpacity()
			placed[generation] = append(placed[generation], best)
		}
		if note.aim != nil {
			marks[i].Strokes = arrowStrokes(best, note.aim, note.seed)
			inkBoxes[i] = nil
		}
	}
	if cut {
		for i, note := range notes {
			marks[i].Strokes = cutStrokes(marks[i].Strokes, note.annotation)
		}
	}
	return marks
}

func braceSide(position string) string {
	switch position {
	case "top-center", "center-left", "center-right":
		return position
	}
	return "bottom-center"
}

// noteBox places a w by h note against the anchor: centered on a side, above
// or below it flush with its start or end, or diagonally off a corner.
func noteBox(anchor *geo.Box, side string, w, h float64) *geo.Box {
	left, top := anchor.TopLeft.X, anchor.TopLeft.Y
	right, bottom := left+anchor.Width, top+anchor.Height
	x, y := left+anchor.Width/2-w/2, top+anchor.Height/2-h/2
	switch side {
	case "top-center", "top-left", "top-right", "top-start", "top-end":
		y = top - annotationLabelGap - h
	case "bottom-center", "bottom-left", "bottom-right", "bottom-start", "bottom-end":
		y = bottom + annotationLabelGap
	}
	switch side {
	case "center-left", "top-left", "bottom-left":
		x = left - annotationLabelGap - w
	case "center-right", "top-right", "bottom-right":
		x = right + annotationLabelGap
	case "top-start", "bottom-start":
		x = left
	case "top-end", "bottom-end":
		x = right - w
	}
	return geo.NewBox(geo.NewPoint(math.Round(x), math.Round(y)), w, h)
}

func overlapArea(a, b *geo.Box) float64 {
	w := math.Min(a.TopLeft.X+a.Width, b.TopLeft.X+b.Width) - math.Max(a.TopLeft.X, b.TopLeft.X)
	h := math.Min(a.TopLeft.Y+a.Height, b.TopLeft.Y+b.Height) - math.Max(a.TopLeft.Y, b.TopLeft.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

func boxContains(outer, inner *geo.Box) bool {
	return outer.TopLeft.X <= inner.TopLeft.X && outer.TopLeft.Y <= inner.TopLeft.Y &&
		outer.TopLeft.X+outer.Width >= inner.TopLeft.X+inner.Width &&
		outer.TopLeft.Y+outer.Height >= inner.TopLeft.Y+inner.Height
}

// lineInside scores a polyline by how much of it runs through the box,
// sampling it every few pixels.
func lineInside[P geo.Point | *geo.Point](pts []P, box *geo.Box, get func(P) geo.Point) float64 {
	const (
		spacing = 8.
		weight  = 90.
	)
	score := 0.
	for j := 0; j+1 < len(pts); j++ {
		a, b := get(pts[j]), get(pts[j+1])
		steps := int(math.Max(1, dist(b.X-a.X, b.Y-a.Y)/spacing))
		for k := 0; k <= steps; k++ {
			t := float64(k) / float64(steps)
			x, y := a.X+(b.X-a.X)*t, a.Y+(b.Y-a.Y)*t
			if x >= box.TopLeft.X && x <= box.TopLeft.X+box.Width && y >= box.TopLeft.Y && y <= box.TopLeft.Y+box.Height {
				score += weight
			}
		}
	}
	return score
}

func inkInside(strokes [][]geo.Point, box *geo.Box) float64 {
	score := 0.
	for _, stroke := range strokes {
		score += lineInside(stroke, box, func(p geo.Point) geo.Point { return p })
	}
	return score
}

// noteCollisions scores how much a note would cover: other shapes, the
// routes and labels of connections, and notes already placed. A container
// around the target is not an obstacle; the note is expected to sit in it.
func (diagram *Diagram) noteCollisions(note, target *geo.Box, notes []*geo.Box) float64 {
	score := 0.
	for i := range diagram.Shapes {
		s := &diagram.Shapes[i]
		box := geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
		if target != nil && boxContains(box, target) {
			continue
		}
		score += overlapArea(note, box)
	}
	for i := range diagram.Connections {
		c := &diagram.Connections[i]
		if routeNear(c.Route, note) {
			score += lineInside(c.Route, note, func(p *geo.Point) geo.Point { return *p })
		}
		if c.Label != "" {
			if tl := c.GetLabelTopLeft(); tl != nil {
				score += 4 * overlapArea(note, geo.NewBox(tl, float64(c.LabelWidth), float64(c.LabelHeight)))
			}
		}
	}
	for _, other := range notes {
		score += 8 * overlapArea(note, other)
	}
	return score
}

// routeNear reports whether the route's bounding box reaches the box, which
// is a cheap way to skip routes that cannot pass through it.
func routeNear(route []*geo.Point, box *geo.Box) bool {
	if len(route) == 0 {
		return false
	}
	x1, y1, x2, y2 := route[0].X, route[0].Y, route[0].X, route[0].Y
	for _, p := range route[1:] {
		x1, y1 = math.Min(x1, p.X), math.Min(y1, p.Y)
		x2, y2 = math.Max(x2, p.X), math.Max(y2, p.Y)
	}
	return x1 <= box.TopLeft.X+box.Width && x2 >= box.TopLeft.X && y1 <= box.TopLeft.Y+box.Height && y2 >= box.TopLeft.Y
}

// markSeed gives every target its own slightly different hand.
func markSeed(id string) float64 {
	h := fnv.New32a()
	h.Write([]byte(id))
	return float64(h.Sum32()%6283) / 1000
}

func shapeMarkStrokes(s *Shape, a *Annotation, box *geo.Box) [][]geo.Point {
	seed := markSeed(s.ID)
	switch a.Shape {
	case AnnotationBox:
		return [][]geo.Point{boxStroke(box, seed)}
	case AnnotationUnderline:
		return [][]geo.Point{underlineStroke(s, box, seed)}
	case AnnotationBrace:
		return [][]geo.Point{braceStroke(box, braceSide(a.Position))}
	case AnnotationCross:
		return crossStrokes(box, seed)
	}
	round := s.Type == ShapeCircle || s.Type == ShapeOval
	return [][]geo.Point{ringStroke(box, round, seed)}
}

// ringStroke loops once around the box the way a hand circles a word: a
// little more than one turn, drifting outward so the ends never quite meet.
func ringStroke(box *geo.Box, round bool, seed float64) []geo.Point {
	const (
		samples = 220
		turns   = 1.07
		pad     = 6.
	)
	cx, cy := box.TopLeft.X+box.Width/2, box.TopLeft.Y+box.Height/2
	// A superellipse hugs a rectangle more closely than an ellipse through
	// its corners. 2^(1/n) is the scale at which it passes through them.
	n := 2.5
	k := math.Pow(2, 1/n)
	if round {
		n, k = 2, 1
	}
	a, b := box.Width/2*k+pad, box.Height/2*k+pad
	start := -2.25 + 0.3*math.Sin(seed)
	pts := make([]geo.Point, 0, samples+1)
	for i := 0; i <= samples; i++ {
		u := float64(i) / samples
		t := start + u*turns*2*math.Pi
		wobble := 1 + 0.016*math.Sin(1.7*t+seed) + 0.01*math.Sin(3.1*t+2*seed) + (u-0.5)*0.07
		ct, st := math.Cos(t), math.Sin(t)
		x := math.Copysign(math.Pow(math.Abs(ct), 2/n), ct)
		y := math.Copysign(math.Pow(math.Abs(st), 2/n), st)
		pts = append(pts, geo.Point{X: cx + a*wobble*x, Y: cy + b*wobble*y})
	}
	return simplify(pts)
}

// boxStroke frames the box with rounded corners and overshoots its start.
func boxStroke(box *geo.Box, seed float64) []geo.Point {
	const (
		pad    = 7.
		radius = 9.
	)
	x1, y1 := box.TopLeft.X-pad, box.TopLeft.Y-pad
	x2, y2 := box.TopLeft.X+box.Width+pad, box.TopLeft.Y+box.Height+pad
	var pts []geo.Point
	side := func(ax, ay, bx, by, nx, ny float64) {
		length := dist(bx-ax, by-ay)
		steps := int(math.Max(2, length/12))
		for i := 0; i <= steps; i++ {
			t := float64(i) / float64(steps)
			bow := 1.4 * math.Sin(math.Pi*t) * math.Sin(seed+ax*0.01+ay*0.013)
			pts = append(pts, geo.Point{X: ax + (bx-ax)*t + nx*bow, Y: ay + (by-ay)*t + ny*bow})
		}
	}
	corner := func(cx, cy, from float64) {
		for i := 1; i < 6; i++ {
			t := from + float64(i)/6*math.Pi/2
			pts = append(pts, geo.Point{X: cx + radius*math.Cos(t), Y: cy + radius*math.Sin(t)})
		}
	}
	side(x1+radius, y1, x2-radius, y1, 0, -1)
	corner(x2-radius, y1+radius, -math.Pi/2)
	side(x2, y1+radius, x2, y2-radius, 1, 0)
	corner(x2-radius, y2-radius, 0)
	side(x2-radius, y2, x1+radius, y2, 0, 1)
	corner(x1+radius, y2-radius, math.Pi/2)
	side(x1, y2-radius, x1, y1+radius, -1, 0)
	corner(x1+radius, y1+radius, math.Pi)
	// Carry on past the start, lifting slightly, the way a pen does.
	overshoot := math.Min(18, box.Width/3)
	pts = append(pts, geo.Point{X: x1 + radius + overshoot*0.5, Y: y1 - 0.8}, geo.Point{X: x1 + radius + overshoot, Y: y1 - 2})
	return simplify(pts)
}

// underlineStroke scribbles under the target's own label when the label is
// inside the shape, and under the whole shape otherwise. It is one stroke
// that goes out, comes back and goes out again, each pass a little lower
// than the last, the way a pen underlines a word it means.
func underlineStroke(s *Shape, box *geo.Box, seed float64) []geo.Point {
	x1, x2 := box.TopLeft.X, box.TopLeft.X+box.Width
	y := box.TopLeft.Y + box.Height + 7
	if labelBox, ok := shapeLabelBox(s); ok {
		tl := labelBox.TopLeft
		x1, x2 = tl.X-5, tl.X+float64(s.LabelWidth)+5
		y = tl.Y + float64(s.LabelHeight) + 2
	}
	length := x2 - x1
	const (
		// turn is the radius of each reversal, so a pass starts two of them
		// below where the last one ended.
		turn = 1.3
		// spread opens the passes away from each reversal, so the three read
		// as layers of one zigzag and not as ruled lines.
		spread = 2.8
	)
	// Two numbers between 0 and 1 that differ from target to target.
	u, v := 0.5+0.5*math.Sin(seed), 0.5+0.5*math.Cos(1.7*seed)
	// A pass climbs a little from left to right. The second stops short of
	// where the pen started, and the third trails off before the end.
	rise := math.Min(1.4, length*0.02)
	inset := float64(math.Min(8, length*0.12) * (0.5 + u))
	short := float64(math.Min(18, length*0.22) * (0.5 + v))

	pts := make([]geo.Point, 0, 64)
	pass := func(ax, ay, bx, by, phase float64) {
		span := math.Abs(bx - ax)
		steps := int(math.Max(6, span/6))
		waves := math.Max(1, math.Round(span/90))
		for i := 0; i <= steps; i++ {
			t := float64(i) / float64(steps)
			// The hand wavers along a pass and is steady where it turns.
			waver := float64(0.6 * math.Sin(math.Pi*t) * math.Sin(math.Pi*waves*t+phase))
			pts = append(pts, geo.Point{X: ax + float64((bx-ax)*t), Y: ay + float64((by-ay)*t) + waver})
		}
	}
	reverse := func(x, y, side float64) {
		for i := 1; i < 6; i++ {
			a := -math.Pi/2 + float64(i)/6*math.Pi
			pts = append(pts, geo.Point{X: x + float64(side*turn*math.Cos(a)), Y: y + turn + float64(turn*math.Sin(a))})
		}
	}
	left := x1 + inset
	second := y + 2*turn + spread
	third := second + 2*turn
	pass(x1, y, x2, y-rise, seed)
	reverse(x2, y-rise, 1)
	pass(x2, y-rise+2*turn, left, second, seed+2)
	reverse(left, second, -1)
	pass(left, third, x2-short, third-rise, seed+4)
	return simplify(pts)
}

// braceStroke spans one side of the box with a curly brace whose point faces
// away from it.
func braceStroke(box *geo.Box, side string) []geo.Point {
	const (
		gap   = 8.
		depth = 11.
	)
	// Build a brace along the positive x axis that opens toward negative y,
	// then turn it to the requested side.
	length := box.Width
	if side == "center-left" || side == "center-right" {
		length = box.Height
	}
	d := math.Min(depth, length/4)
	mid := length / 2
	var local []geo.Point
	quad := func(x0, y0, cx, cy, x1, y1 float64) {
		for i := 1; i <= 10; i++ {
			t := float64(i) / 10
			mt := 1 - t
			local = append(local, geo.Point{X: mt*mt*x0 + 2*mt*t*cx + t*t*x1, Y: mt*mt*y0 + 2*mt*t*cy + t*t*y1})
		}
	}
	local = append(local, geo.Point{X: 0, Y: 0})
	quad(0, 0, 0, d, d, d)
	local = append(local, geo.Point{X: mid - d, Y: d})
	quad(mid-d, d, mid, d, mid, 2*d)
	quad(mid, 2*d, mid, d, mid+d, d)
	local = append(local, geo.Point{X: length - d, Y: d})
	quad(length-d, d, length, d, length, 0)

	x1, y1 := box.TopLeft.X, box.TopLeft.Y
	x2, y2 := x1+box.Width, y1+box.Height
	pts := make([]geo.Point, len(local))
	for i, p := range local {
		switch side {
		case "top-center":
			pts[i] = geo.Point{X: x1 + p.X, Y: y1 - gap - p.Y}
		case "center-left":
			pts[i] = geo.Point{X: x1 - gap - p.Y, Y: y1 + p.X}
		case "center-right":
			pts[i] = geo.Point{X: x2 + gap + p.Y, Y: y1 + p.X}
		default:
			pts[i] = geo.Point{X: x1 + p.X, Y: y2 + gap + p.Y}
		}
	}
	return simplify(pts)
}

// strike is one pen stroke from a to b that bows to one side on the way.
func strike(ax, ay, bx, by, bow float64) []geo.Point {
	length := dist(bx-ax, by-ay)
	if length == 0 {
		return nil
	}
	nx, ny := -(by-ay)/length, (bx-ax)/length
	steps := int(math.Max(6, length/10))
	pts := make([]geo.Point, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		off := float64(bow * math.Sin(math.Pi*t))
		pts = append(pts, geo.Point{X: ax + float64((bx-ax)*t) + float64(nx*off), Y: ay + float64((by-ay)*t) + float64(ny*off)})
	}
	return simplify(pts)
}

// crossStrokes strikes the box out with two strokes that run on past its
// corners, each end by its own amount, as a pen does crossing something out.
func crossStrokes(box *geo.Box, seed float64) [][]geo.Point {
	x1, y1 := box.TopLeft.X, box.TopLeft.Y
	x2, y2 := x1+box.Width, y1+box.Height
	diagonal := dist(box.Width, box.Height)
	if diagonal == 0 {
		return nil
	}
	ux, uy := box.Width/diagonal, box.Height/diagonal
	past := math.Max(9, math.Min(15, math.Min(box.Width, box.Height)*0.22))
	u, v := 0.5+0.5*math.Sin(seed), 0.5+0.5*math.Cos(1.7*seed)
	a, b := float64(past*(0.85+0.25*u)), float64(past*(1.08-0.2*u))
	c, d := float64(past*(1-0.18*v)), float64(past*(0.82+0.26*v))
	bow := math.Min(4, diagonal*0.02)
	return [][]geo.Point{
		strike(x1-float64(ux*a), y1-float64(uy*a), x2+float64(ux*b), y2+float64(uy*b), bow*(1+0.3*math.Sin(seed))),
		strike(x2+float64(ux*c), y1-float64(uy*c), x1-float64(ux*d), y2+float64(uy*d), -bow*(1+0.3*math.Cos(seed))),
	}
}

// routeCrossStrokes strikes a route out at its middle with an X about 37
// across, so that its arms stand clear of the line on both sides. The X is
// upright, as it is on a page. Where the route runs close to one of its
// strokes, it turns just far enough for that stroke to cross the route
// instead of lying along it.
func routeCrossStrokes(route []geo.Point, seed float64) [][]geo.Point {
	const (
		// arm is half the length of a stroke.
		arm = 26.
		// clearance is the least angle between the route and either stroke.
		clearance = 24 * math.Pi / 180
	)
	mid := pointAlong(route, 0.5)
	turn := 0.
	if total := strokeLength(route); total > 0 {
		reach := math.Min(6, total/4) / total
		p, q := pointAlong(route, 0.5-reach), pointAlong(route, 0.5+reach)
		// The heading is read off the tenth-of-a-pixel grid. A route that
		// runs exactly along a stroke is then a tie on every machine, and
		// the choice of which way to turn is made on whole numbers.
		dx, dy := tenths(q.X)-tenths(p.X), tenths(q.Y)-tenths(p.Y)
		if dx < 0 {
			dx, dy = -dx, -dy
		}
		rise := dy
		if rise < 0 {
			rise = -rise
		}
		if dx != 0 && rise != 0 {
			near := math.Pi/4 - math.Atan2(float64(min(dx, rise)), float64(max(dx, rise)))
			if near < clearance {
				// Turn the nearer stroke away from the route: steeper when
				// the route is the shallower of the two, and the reverse.
				turn = clearance - near
				if rise > dx {
					turn = -turn
				}
				if dy < 0 {
					turn = -turn
				}
			}
		}
	}
	// The strokes of an upright X, turned together.
	ax, ay := math.Cos(math.Pi/4+turn), math.Sin(math.Pi/4+turn)
	bx, by := math.Cos(turn-math.Pi/4), math.Sin(turn-math.Pi/4)
	u, v := 0.5+0.5*math.Sin(seed), 0.5+0.5*math.Cos(1.7*seed)
	a1, a2 := float64(arm*(0.92+0.12*u)), float64(arm*(1.06-0.1*u))
	b1, b2 := float64(arm*(1.04-0.1*v)), float64(arm*(0.9+0.14*v))
	return [][]geo.Point{
		strike(mid.X-float64(ax*a1), mid.Y-float64(ay*a1), mid.X+float64(ax*a2), mid.Y+float64(ay*a2), 1+0.4*math.Sin(seed)),
		strike(mid.X+float64(bx*b1), mid.Y+float64(by*b1), mid.X-float64(bx*b2), mid.Y-float64(by*b2), -1-0.4*math.Cos(seed)),
	}
}

// flattenRoute turns a connection's drawn path into a polyline.
func flattenRoute(c *Connection) []geo.Point {
	var pts []geo.Point
	var current geo.Point
	zero := &geo.Point{}
	for cmd := range c.PathCommands(zero, zero) {
		switch cmd.Kind {
		case 'M', 'L':
			pts = append(pts, cmd.End)
		case 'C', 'S':
			c1 := cmd.Control1
			if cmd.Kind == 'S' {
				// The smooth segment follows a line, so its first control is
				// the point it starts from.
				c1 = current
			}
			for i := 1; i <= 16; i++ {
				t := float64(i) / 16
				mt := 1 - t
				pts = append(pts, geo.Point{
					X: mt*mt*mt*current.X + 3*mt*mt*t*c1.X + 3*mt*t*t*cmd.Control2.X + t*t*t*cmd.End.X,
					Y: mt*mt*mt*current.Y + 3*mt*mt*t*c1.Y + 3*mt*t*t*cmd.Control2.Y + t*t*t*cmd.End.Y,
				})
			}
		}
		current = cmd.End
	}
	return simplify(pts)
}

func strokeLength(pts []geo.Point) float64 {
	total := 0.
	for i := 1; i < len(pts); i++ {
		total += dist(pts[i].X-pts[i-1].X, pts[i].Y-pts[i-1].Y)
	}
	return total
}

func pointAlong(pts []geo.Point, fraction float64) geo.Point {
	if len(pts) == 0 {
		return geo.Point{}
	}
	target := strokeLength(pts) * clampUnit(fraction)
	for i := 1; i < len(pts); i++ {
		seg := dist(pts[i].X-pts[i-1].X, pts[i].Y-pts[i-1].Y)
		if target <= seg && seg > 0 {
			t := target / seg
			return geo.Point{X: pts[i-1].X + (pts[i].X-pts[i-1].X)*t, Y: pts[i-1].Y + (pts[i].Y-pts[i-1].Y)*t}
		}
		target -= seg
	}
	return pts[len(pts)-1]
}

// cutStrokes keeps the part of a mark between two fractions of its total
// length. Several strokes are drawn one after another, like lifting a pen.
func cutStrokes(strokes [][]geo.Point, a *Annotation) [][]geo.Point {
	from, to := a.drawn()
	if from <= 0 && to >= 1 {
		return strokes
	}
	total := 0.
	for _, s := range strokes {
		total += strokeLength(s)
	}
	start, end := from*total, to*total
	if end-start < 0.5 {
		return nil
	}
	var out [][]geo.Point
	offset := 0.
	for _, s := range strokes {
		length := strokeLength(s)
		lo, hi := math.Max(start-offset, 0), math.Min(end-offset, length)
		offset += length
		if hi-lo < 0.5 {
			continue
		}
		var part []geo.Point
		walked := 0.
		for i := 1; i < len(s); i++ {
			seg := dist(s[i].X-s[i-1].X, s[i].Y-s[i-1].Y)
			segStart, segEnd := walked, walked+seg
			walked = segEnd
			if segEnd < lo || segStart > hi || seg == 0 {
				continue
			}
			// The pen stops on the same tenth-of-a-pixel grid as the rest of
			// the mark, which also keeps a frame's path data short.
			at := func(d float64) geo.Point {
				t := (d - segStart) / seg
				return geo.Point{X: snapTenth(s[i-1].X + (s[i].X-s[i-1].X)*t), Y: snapTenth(s[i-1].Y + (s[i].Y-s[i-1].Y)*t)}
			}
			if len(part) == 0 {
				part = append(part, at(math.Max(lo, segStart)))
			}
			part = append(part, at(math.Min(hi, segEnd)))
		}
		if len(part) > 1 {
			out = append(out, part)
		}
	}
	return out
}

// tenths rounds a coordinate to a whole number of tenths of a pixel. A value
// exactly halfway between two tenths would round up on one machine and down
// on another once its last bit differs. The nudge decides those the same way
// everywhere and is far too small to see.
func tenths(v float64) int64 {
	return int64(math.Floor(v*10 + 0.5 + 1e-7))
}

func snapTenth(v float64) float64 {
	return float64(tenths(v)) / 10
}

// simplify snaps a stroke to tenths of a pixel and drops the points that sit
// within a fifth of a pixel of the line between the points kept on either
// side. The result is a polyline that reads as a curve at the stroke widths
// annotations use.
//
// The same diagram must give the same bytes on every architecture. Sines,
// powers and fused multiply-adds differ in their last bit between them, so
// the points are quantized first and every choice after that is made on
// whole numbers, where a tie is a tie everywhere.
func simplify(pts []geo.Point) []geo.Point {
	type point struct{ x, y int64 }
	q := make([]point, 0, len(pts))
	for _, p := range pts {
		next := point{tenths(p.X), tenths(p.Y)}
		if len(q) == 0 || q[len(q)-1] != next {
			q = append(q, next)
		}
	}
	keep := make([]bool, len(q))
	if len(q) > 0 {
		keep[0], keep[len(q)-1] = true, true
	}
	type span struct{ a, b int }
	stack := make([]span, 0, 16)
	if len(q) > 2 {
		stack = append(stack, span{0, len(q) - 1})
	}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		// Whole numbers held in float64 multiply exactly at any size a
		// diagram reaches. The conversions around each product keep the
		// compiler from fusing it into the subtraction that follows.
		dx, dy := float64(q[s.b].x-q[s.a].x), float64(q[s.b].y-q[s.a].y)
		chord := float64(dx*dx) + float64(dy*dy)
		// Compare points by twice the area they span with the chord, which
		// orders them by distance from it. A closed span has no chord, so
		// compare distances from its one end instead.
		worst, index := 0., -1
		for i := s.a + 1; i < s.b; i++ {
			ex, ey := float64(q[i].x-q[s.a].x), float64(q[i].y-q[s.a].y)
			d := math.Abs(float64(dy*ex) - float64(dx*ey))
			if chord == 0 {
				d = float64(ex*ex) + float64(ey*ey)
			}
			if d > worst {
				worst, index = d, i
			}
		}
		// The tolerance is two tenths: distance² > 4, where distance is the
		// area over the chord's length.
		far := worst > 4
		if chord != 0 {
			far = float64(worst*worst) > float64(4*chord)
		}
		if index >= 0 && far {
			keep[index] = true
			stack = append(stack, span{s.a, index}, span{index, s.b})
		}
	}
	out := make([]geo.Point, 0, len(q)/2+2)
	for i, p := range q {
		if keep[i] {
			out = append(out, geo.Point{X: float64(p.x) / 10, Y: float64(p.y) / 10})
		}
	}
	return out
}

// ReadingMS estimates how long the board's caption and notes take to read.
// An animation holds the board at least this long, so that a step lasts as
// long as its sentence. Subtitles are paced at about 17 characters a second;
// the constant gives the eye time to find each new piece of text.
func (diagram *Diagram) ReadingMS() int {
	chars := 0
	if a := diagram.Root.Annotation; a != nil {
		chars += readableLength(a.Label)
	}
	for i := range diagram.Shapes {
		if a := diagram.Shapes[i].Annotation; a != nil {
			chars += readableLength(a.Label)
		}
	}
	for i := range diagram.Connections {
		if a := diagram.Connections[i].Annotation; a != nil {
			chars += readableLength(a.Label)
		}
	}
	return readingMS(chars)
}

// LabelReadingMS applies the animation's reading pace to a single label.
func LabelReadingMS(text string, markdown bool) int {
	if markdown {
		return readingMS(readableLength(text))
	}
	return readingMS(utf8.RuneCountInString(text))
}

func readingMS(chars int) int {
	const settleMS, perCharMS, maxMS = 700, 58, 20_000
	if chars == 0 {
		return 0
	}
	return min(maxMS, settleMS+perCharMS*min(chars, maxMS/perCharMS))
}

// readableLength counts the characters a reader sees, leaving out Markdown
// punctuation and the destinations of links.
func readableLength(markdown string) int {
	n, inDestination := 0, false
	runes := []rune(markdown)
	for i, r := range runes {
		switch {
		case inDestination:
			inDestination = r != ')'
		case r == ']' && i+1 < len(runes) && runes[i+1] == '(':
			inDestination = true
		case r == '*' || r == '_' || r == '`' || r == '[' || r == '#' || r == '~':
		default:
			n++
		}
	}
	return n
}

// CaptionBox places the board's caption under everything else on the board.
// ink is the board's extent without the caption.
func (diagram *Diagram) CaptionBox(inkTL, inkBR Point) *geo.Box {
	a := diagram.Root.Annotation
	if a == nil || a.Label == "" || a.LabelWidth <= 0 || a.LabelHeight <= 0 {
		return nil
	}
	w, h := float64(a.LabelWidth), float64(a.LabelHeight)
	cx, top := float64(inkTL.X+inkBR.X)/2, float64(inkBR.Y+CaptionGap)
	if a.Anchor != nil {
		cx, top = float64(a.Anchor.X), float64(a.Anchor.Y)
	}
	return geo.NewBox(geo.NewPoint(math.Round(cx-w/2), math.Round(top)), w, h)
}
