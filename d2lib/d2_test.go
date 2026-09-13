package d2lib

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/lib/geo"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestParseAndCompileHonorCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Parse(ctx, "x", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Parse() error = %v, want context.Canceled", err)
	}
	if _, _, err := Compile(ctx, "x", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile() error = %v, want context.Canceled", err)
	}
}

func TestCompilePropagatesVariableExpansionLimit(t *testing.T) {
	_, _, err := Compile(context.Background(), "vars: {x: 12345678}\nout: ${x}${x}${x}${x}${x}", &CompileOptions{
		MaxVariableExpansion: 32,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "variable substitution expansion exceeds limit of 32 work units") {
		t.Fatalf("Compile() error = %v, want variable expansion limit", err)
	}
}

func TestCompilePropagatesGlobExpansionLimit(t *testing.T) {
	_, _, err := Compile(context.Background(), "**.a\n**.b\n**.c\nx\n", &CompileOptions{
		MaxGlobExpansion: 64,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "glob expansion exceeds limit of 64 work units") {
		t.Fatalf("Compile() error = %v, want glob expansion limit", err)
	}
}

func TestCompilePropagatesEdgeExpansionLimit(t *testing.T) {
	_, _, err := Compile(context.Background(), "a\nb\nc\n* -> *\n", &CompileOptions{
		MaxEdgeExpansion: 8,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "edge glob expansion exceeds limit of 8 endpoint pairs") {
		t.Fatalf("Compile() error = %v, want edge expansion limit", err)
	}
}

func TestCompilePropagatesEdgeExpansionWorkLimit(t *testing.T) {
	_, _, err := Compile(context.Background(), "(* -> *)[*].style.opacity: 0\na\nb\nc\n", &CompileOptions{
		MaxEdgeExpansion:     9,
		MaxEdgeExpansionWork: 9,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "work limit of 9 endpoint-pair examinations") {
		t.Fatalf("Compile() error = %v, want edge expansion work limit", err)
	}
}

func TestNilFSDeniesImports(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "secret.d2"), []byte("disclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Compile(context.Background(), "...@secret", &CompileOptions{
		InputPath: filepath.Join(directory, "index.d2"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "imports are disabled") {
		t.Fatalf("Compile error = %v, want imports-disabled error", err)
	}
}

func TestGetLayoutDoesNotUseEnvironmentFallback(t *testing.T) {
	t.Setenv("D2_LAYOUT", "dagre")

	_, err := getLayout(&CompileOptions{})
	if err == nil || err.Error() != "no available layout" {
		t.Fatalf("getLayout() error = %v, want no available layout", err)
	}
}

func TestCompileRequiresLayoutResolver(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = Compile(context.Background(), "x", &CompileOptions{Ruler: ruler}, nil)
	const want = `no layout resolver configured for layout engine "dagre"`
	if err == nil || err.Error() != want {
		t.Fatalf("Compile() error = %v, want %q", err, want)
	}
}

func TestCompilePropagatesIndependentConfigDataToEveryBoard(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}

	input := `vars: {
  d2-config: {
    layout-engine: tala
    data: {
      tala-seeds: [7; 11]
    }
  }
}
root
layers: {
  layer: { layer-object }
}
scenarios: {
  scenario: { scenario-object }
}
steps: {
  step: { step-object }
}
`

	var seen [][]interface{}
	opts := &CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(engine string) (d2graph.LayoutGraph, error) {
			if engine != "tala" {
				t.Fatalf("layout engine = %q, want tala", engine)
			}
			return func(_ context.Context, graph *d2graph.Graph) error {
				seeds := graph.Data["tala-seeds"].([]interface{})
				seen = append(seen, append([]interface{}(nil), seeds...))
				// Mutating one board's layout data must not affect later boards.
				seeds[0] = "mutated"
				graph.Data["board-local"] = true
				for i, object := range graph.Objects {
					object.TopLeft = geo.NewPoint(float64(i*100), 0)
				}
				return nil
			}, nil
		},
	}
	ctx := d2log.WithDefault(context.Background())
	if _, _, err := Compile(ctx, input, opts, nil); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 {
		t.Fatalf("boards receiving config data = %d, want 4", len(seen))
	}
	want := []interface{}{"7", "11"}
	for i, seeds := range seen {
		if !reflect.DeepEqual(seeds, want) {
			t.Fatalf("board %d tala-seeds = %#v, want %#v", i, seeds, want)
		}
	}
}

func TestCompileTALAConfigSeedsInNestedLayouts(t *testing.T) {
	const nodes = `a -> b -> c
a -> d -> e
b -> e
d -> c
f -> e
g -> b
`
	const config = `vars: {d2-config: {layout-engine: tala; data: {tala-seeds: [7]}}}
`
	for _, tc := range []struct{ name, source string }{
		{"plain", nodes},
		{"grid-cell", "grid: {grid-columns: 1\ncell: {\n" + nodes + "}\n}\n"},
		{"batched-grids", "grid1: {grid-columns: 1\ncell: {\n" + nodes + "}\n}\ngrid2: {grid-columns: 1\ncell: {\n" + nodes + "}\n}\n"},
		{"constant-near", "main\nlegend: {near: top-left\n" + nodes + "}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ruler, err := textmeasure.NewRuler()
			if err != nil {
				t.Fatal(err)
			}
			ctx := d2log.WithDefault(t.Context())
			opts := &CompileOptions{
				Ruler: ruler,
				RouterResolver: func(string) (d2graph.RouteEdges, error) {
					return d2talalayout.RouteEdges, nil
				},
			}
			setSeeds := func(seed int64) {
				opts.LayoutResolver = func(string) (d2graph.LayoutGraph, error) {
					return func(ctx context.Context, g *d2graph.Graph) error {
						return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{seed}, MaxConcurrency: 1})
					}, nil
				}
			}
			setSeeds(7)
			want, _, err := Compile(ctx, config+tc.source, opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			// Source seeds must override the supplied options in every core
			// layout call, including graphs extracted for nested diagrams.
			setSeeds(1)
			got, _, err := Compile(ctx, config+tc.source, opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("layout changed when source seeds override the supplied options")
			}
		})
	}
}
