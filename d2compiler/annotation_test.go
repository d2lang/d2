package d2compiler_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/util-go/assert"
)

func compileAnnotated(t *testing.T, source string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("test.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	return g
}

func deepEqual(t *testing.T, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func object(t *testing.T, g *d2graph.Graph, id string) *d2graph.Object {
	t.Helper()
	for _, obj := range g.Objects {
		if obj.AbsID() == id {
			return obj
		}
	}
	t.Fatalf("no object %q", id)
	return nil
}

func TestAnnotationForms(t *testing.T) {
	t.Parallel()

	g := compileAnnotated(t, `a: {annotation: 4 s behind {shape: box; near: top-center; style.stroke: red}}
b.annotation.shape: underline
c.annotation: |md **hot** path |
d.annotation: plain
a -> b: {annotation: slow}
(a -> b)[0].annotation.shape: cross
`)

	a := object(t, g, "a").Annotation
	assert.String(t, "4 s behind", a.Label.Value)
	assert.String(t, "box", a.Shape.Value)
	assert.String(t, "top-center", a.Position.Value)
	assert.String(t, "red", a.Stroke.Value)

	// A mark needs no words.
	b := object(t, g, "b").Annotation
	assert.String(t, "", b.Label.Value)
	assert.String(t, "underline", b.Shape.Value)

	assert.String(t, "**hot** path", object(t, g, "c").Annotation.Label.Value)

	// With no shape named, the renderer chooses.
	d := object(t, g, "d").Annotation
	assert.String(t, "plain", d.Label.Value)
	if d.Shape != nil {
		t.Fatalf("unset shape compiled to %q", d.Shape.Value)
	}

	// The dotted form refines the annotation set in the connection's map.
	edge := g.Edges[0].Annotation
	assert.String(t, "slow", edge.Label.Value)
	assert.String(t, "cross", edge.Shape.Value)
}

func TestAnnotationFromClass(t *testing.T) {
	t.Parallel()

	g := compileAnnotated(t, `classes: {flagged: {annotation: check this {shape: box}}}
a.class: flagged
b: {class: flagged; annotation.shape: circle}
`)
	a := object(t, g, "a").Annotation
	assert.String(t, "check this", a.Label.Value)
	assert.String(t, "box", a.Shape.Value)

	// The object's own fields refine what its class set.
	b := object(t, g, "b").Annotation
	assert.String(t, "check this", b.Label.Value)
	assert.String(t, "circle", b.Shape.Value)
}

func TestAnnotationLastsOneBoard(t *testing.T) {
	t.Parallel()

	g := compileAnnotated(t, `classes: {flagged: {annotation: from a class}}
a; b; c: {class: flagged}
a -> b
annotation: root caption
a.annotation: on root
(a -> b)[0].annotation: on root
scenarios: {
  alt: {b.annotation: on the scenario}
}
steps: {
  one: {
    annotation: first
    b.annotation: on one
  }
  two: {
    d
  }
  three: {
    b.annotation: on three
  }
}
`)
	annotated := func(board *d2graph.Graph) map[string]string {
		found := make(map[string]string)
		if board.Root.Annotation != nil {
			found["(board)"] = board.Root.Annotation.Label.Value
		}
		for _, obj := range board.Objects {
			if obj.Annotation != nil {
				found[obj.AbsID()] = obj.Annotation.Label.Value
			}
		}
		for _, edge := range board.Edges {
			if edge.Annotation != nil {
				found[edge.AbsID()] = edge.Annotation.Label.Value
			}
		}
		return found
	}
	deepEqual(t, map[string]string{"(board)": "root caption", "a": "on root", "(a -> b)[0]": "on root", "c": "from a class"}, annotated(g))

	// A scenario and a step start from another board without its annotations.
	// A class is a declaration, so the annotation it carries applies on every
	// board that uses the class.
	deepEqual(t, map[string]string{"b": "on the scenario", "c": "from a class"}, annotated(g.Scenarios[0]))
	deepEqual(t, map[string]string{"(board)": "first", "b": "on one", "c": "from a class"}, annotated(g.Steps[0]))
	deepEqual(t, map[string]string{"c": "from a class"}, annotated(g.Steps[1]))
	deepEqual(t, map[string]string{"b": "on three", "c": "from a class"}, annotated(g.Steps[2]))
}

func TestAnnotationQuotedNameIsAShape(t *testing.T) {
	t.Parallel()

	// A quoted key is an ordinary name and carries forward like any shape.
	g := compileAnnotated(t, `"annotation": a shape
steps: {one: {x}}
`)
	for _, board := range []*d2graph.Graph{g, g.Steps[0]} {
		if board.Root.Annotation != nil {
			t.Fatal("quoted name compiled as the board's caption")
		}
		if len(board.Objects) == 0 || board.Objects[0].Label.Value != "a shape" {
			t.Fatalf("quoted name is not a shape on board %q", board.Name)
		}
	}
}

func TestCaptionCues(t *testing.T) {
	t.Parallel()

	g := compileAnnotated(t, `api; db: {r2}
annotation: |md The [API](api) reads [a replica](db.r2), then the [API](api) again. See [the docs](https://d2lang.com) or [nothing](missing). |
`)
	caption := g.Root.Annotation
	// A link to an object on the board becomes emphasis and a cue. Other
	// links are left for Markdown.
	assert.String(t, "The **API** reads **a replica**, then the **API** again. See [the docs](https://d2lang.com) or [nothing](missing).", caption.Markdown())
	deepEqual(t, []string{"api", "db.r2"}, caption.Cues)
	// The authored text is what tools read back and edit.
	if !strings.Contains(caption.Label.Value, "[API](api)") {
		t.Fatalf("authored caption was rewritten: %q", caption.Label.Value)
	}

	plain := compileAnnotated(t, `x
annotation: No links here.
`).Root.Annotation
	assert.String(t, "No links here.", plain.Markdown())
	assert.Equal(t, 0, len(plain.Cues))
}

func TestAnnotationErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, source, want string }{
		{"unknown shape", `x.annotation.shape: squiggle`, `unknown annotation shape "squiggle"`},
		{"trace on a shape", `x.annotation.shape: trace`, `"trace" follows a connection`},
		{"ring on a connection", `x -> y: {annotation.shape: circle}`, `on a connection use trace or cross`},
		{"shape on a board", `x; annotation: caption {shape: box}`, `a board's annotation is its caption and has no shape`},
		{"near on a board", `x; annotation: caption {near: top-center}`, `always placed below the diagram`},
		{"corner near", `x.annotation: note {near: top-left}`, `invalid annotation near "top-left"`},
		{"code block", "x.annotation: |go\n  fmt.Println()\n|", `annotation text is Markdown`},
		{"unexpected field", `x.annotation: note {width: 3}`, `unexpected field width in annotation`},
		{"style other than stroke", `x.annotation: note {style.fill: red}`, `can only style its "stroke"`},
		{"bad stroke", `x.annotation: note {style.stroke: notacolor}`, `expected "stroke" to be a valid named color`},
		{"composite shape", `x.annotation.shape: {a: b}`, `annotation shape must be a single value`},
		{"arrowhead", `x -> y: {target-arrowhead.annotation: tip}`, `an arrowhead cannot be annotated`},
		{"edge in map", `x.annotation: {a -> b}`, `unexpected edges in map`},
		// "annotation" was an ordinary name before it became a keyword. The
		// ways an existing shape of that name fails all say how to keep it.
		{"shape named annotation", `annotation: {shape: text; near: a}`, `If "annotation" is the name of a shape here, quote it`},
		{"shape named annotation placed near another", `a; annotation: note {near: a}`, `If "annotation" is the name of a shape here, quote it`},
		{"container named annotation", `annotation: {child}`, `If "annotation" is the name of a shape here, quote it`},
		{"shape named annotation in a connection", `annotation -> a`, `reserved keywords are prohibited in edges. If "annotation" is the name of a shape here, quote it`},
		{"shape named annotation inside a connection's path", `a -> b.annotation`, `If "annotation" is the name of a shape here, quote it`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := d2compiler.Compile("test.d2", strings.NewReader(tc.source), nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}
