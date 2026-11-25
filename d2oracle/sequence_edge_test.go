package d2oracle_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2oracle"
)

func TestEditScopedSequenceMessage(t *testing.T) {
	const source = `shape: sequence-diagram
f: {shape: edge-group {branched: true}
  one: {a -> b: One}
  two: {
    a -> b -> c: Chain
    a -> b: Two
    (a -> b)[1].style.stroke: red
    (* -> *)[*].style.stroke-dash: 2
  }
}
h: {shape: edge-group; a -> b: Three; (a -> b)[0].style.stroke: blue}
`
	compile := func() *d2graph.Graph {
		g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), nil)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	eq := func(g *d2graph.Graph, label, want string) {
		t.Helper()
		for _, e := range g.Edges {
			if e.Label.Value == label {
				got := ""
				if e.Style.Stroke != nil {
					got = e.Style.Stroke.Value
				}
				if got != want {
					t.Fatalf("%s stroke %q, want %q", label, got, want)
				}
				return
			}
		}
		t.Fatalf("missing message %q in %s", label, d2format.Format(g.AST))
	}
	g := compile()
	color := "green"
	g, err := d2oracle.Set(g, nil, "f.two.(a -> b)[1].style.stroke", nil, &color)
	if err != nil {
		t.Fatal(err)
	}
	eq(g, "One", "")
	eq(g, "Two", "green")
	eq(g, "Three", "blue")
	g, err = d2oracle.Delete(g, nil, "f.two.(a -> b)[1].style.stroke")
	if err != nil {
		t.Fatal(err)
	}
	eq(g, "Two", "")
	g = compile()
	var target string
	for _, e := range g.Edges {
		if e.Label.Value == "Chain" && e.Src.ID == "a" {
			target = e.AbsID()
		}
	}
	g, err = d2oracle.Delete(g, nil, target)
	if err != nil {
		t.Fatal(err)
	}
	eq(g, "One", "")
	eq(g, "Two", "red")
	eq(g, "Three", "blue")
	for _, e := range g.Edges {
		if e.Label.Value == "Chain" && (e.Src.ID != "b" || e.Dst.ID != "c") {
			t.Fatal("removed the wrong link from a chain")
		}
	}
	text := d2format.Format(g.AST)
	if strings.Contains(text, "[-1]") || !strings.Contains(text, "(* -> *)[*].style.stroke-dash: 2") {
		t.Fatal(text)
	}
	g, err = d2oracle.Delete(g, nil, "f.two.(a -> b)[0]")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 3 {
		t.Fatal(d2format.Format(g.AST))
	}
}

func TestEditMaterializedSequenceMessages(t *testing.T) {
	const frame = `shape: edge-group; a -> b: First; a -> b: Second`
	for _, tc := range []struct {
		name, source string
		board        []string
	}{
		{"import", "shape: sequence-diagram\nf: @frame.d2\nh: @frame.d2", nil},
		{"variable", "vars: {frame: {" + frame + "}}\nshape: sequence-diagram\nf: ${frame}\nh: ${frame}", nil},
		{"spread", "vars: {frame: {" + frame + "}}\nshape: sequence-diagram\nf: {...${frame}}\nh: {...${frame}}", nil},
		{"scenario", "shape: sequence-diagram\nf: {" + frame + "}\nh: {" + frame + "}\nscenarios: {demo: {}}", []string{"demo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := fstest.MapFS{"frame.d2": {Data: []byte(frame)}}
			g, _, err := d2compiler.Compile("main.d2", strings.NewReader(tc.source), &d2compiler.CompileOptions{FS: fs})
			if err != nil {
				t.Fatal(err)
			}
			g.FS = fs
			board := d2oracle.GetBoardGraph(g, tc.board)
			var graphID string
			for _, e := range board.Edges {
				if d2format.Format(e.SequenceKey) == "h.(a -> b)[1]" {
					graphID = e.AbsID()
				}
			}
			if graphID == "" {
				t.Fatal("missing materialized source address")
			}
			val := "green"
			if tc.name == "variable" || tc.name == "spread" {
				before := d2format.Format(g.AST)
				if _, err := d2oracle.Set(g, tc.board, graphID+".style.stroke", nil, &val); !errors.As(err, &d2oracle.OutsideScopeError{}) {
					t.Fatalf("unexpected result of styling a template edge: %v", err)
				}
				if _, err := d2oracle.Delete(g, tc.board, graphID+".style.stroke"); !errors.As(err, &d2oracle.OutsideScopeError{}) {
					t.Fatalf("unexpected result of resetting a template edge style: %v", err)
				}
				if tc.name == "variable" {
					if _, err := d2oracle.Delete(g, tc.board, graphID); !errors.As(err, &d2oracle.OutsideScopeError{}) {
						t.Fatalf("unexpected result of deleting from a direct substitution: %v", err)
					}
					if got := d2format.Format(g.AST); got != before {
						t.Fatalf("refused edit changed source: %s", got)
					}
					return
				}
			} else {
				g, err = d2oracle.Set(g, tc.board, graphID+".style.stroke", nil, &val)
				if err != nil {
					t.Fatal(err)
				}
				board = d2oracle.GetBoardGraph(g, tc.board)
				for _, e := range board.Edges {
					if (e.Style.Stroke != nil) != (e.AbsID() == graphID) {
						t.Fatalf("styled wrong message: %s", d2format.Format(g.AST))
					}
				}
			}
			g, err = d2oracle.Delete(g, tc.board, graphID)
			if err != nil {
				t.Fatal(err)
			}
			board = d2oracle.GetBoardGraph(g, tc.board)
			if len(board.Edges) != 3 {
				t.Fatalf("expected three messages after deletion: %s", d2format.Format(g.AST))
			}
			last := d2format.Format(g.AST)
			if !strings.Contains(last, "h.(a -> b)[1]: null") {
				t.Fatal(last)
			}
			if tc.board != nil && len(g.Edges) != 4 {
				t.Fatal("editing inherited messages changed original board")
			}
		})
	}
}

