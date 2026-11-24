package d2lib

import (
	"strings"

	"github.com/mazznoer/csscolorparser"

	"github.com/d2lang/d2/d2target"
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
	label      string
	underline  bool
	strikeFrom string
}

func planLabelInk(old, next d2target.Text) labelInk {
	if next.Label == "" || old.Language != next.Language {
		return labelInk{}
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

func changedSketchFill(old, next *d2target.Shape) bool {
	if strings.EqualFold(old.Fill, next.Fill) || !old.CanShadeSketchFill() || !next.CanShadeSketchFill() {
		return false
	}
	// Theme tokens remain unresolved until rendering, including the dark theme.
	from, fromErr := csscolorparser.Parse(old.Fill)
	to, toErr := csscolorparser.Parse(next.Fill)
	return fromErr != nil || toErr != nil || from != to
}

func planBoardInk(prev, next *d2target.Diagram, sketch, pencilOnly bool) *boardInk {
	if prev == nil {
		return nil
	}
	var ink boardInk
	shapes := gridEdgeShapeMap(prev.Shapes)
	for i := range next.Shapes {
		s := &next.Shapes[i]
		old := shapes[s.ID]
		if old == nil || old.Type != s.Type {
			continue
		}
		var edit shapeInk
		if !pencilOnly && s.Type != d2target.ShapeClass && s.Type != d2target.ShapeSQLTable {
			edit.label = planLabelInk(old.Text, s.Text)
		}
		if sketch && changedSketchFill(old, s) {
			edit.fill = &d2target.SketchFillTransition{From: old.Fill, To: s.Fill}
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
				edit := planLabelInk(old.Text, c.Text)
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
		t.MarkdownStrike = &d2target.MarkdownStrikeTransition{From: edit.strikeFrom, Progress: progress}
	}
}

func (ink *boardInk) apply(frame *d2target.Diagram, progress float64) {
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		if edit, ok := ink.shapes[s.ID]; ok {
			edit.label.apply(&s.Text, progress)
			if edit.fill != nil {
				fill := *edit.fill
				fill.Progress = progress
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
