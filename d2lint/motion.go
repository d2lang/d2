// Package d2lint reviews compiled diagrams for potential authoring problems.
package d2lint

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2target"
)

// Diagnostic is an advisory motion finding with a one-based source position.
type Diagnostic struct {
	Code    string `json:"code"`
	Board   string `json:"board"`
	Object  string `json:"object,omitempty"`
	Message string `json:"message"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

type board struct {
	*d2target.Diagram
	graph *d2graph.Graph
	path  string
	// Each independent layer starts a fresh namespace for object identity.
	layer string
	base  *board // source for an incoming step or scenario
}

func (b *board) diagnostic(code, object, message string, key *d2ast.Key) Diagnostic {
	r := b.graph.BoardRange
	if key != nil {
		r = key.Range
	}
	return Diagnostic{
		Code: code, Board: b.path, Object: object, Message: message,
		Path: r.Path, Line: r.Start.Line + 1, Column: r.Start.Column + 1,
	}
}

// Motion reviews a diagram and its corresponding source graph from the same
// compilation. Supply tween samples and the export's resolved board interval.
func Motion(ctx context.Context, diagram *d2target.Diagram, graph *d2graph.Graph, intervalMS int) ([]Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if intervalMS <= 0 {
		return nil, fmt.Errorf("motion review requires a positive board interval")
	}
	if err := d2target.ValidateRenderTarget(diagram); err != nil {
		return nil, err
	}

	var boards []*board
	root := &board{Diagram: diagram, graph: graph, path: "root", layer: "root"}
	if err := root.collect(&boards, diagram.ScenarioPredecessors()); err != nil {
		return nil, err
	}

	findings := []Diagnostic{}
	labels := newLabelTracker()
	var elapsed int64
	for _, b := range boards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var first, previous map[string]*d2target.Shape
		if b.base != nil {
			first = shapeIndex(b.base.Diagram)
			previous = first
		}
		sources := labelSources(b)
		starts := map[string]int{}
		seenCue := map[string]bool{}
		frames := d2animate.AppendBoardFrames(nil, b.Diagram, intervalMS)
		for _, frame := range frames {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if elapsed > math.MaxInt64-int64(frame.DurationMS) {
				return nil, fmt.Errorf("motion review timeline is too long")
			}
			current := shapeIndex(frame.Diagram)
			if b.base != nil {
				findStarts(previous, current, frame.ElapsedMS, starts)
			}
			if caption := frame.Diagram.Root.Annotation; caption != nil && b.Root.Annotation != nil && caption.Label == b.Root.Annotation.Label && caption.CaptionOpacity() > 0 {
				for _, id := range caption.Cues {
					if visible(current[id]) {
						seenCue[id] = true
					}
				}
			}
			labels.observe(b, frame.Diagram, sources, elapsed, &findings)
			previous = current
			elapsed += int64(frame.DurationMS)
		}
		// The final frame is the authored endpoint. A renderer can add temporary
		// shapes during a tween (for example, a separate fading title).
		if b.base != nil {
			findings = append(findings, replacements(b, first, previous)...)
			for id := range starts {
				if first[id] == nil && previous[id] == nil {
					delete(starts, id)
				}
			}
		}
		if n := crowded(starts); n >= 6 {
			findings = append(findings, b.diagnostic("crowded-step", "", fmt.Sprintf("%d objects start changing within 200 ms. Stagger them across boards.", n), nil))
		}
		if caption := b.Root.Annotation; caption != nil && b.graph.Root.Annotation != nil {
			for _, id := range caption.Cues {
				var message string
				switch {
				case !seenCue[id]:
					message = fmt.Sprintf("The caption names %s while it is hidden. Reveal it while the caption is on screen.", id)
				case !visible(previous[id]) && frames[len(frames)-1].DurationMS >= 200:
					message = fmt.Sprintf("The caption names %s, but it is hidden once the board settles. Keep it visible while the caption is on screen.", id)
				}
				if message != "" {
					findings = append(findings, b.diagnostic("hidden-cue", id, message, b.graph.Root.Annotation.Label.MapKey))
				}
			}
		}
	}
	labels.finish(elapsed, &findings)

	// Several labels can disappear on the same frame.
	slices.SortFunc(findings, func(a, b Diagnostic) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column),
			cmp.Compare(a.Board, b.Board), cmp.Compare(a.Code, b.Code), cmp.Compare(a.Object, b.Object), cmp.Compare(a.Message, b.Message))
	})
	return findings, nil
}

// collect follows export order but also preserves the semantic predecessor
// used by the compiler to build incoming steps and scenarios.
func (b *board) collect(boards *[]*board, scenarioBases map[*d2target.Diagram]*d2target.Diagram) error {
	d, g := b.Diagram, b.graph
	if g == nil || d.Name != g.Name || len(d.Layers) != len(g.Layers) || len(d.Scenarios) != len(g.Scenarios) || len(d.Steps) != len(g.Steps) {
		return fmt.Errorf("motion review requires a matching source graph at %s", b.path)
	}
	if !b.IsFolderOnly {
		*boards = append(*boards, b)
	}
	for _, group := range []struct {
		name     string
		diagrams []*d2target.Diagram
		graphs   []*d2graph.Graph
	}{
		{"layers", d.Layers, g.Layers},
		{"scenarios", d.Scenarios, g.Scenarios},
		{"steps", d.Steps, g.Steps},
	} {
		prev := b
		for i, child := range group.diagrams {
			path := d2format.Format(d2ast.MakeKeyPath([]string{group.name, child.Name}))
			if b.path != "root" {
				path = b.path + "." + path
			}
			next := &board{Diagram: child, graph: group.graphs[i], path: path, layer: b.layer}
			if group.name == "layers" {
				next.layer = path
			}
			if group.name == "steps" && !prev.IsFolderOnly {
				next.base = prev
			}
			if scenarioBases[child] != nil {
				next.base = (*boards)[len(*boards)-1]
			}
			if err := next.collect(boards, scenarioBases); err != nil {
				return err
			}
			prev = next
		}
	}
	return nil
}

func shapeIndex(d *d2target.Diagram) map[string]*d2target.Shape {
	shapes := make(map[string]*d2target.Shape, len(d.Shapes))
	for i := range d.Shapes {
		shapes[d.Shapes[i].ID] = &d.Shapes[i]
	}
	return shapes
}

func visible(s *d2target.Shape) bool {
	if s == nil || s.Opacity <= 0 {
		return false
	}
	if c := s.MotionClip; c != nil {
		return c.Width > 0 && c.Height > 0 && s.Pos.X < c.X+c.Width && c.X < s.Pos.X+s.Width && s.Pos.Y < c.Y+c.Height && c.Y < s.Pos.Y+s.Height
	}
	return true
}

// Count each shape's first visible change once, including cuts, arrivals and
// departures. Sampling the rendered frames preserves actual stagger and holds.
func findStarts(before, after map[string]*d2target.Shape, ms int, starts map[string]int) {
	for id, s := range after {
		if _, started := starts[id]; started {
			continue
		}
		old := before[id]
		was, now := visible(old), visible(s)
		if was != now || was && (old.Pos != s.Pos || old.Width != s.Width || old.Height != s.Height ||
			old.Opacity != s.Opacity || old.Fill != s.Fill || old.Stroke != s.Stroke || old.Animated != s.Animated ||
			old.RenderedStrokeWidth() != s.RenderedStrokeWidth() || old.StrokeDash != s.StrokeDash || old.BorderRadius != s.BorderRadius ||
			old.Label != s.Label || s.Label != "" && (old.FontSize != s.FontSize || old.Color != s.Color)) {
			starts[id] = ms
		}
	}
	for id, s := range before {
		if _, started := starts[id]; !started && after[id] == nil && visible(s) {
			starts[id] = ms
		}
	}
}

func crowded(starts map[string]int) int {
	times := make([]int, 0, len(starts))
	for _, ms := range starts {
		times = append(times, ms)
	}
	slices.Sort(times)
	maximum, first := 0, 0
	for i, t := range times {
		for t-times[first] > 200 {
			first++
		}
		maximum = max(maximum, i-first+1)
	}
	return maximum
}

// A single removed and added leaf in the same container is a possible ID
// change when they have the same type and label or occupy the same slot.
// Skip ambiguous replacements and moves across container boundaries.
func replacements(b *board, old, now map[string]*d2target.Shape) []Diagnostic {
	type group struct{ parent, kind string }
	type pair struct{ added, removed []*d2graph.Object }
	groups := map[group]*pair{}
	for _, side := range []struct {
		objects       []*d2graph.Object
		shapes, other map[string]*d2target.Shape
		added         bool
	}{
		{b.base.graph.Objects, old, now, false}, {b.graph.Objects, now, old, true},
	} {
		for _, obj := range side.objects {
			id := obj.AbsID()
			s := side.shapes[id]
			if !visible(s) || side.other[id] != nil || len(obj.ChildrenArray) != 0 {
				continue
			}
			key := group{obj.Parent.AbsID(), s.Type}
			p := groups[key]
			if p == nil {
				p = new(pair)
				groups[key] = p
			}
			if side.added {
				p.added = append(p.added, obj)
			} else {
				p.removed = append(p.removed, obj)
			}
		}
	}
	var out []Diagnostic
	var removals map[*d2graph.Object]*d2ast.Key
	for _, p := range groups {
		if len(p.added) != 1 || len(p.removed) != 1 {
			continue
		}
		removed, added := p.removed[0], p.added[0]
		from, to := removed.AbsID(), added.AbsID()
		a, z := old[from], now[to]
		sameLabel := a.Label != "" && a.Label == z.Label
		sameBox := a.Pos == z.Pos && a.Width == z.Width && a.Height == z.Height
		onlyChild := removed.Parent.Parent != nil && len(removed.Parent.ChildrenArray) == 1 && len(added.Parent.ChildrenArray) == 1
		if !sameLabel && !sameBox && !onlyChild {
			continue
		}
		if removals == nil {
			removals = map[*d2graph.Object]*d2ast.Key{}
			indexRemovals(b.graph.BaseAST, b.base.graph.Root, removals)
		}
		object, key := to, added.Label.MapKey
		if removedAt := removals[removed]; removedAt != nil {
			object, key = from, removedAt
		}
		out = append(out, b.diagnostic("replaced-id", object, fmt.Sprintf("%s disappears as %s takes its place. If they represent the same object, keep one ID across boards.", from, to), key))
	}
	return out
}

// A deleted object has no reference in the new graph. Look up literal nulls
// in this step's original AST against the previous step's scope. Expansions
// and dotted boards without a local AST keep the new declaration's location.
func indexRemovals(m *d2ast.Map, scope *d2graph.Object, out map[*d2graph.Object]*d2ast.Key) {
	if m == nil {
		return
	}
	for _, node := range m.Nodes {
		k := node.MapKey
		if k == nil || k.Key == nil || len(k.Edges) > 0 || k.Key.HasGlob() || d2ast.Keyword(k.Key.Last().Unbox()) != "" {
			continue
		}
		obj, ok := scope.HasChild(d2graph.Key(k.Key))
		if !ok || obj == scope {
			continue
		}
		if k.Value.Null != nil {
			out[obj] = k
		}
		indexRemovals(k.Value.Map, obj, out)
	}
}
