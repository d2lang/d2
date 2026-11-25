package d2scenebuild_test

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/internal/testlog"
	d2log "github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

// The raster exports (PNG, GIF, PDF, PPTX) draw their appendix from the scene
// built here, so this covers the PNG path of the mixed link and tooltip case.
const mixedLinkTooltipScript = `views: {
  label: Views
  shape: page
  style.multiple: true
  one: One {link: scenarios.one; shape: step}
  two: Two {link: scenarios.two; shape: step}
  three: Three {link: scenarios.three; shape: step}
}
c: {label: C; tooltip: "tooltip for C"}
d: {label: D; tooltip: "tooltip for D"}
c -- d
scenarios: {
  one: {
    views.one.link: _
  }
}
`

var (
	sceneMarkerIDPattern = regexp.MustCompile(`^appendix:shape:\d+:(tooltip|link)$`)
	sceneRowIDPattern    = regexp.MustCompile(`^appendix:row:\d+$`)
)

func TestAppendixMixedLinkTooltipNumberingScene(t *testing.T) {
	t.Parallel()

	ctx := d2log.Leveled(d2log.With(context.Background(), testlog.New(t)), slog.LevelDebug)
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	diagram, _, err := d2lib.Compile(ctx, mixedLinkTooltipScript, &d2lib.CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2dagrelayout.DefaultLayout, nil
		},
	}, &d2svg.RenderOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagram.Scenarios) != 1 {
		t.Fatalf("scenarios = %d, want 1", len(diagram.Scenarios))
	}

	// The CLI rewrites board links to relative output paths before rendering.
	relinked := *diagram.Scenarios[0]
	relinked.Shapes = append([]d2target.Shape(nil), relinked.Shapes...)
	for i := range relinked.Shapes {
		if relinked.Shapes[i].Link != "" {
			relinked.Shapes[i].Link = "index.png"
		}
	}

	for _, tc := range []struct {
		name     string
		diagram  *d2target.Diagram
		wantRows []string
	}{
		{"root", diagram, []string{"one", "tooltip for C", "tooltip for D"}},
		{"scenarios.one", diagram.Scenarios[0], []string{"root", "tooltip for C", "tooltip for D"}},
		{"scenarios.one relinked", &relinked, []string{"index.png", "tooltip for C", "tooltip for D"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Mirror the CLI: each board is built on its own.
			board := *tc.diagram
			board.Layers, board.Scenarios, board.Steps = nil, nil, nil
			document, err := d2scenebuild.Build(ctx, &board, d2scenebuild.Options{
				Appendix:   true,
				LinkBudget: d2scenebuild.LinkBudget{MaxRegions: 64, MaxStringBytes: 4_096},
			})
			if err != nil {
				t.Fatal(err)
			}
			var markers, rows []int
			var rowTexts []string
			var walk func(*d2scene.Node)
			walk = func(node *d2scene.Node) {
				switch {
				case sceneMarkerIDPattern.MatchString(node.ID):
					markers = append(markers, sceneIconNumber(t, node))
				case sceneRowIDPattern.MatchString(node.ID):
					rows = append(rows, sceneIconNumber(t, node.Children[0]))
					var lines []string
					for _, child := range node.Children[1:] {
						lines = append(lines, child.Primitive.(d2scene.TextRun).Text)
					}
					rowTexts = append(rowTexts, strings.Join(lines, "\n"))
				}
				for _, child := range node.Children {
					walk(child)
				}
			}
			walk(document.Root)

			for i, n := range rows {
				if n != i+1 {
					t.Fatalf("appendix row numbers = %v, want 1..%d in order", rows, len(rows))
				}
			}
			seen := map[int]bool{}
			for _, n := range markers {
				if seen[n] {
					t.Fatalf("diagram marker numbers = %v, number %d is duplicated", markers, n)
				}
				seen[n] = true
				if n < 1 || n > len(rows) {
					t.Fatalf("diagram marker %d has no appendix row (markers %v, rows %v)", n, markers, rows)
				}
			}
			if len(markers) != len(rows) {
				t.Fatalf("diagram markers %v and appendix rows %v differ in count", markers, rows)
			}
			if strings.Join(rowTexts, "|") != strings.Join(tc.wantRows, "|") {
				t.Fatalf("appendix rows = %q, want %q", rowTexts, tc.wantRows)
			}
		})
	}
}

func sceneIconNumber(t *testing.T, icon *d2scene.Node) int {
	t.Helper()
	if len(icon.Children) != 2 {
		t.Fatalf("appendix icon %q has %d children, want circle and number", icon.ID, len(icon.Children))
	}
	run, ok := icon.Children[1].Primitive.(d2scene.TextRun)
	if !ok {
		t.Fatalf("appendix icon %q number = %#v", icon.ID, icon.Children[1].Primitive)
	}
	n, err := strconv.Atoi(run.Text)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
