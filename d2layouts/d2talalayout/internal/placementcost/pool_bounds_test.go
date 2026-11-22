package placementcost

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// TestPooledScoringScratchIsBounded checks that edge-length and symmetry
// scratch grown past their bounds is not returned to its pool (TinyGo's
// sync.Pool never frees). The edge scratch's sweep obstacles were bounded
// per edge only, so one scratch could keep 4096 edges x 4096 obstacles.
func TestPooledScoringScratchIsBounded(t *testing.T) {
	small := scratchPool.Get()
	small.sweepEdges = make([]sweepEdgeObstacles, 2, 8)
	small.sweepEdges[0].obstacles = make([]sweepObstacle, 0, 100)
	if !small.retainable() {
		t.Fatal("small edge scratch not retainable")
	}
	putEdgeScratch(small)

	for name, grow := range map[string]func(*edgeScratch){
		"sweep obstacles of all edges": func(s *edgeScratch) {
			// Each edge within the old per-edge cap, together beyond the bound.
			s.sweepEdges = make([]sweepEdgeObstacles, 3)
			for i := range s.sweepEdges {
				s.sweepEdges[i].obstacles = make([]sweepObstacle, 0, maxPooledEdgeScratchEntries/2)
			}
		},
		"sweep obstacles beyond the length": func(s *edgeScratch) {
			s.sweepEdges = make([]sweepEdgeObstacles, 1, 3)
			full := s.sweepEdges[:3]
			for i := range full {
				full[i].obstacles = make([]sweepObstacle, 0, maxPooledEdgeScratchEntries/2)
			}
		},
		"used": func(s *edgeScratch) { s.used = make([]bool, maxPooledEdgeScratchEntries+1) },
		"node replacements": func(s *edgeScratch) {
			s.nRepl = make([]*layoutgraph.Node, maxPooledEdgeScratchEntries+1)
		},
		"obstruction keys": func(s *edgeScratch) {
			s.obstructionKeys = make([]*layoutgraph.Node, 0, maxPooledEdgeScratchEntries+1)
		},
	} {
		s := scratchPool.Get()
		grow(s)
		if s.retainable() {
			t.Fatalf("%s: oversized edge scratch retainable", name)
		}
		putEdgeScratch(s)
		for i := 0; i < 4; i++ {
			if scratchPool.Get() == s {
				t.Fatalf("%s: oversized edge scratch returned to the pool", name)
			}
		}
	}

	ws := getSymmetryWorkspace()
	for depth := 0; depth < 5; depth++ {
		frame := ws.push(maxPooledSymmetryFrameEntries)
		frame.children = make([]*layoutgraph.Node, 0, maxPooledSymmetryFrameEntries)
	}
	if ws.retainable() {
		t.Fatal("oversized symmetry workspace retainable")
	}
	putSymmetryWorkspace(ws)
	for i := 0; i < 4; i++ {
		if symmetryWorkspacePool.Get() == ws {
			t.Fatal("oversized symmetry workspace returned to the pool")
		}
	}
	ws = getSymmetryWorkspace()
	ws.push(8)
	ws.pop()
	if !ws.retainable() {
		t.Fatal("small symmetry workspace not retainable")
	}
	putSymmetryWorkspace(ws)
}
