package d2lib

import (
	"html"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/textmeasure"
)

// Changes refer to object IDs because grid motion can reorder, hide, or add
// objects between samples. An unrelated label at the same slot keeps its ink.
type boardInk struct {
	shapes map[string]shapeInk
	edges  map[string]labelInk
}

type shapeInk struct {
	label labelInk
	fill  *d2target.SketchFillTransition
}

type labelInk struct {
	label       string
	underline   bool
	strikeFrom  string
	plainRanges [][2]float64
}

type labelInkPlanner struct {
	board *d2target.Diagram
	ruler *textmeasure.Ruler
}

func (p *labelInkPlanner) plainStrikeRanges(text d2target.Text, spans [][2]int) [][2]float64 {
	if len(spans) == 1 && spans[0] == [2]int{0, len(text.Label)} {
		return [][2]float64{{0, 1}}
	}
	if p.ruler == nil {
		p.ruler, _ = textmeasure.NewRuler()
	}
	if p.ruler == nil {
		return nil
	}
	family := d2fonts.SourceSansPro
	if p.board.FontFamily != nil {
		family = *p.board.FontFamily
	}
	if strings.EqualFold(text.FontFamily, "mono") {
		family = d2fonts.SourceCodePro
		if p.board.MonoFontFamily != nil {
			family = *p.board.MonoFontFamily
		}
	} else if override, ok := d2fonts.D2_FONT_TO_FAMILY[strings.ToLower(text.FontFamily)]; ok && !strings.EqualFold(text.FontFamily, "default") {
		family = override
	}
	style := d2fonts.FONT_STYLE_REGULAR
	if text.Bold {
		style = d2fonts.FONT_STYLE_BOLD
	} else if text.Italic {
		style = d2fonts.FONT_STYLE_ITALIC
	}
	return p.ruler.StrikeAdvances(text.Label, spans, family.Font(text.FontSize, style))
}

func (p *labelInkPlanner) plan(old, next d2target.Text) labelInk {
	if next.Label == "" {
		return labelInk{}
	}
	if old.Language != next.Language {
		plain, marked := old, next
		if next.Language == "" {
			plain, marked = next, old
		}
		if plain.Language != "" || marked.Language != "markdown" {
			return labelInk{}
		}
		underline := old.Underline != next.Underline
		if !underline && !strings.ContainsAny(marked.Label, "~<") {
			return labelInk{}
		}
		spans := textmeasure.PlainMarkdownStrikes(plain.Label, marked.Label)
		if spans == nil || (len(spans) == 0 && !underline) {
			return labelInk{}
		}
		change := labelInk{label: next.Label, underline: underline}
		if len(spans) != 0 {
			if next.Language == "markdown" {
				// Character data preserves literal punctuation across the Markdown parser.
				change.strikeFrom = "<p>" + html.EscapeString(plain.Label) + "</p>"
			} else {
				change.plainRanges = p.plainStrikeRanges(plain, spans)
				if len(change.plainRanges) != 0 {
					change.strikeFrom = marked.Label
				} else if !underline {
					return labelInk{}
				}
			}
		}
		return change
	}
	switch next.Language {
	case "", "markdown":
	default:
		return labelInk{}
	}
	var change labelInk
	if next.Language == "markdown" && old.Label != next.Label && textmeasure.MarkdownStrikeChanged(old.Label, next.Label) {
		change.strikeFrom = old.Label
	}
	if old.Label == next.Label || change.strikeFrom != "" {
		change.underline = old.Underline != next.Underline
	}
	if change.underline || change.strikeFrom != "" {
		change.label = next.Label
	}
	return change
}

func sameSketchFill(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	// Theme tokens remain unresolved until rendering, including the dark theme.
	from, fromOK := color.ParseRGBA(a)
	to, toOK := color.ParseRGBA(b)
	return fromOK && toOK && from == to
}

