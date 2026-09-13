package d2elklayout

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
)

func TestLayoutRejectsDenseGraphBeforeELK(t *testing.T) {
	t.Parallel()
	g := d2graph.NewGraph()
	for i := 0; i < 20; i++ {
		g.Objects = append(g.Objects, &d2graph.Object{Graph: g, Parent: g.Root})
	}
	for src := range g.Objects {
		for dst := range g.Objects {
			if src != dst {
				g.Edges = append(g.Edges, &d2graph.Edge{Src: g.Objects[src], Dst: g.Objects[dst]})
			}
		}
	}
	err := DefaultLayout(context.Background(), g)
	if err == nil || !strings.Contains(err.Error(), "layout graph is too interconnected") {
		t.Fatalf("DefaultLayout error = %v, want interaction-work limit", err)
	}
}

func TestLayoutChecksCancellationBeforeELK(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := DefaultLayout(ctx, &d2graph.Graph{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("DefaultLayout error = %v, want context.Canceled", err)
	}
}
