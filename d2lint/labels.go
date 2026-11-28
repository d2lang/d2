package d2lint

import (
	"fmt"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
)

type labelID struct {
	id   string
	edge bool
}

type authoredLabel struct {
	text     string
	markdown bool
	key      *d2ast.Key
}

func labelSources(b *board) map[labelID]authoredLabel {
	labels := map[labelID]authoredLabel{}
	for i := range b.Shapes {
		s := &b.Shapes[i]
		if s.Label != "" {
			labels[labelID{id: s.ID}] = authoredLabel{text: s.Label, markdown: s.Language == "markdown"}
		}
	}
	for i := range b.Connections {
		c := &b.Connections[i]
		if c.Label != "" {
			labels[labelID{id: c.ID, edge: true}] = authoredLabel{text: c.Label, markdown: c.Language == "markdown"}
		}
	}
	// A previously hidden label can flash even if its text never changes.
	// Point to the opacity change that revealed it in this step.
	hidden := map[labelID]string{}
	if b.base != nil {
		for i := range b.base.Shapes {
			s := &b.base.Shapes[i]
			if s.Label != "" && !visible(s) {
				hidden[labelID{id: s.ID}] = s.Label
			}
		}
		for i := range b.base.Connections {
			c := &b.base.Connections[i]
			if c.Label != "" && !edgeLabelVisible(c) {
				hidden[labelID{id: c.ID, edge: true}] = c.Label
			}
		}
	}
	locate := func(id labelID, a *d2graph.Attributes) {
		l, ok := labels[id]
		if !ok {
			return
		}
		l.key = a.Label.MapKey
		if hidden[id] == l.text {
			l.key = nil
			if a.Style.Opacity != nil {
				l.key = a.Style.Opacity.MapKey
			}
		}
		labels[id] = l
	}
	for _, o := range b.graph.Objects {
		locate(labelID{id: o.AbsID()}, &o.Attributes)
	}
	for _, e := range b.graph.Edges {
		locate(labelID{id: e.AbsID(), edge: true}, &e.Attributes)
	}
	return labels
}

type screenLabel struct {
	labelID
	layer, text string
}

type labelRun struct {
	board           *board
	source          authoredLabel
	start, lastSeen int64
}

type labelTracker struct {
	active map[screenLabel]*labelRun
	// Opening runs can continue across the loop boundary. Defer their
	// diagnostics until we know which labels are still up in the final frame.
	opening map[screenLabel]*labelRun
}

func newLabelTracker() *labelTracker {
	return &labelTracker{active: make(map[screenLabel]*labelRun), opening: make(map[screenLabel]*labelRun)}
}

func (l *labelTracker) observe(b *board, d *d2target.Diagram, authored map[labelID]authoredLabel, t int64, findings *[]Diagnostic) {
	see := func(id labelID, text string) {
		if text == "" {
			return
		}
		k := screenLabel{id, b.layer, text}
		if run := l.active[k]; run != nil {
			run.lastSeen = t
		} else if source, ok := authored[id]; ok && source.text == text {
			// Tween samples may contain generated intermediate text. Only start
			// the clock on a label that the author actually asked to display.
			l.active[k] = &labelRun{b, source, t, t}
		}
	}
	for i := range d.Shapes {
		s := &d.Shapes[i]
		if visible(s) {
			see(labelID{id: s.ID}, s.Label)
		}
	}
	for i := range d.Connections {
		c := &d.Connections[i]
		if edgeLabelVisible(c) {
			see(labelID{id: c.ID, edge: true}, c.Label)
		}
	}
	for k, run := range l.active {
		if run.lastSeen == t {
			continue
		}
		if run.start == 0 {
			run.lastSeen = t // end of the opening run
			l.opening[k] = run
		} else {
			run.report(k, t-run.start, findings)
		}
		delete(l.active, k)
	}
}

func edgeLabelVisible(c *d2target.Connection) bool {
	return c.Opacity > 0 && (c.MotionLabel == nil || c.MotionLabel.Opacity > 0)
}

func (l *labelTracker) finish(t int64, findings *[]Diagnostic) {
	for k, run := range l.active {
		if run.start == 0 { // visible for the entire animation
			continue
		}
		length := t - run.start
		if opening := l.opening[k]; opening != nil {
			length += opening.lastSeen
			delete(l.opening, k)
		}
		run.report(k, length, findings)
	}
	for k, run := range l.opening {
		run.report(k, run.lastSeen, findings)
	}
}

func (run *labelRun) report(label screenLabel, ms int64, findings *[]Diagnostic) {
	want := d2target.LabelReadingMS(label.text, run.source.markdown)
	if ms >= int64(want) {
		return
	}
	text := label.text
	if len(text) > 80 {
		runes := []rune(text)
		if len(runes) > 80 {
			text = string(runes[:79]) + "…"
		}
	}
	*findings = append(*findings, run.board.diagnostic("short-label", label.id, fmt.Sprintf("%q is visible for %.2g s. Allow about %.2g s to read it.", strings.Join(strings.Fields(text), " "), float64(ms)/1000, float64(want)/1000), run.source.key))
}