func TestScopedSequenceEditingEntryPoints(t *testing.T) {
	const source = `shape: sequence-diagram
f: {shape: edge-group; a -> b: First}
h: {shape: edge-group; a -> b: Second}
c
`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	const target = "h.(a -> b)[0]"
	e := d2oracle.GetEdge(g, nil, target)
	if e == nil || e.Label.Value != "Second" {
		t.Fatal("missing scoped message")
	}
	stroke := "gold"
	g, err = d2oracle.Set(g, nil, target+".style.stroke", nil, &stroke)
	if err != nil {
		t.Fatal(err)
	}
	text := d2format.Format(g.AST)
	if !strings.Contains(text, "a -> b: Second {style.stroke: gold}") || strings.Contains(text, "[0]") {
		t.Fatal(text)
	}
	createG, createKey, err := d2oracle.Create(g, nil, "h.(a -> b)")
	if err != nil {
		t.Fatal(err)
	}
	if createKey != "h.(a -> b)[1]" || d2oracle.GetEdge(createG, nil, createKey) == nil {
		t.Fatalf("%s: %s", createKey, d2format.Format(createG.AST))
	}
	dst := "c"
	graphID := e.AbsID()
	a, err := d2oracle.ReconnectEdgeIDDeltas(g, nil, target, nil, &dst)
	if err != nil {
		t.Fatal(err)
	}
	b, err := d2oracle.ReconnectEdgeIDDeltas(g, nil, graphID, nil, &dst)
	if err != nil || !reflect.DeepEqual(a, b) || a[graphID] != "(a -> c)[0]" {
		t.Fatalf("reconnect deltas %v %v %v", a, b, err)
	}
	a, err = d2oracle.DeleteIDDeltas(g, nil, "f.(a -> b)[0]")
	if err != nil || a[graphID] != "(a -> b)[0]" {
		t.Fatalf("delete deltas %v %v", a, err)
	}
	g, err = d2oracle.ReconnectEdge(g, nil, target, nil, &dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := d2oracle.GetEdge(g, nil, "(a -> c)[0]"); got == nil || got.Label.Value != "Second" || got.Style.Stroke == nil || got.Style.Stroke.Value != "gold" {
		t.Fatal(d2format.Format(g.AST))
	}
}

func TestReconnectScopedSequenceIndexes(t *testing.T) {
	const source = `shape: sequence-diagram
f: {shape: edge-group; a -> c: Existing; a -> b: First; a -> b: Second; (b <- a)[1].style.stroke: gold; a -> c: Tail; (a -> c)[1].style.stroke: purple; (* -> *)[*].style.bold: true}
`
	for _, id := range []string{"f.(a -> b)[0]", "f.(b <- a)[1]"} {
		g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), nil)
		if err != nil {
			t.Fatal(err)
		}
		before := map[string]string{}
		for _, e := range g.Edges {
			before[e.Label.Value] = e.AbsID()
		}
		dst := "c"
		deltas, err := d2oracle.ReconnectEdgeIDDeltas(g, nil, id, nil, &dst)
		if err != nil {
			t.Fatal(err)
		}
		g, err = d2oracle.ReconnectEdge(g, nil, id, nil, &dst)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range g.Edges {
			expected := before[e.Label.Value]
			if d, ok := deltas[expected]; ok {
				expected = d
			}
			if e.AbsID() != expected {
				t.Fatalf("wrong delta for %s: expected %q, got %q", e.Label.Value, expected, e.AbsID())
			}
			want := map[string]string{"Tail": "purple", "Second": "gold"}[e.Label.Value]
			if want != "" && (e.Style.Stroke == nil || e.Style.Stroke.Value != want) {
				t.Fatalf("incorrect style on %s: %s", e.Label.Value, d2format.Format(g.AST))
			}
		}
		if !strings.Contains(d2format.Format(g.AST), "(* -> *)[*].style.bold: true") {
			t.Fatal(d2format.Format(g.AST))
		}
	}
}

func TestReconnectAcrossSequenceFrames(t *testing.T) {
	const source = `shape: sequence-diagram
f: {shape: edge-group; a -> c: Earlier}
a -> b: Moving
h: {shape: edge-group; a -> c: Latest}
`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	dst := "c"
	deltas, err := d2oracle.ReconnectEdgeIDDeltas(g, nil, "(a -> b)[0]", nil, &dst)
	if err != nil {
		t.Fatal(err)
	}
	g, err = d2oracle.ReconnectEdge(g, nil, "(a -> b)[0]", nil, &dst)
	if err != nil {
		t.Fatal(err)
	}
	e := d2oracle.GetEdge(g, nil, deltas["(a -> b)[0]"])
	if len(deltas) != 1 || e == nil || e.Label.Value != "Moving" {
		t.Fatalf("incorrect deltas %v for %s", deltas, d2format.Format(g.AST))
	}
}