func planSketchFill(old, next, inherited *d2target.Shape) *d2target.SketchFillTransition {
	if old.Fill == next.Fill && (inherited == nil || inherited.Fill == next.Fill) {
		return nil
	}
	if !old.CanShadeSketchFill() || !next.CanShadeSketchFill() {
		return nil
	}
	if !sameSketchFill(old.Fill, next.Fill) {
		return &d2target.SketchFillTransition{From: old.Fill, To: next.Fill}
	}
	if inherited != nil && !sameSketchFill(inherited.Fill, next.Fill) {
		// Keep an unchanged surface painted through an unrelated inherited tween.
		// Regular rendering still uses the original frame's fill.
		return &d2target.SketchFillTransition{From: next.Fill, To: next.Fill, Progress: 1}
	}
	return nil
}

func planBoardInk(prev, next *d2target.Diagram, sketch, pencilOnly bool) *boardInk {
	if prev == nil {
		return nil
	}
	var ink boardInk
	labels := labelInkPlanner{board: next}
	shapes := gridEdgeShapeMap(prev.Shapes)
	// A step inherits paint samples from its parent or previous sibling. Its
	// predecessor on the playback timeline can instead be a layer or scenario.
	var inherited map[string]*d2target.Shape
	if sketch && len(next.TweenFrames) > 0 {
		inherited = gridEdgeShapeMap(next.TweenFrames[0].Shapes)
	}
	for i := range next.Shapes {
		s := &next.Shapes[i]
		old := shapes[s.ID]
		if old == nil {
			continue
		}
		sameShape := old.Type == s.Type
		// D2 infers a text shape for a Markdown block and a rectangle for
		// a plain label. Their matching words can still gain or lose a strike.
		plainToText := old.Type == d2target.ShapeRectangle && s.Type == d2target.ShapeText && old.Language == "" && s.Language == "markdown"
		textToPlain := old.Type == d2target.ShapeText && s.Type == d2target.ShapeRectangle && old.Language == "markdown" && s.Language == ""
		if !sameShape && !plainToText && !textToPlain {
			continue
		}
		var edit shapeInk
		if !pencilOnly && s.Type != d2target.ShapeClass && s.Type != d2target.ShapeSQLTable {
			edit.label = labels.plan(old.Text, s.Text)
		}
		if sketch && sameShape {
			edit.fill = planSketchFill(old, s, inherited[s.ID])
		}
		if edit.fill != nil || edit.label.label != "" {
			if ink.shapes == nil {
				ink.shapes = make(map[string]shapeInk)
			}
			ink.shapes[s.ID] = edit
		}
	}
	if !pencilOnly {
		edges := make(map[string]*d2target.Connection, len(prev.Connections))
		for i := range prev.Connections {
			c := &prev.Connections[i]
			edges[c.ID] = c
		}
		for i := range next.Connections {
			c := &next.Connections[i]
			if old := edges[c.ID]; old != nil {
				edit := labels.plan(old.Text, c.Text)
				// A plain linked connection label already has a permanent underline.
				if c.Language == "" && (old.Link != "" || c.Link != "") {
					edit.underline = false
				}
				if edit.underline || edit.strikeFrom != "" {
					if ink.edges == nil {
						ink.edges = make(map[string]labelInk)
					}
					ink.edges[c.ID] = edit
				}
			}
		}
	}
	if ink.shapes == nil && ink.edges == nil {
		return nil
	}
	return &ink
}

func (edit labelInk) apply(t *d2target.Text, progress float64) {
	if edit.label == "" || t.Label != edit.label {
		return
	}
	if edit.underline {
		visible := progress
		if !t.Underline {
			visible = 1 - progress
		}
		t.UnderlineProgress = &visible
	}
	if edit.strikeFrom != "" {
		t.MarkdownStrike = &d2target.MarkdownStrikeTransition{From: edit.strikeFrom, Progress: progress, PlainRanges: edit.plainRanges}
	}
}

func (ink *boardInk) apply(frame *d2target.Diagram, progress float64) {
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		if edit, ok := ink.shapes[s.ID]; ok {
			edit.label.apply(&s.Text, progress)
			if edit.fill != nil {
				fill := *edit.fill
				if fill.From != fill.To {
					fill.Progress = progress
				}
				s.SketchFillTransition = &fill
			}
		}
	}
	for i := range frame.Connections {
		c := &frame.Connections[i]
		if edit, ok := ink.edges[c.ID]; ok {
			edit.apply(&c.Text, progress)
		}
	}
}
