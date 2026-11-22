package routing

import (
	"context"
	"math/rand"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// The OVG port index numbered its owners, and charged its axis sort work,
// in map order, and merged hierarchy ports in map order: the units charged
// (and where a limit failed) varied from run to run. These tests repeat the builds on fresh maps.

func portOrderOwners(seed int64) map[*layoutgraph.Node][]*OVGNode {
	rng := rand.New(rand.NewSource(seed))
	ports := map[*layoutgraph.Node][]*OVGNode{}
	for i := 0; i < 24; i++ {
		owner := layoutgraph.NewNode(layoutgraph.EntityID(rng.Intn(1000)), 40, 30)
		owner.TopLeft = geo.NewPoint(float64(rng.Intn(20)*50), float64(rng.Intn(20)*50))
		var nodePorts []*OVGNode
		for k := 0; k < 1+rng.Intn(6); k++ {
			nodePorts = append(nodePorts, NewOVGNode(geo.NewPoint(owner.TopLeft.X+float64(10*k), owner.TopLeft.Y+float64(rng.Intn(3)*15))))
		}
		ports[owner] = nodePorts
	}
	return ports
}

func TestPortIndexDoesNotDependOnMapOrder(t *testing.T) {
	ctx := context.Background()
	for seed := int64(0); seed < 20; seed++ {
		// The same owners and ports in fresh maps (each with its own order).
		base := portOrderOwners(seed)
		var owners []*layoutgraph.Node
		for owner := range base {
			owners = append(owners, owner)
		}
		var wantOwners []*layoutgraph.Node
		var wantErr string
		var wantWork uint64
		for run := 0; run < 10; run++ {
			ports := make(map[*layoutgraph.Node][]*OVGNode, len(base))
			for _, i := range rand.New(rand.NewSource(int64(run))).Perm(len(owners)) {
				ports[owners[i]] = base[owners[i]]
			}
			guard, err := newOVGBuildGuard(ctx, defaultOVGBuildLimits())
			if err != nil {
				t.Fatal(err)
			}
			index, err := newOVGPortIndex(ports, nil, nil, guard)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.IsSortedFunc(index.owners, comparePortOwners) {
				t.Fatalf("seed %d: owners not in their deterministic order", seed)
			}
			// A work limit that fails among the axis sort charges.
			limits := defaultOVGBuildLimits()
			limits.work = guard.work - 7
			limited, err := newOVGBuildGuard(ctx, limits)
			if err != nil {
				t.Fatal(err)
			}
			_, limitErr := newOVGPortIndex(ports, nil, nil, limited)
			if limitErr == nil {
				t.Fatalf("seed %d: no limit error", seed)
			}
			if run == 0 {
				wantOwners, wantErr, wantWork = index.owners, limitErr.Error(), guard.work
				continue
			}
			if !slices.Equal(index.owners, wantOwners) || limitErr.Error() != wantErr || guard.work != wantWork {
				t.Fatalf("seed %d run %d: index or limit error differs (%q, first %q)", seed, run, limitErr.Error(), wantErr)
			}
		}
	}
}

func TestMergePortsDoesNotDependOnMapOrder(t *testing.T) {
	ctx := context.Background()
	for seed := int64(0); seed < 20; seed++ {
		base := portOrderOwners(seed)
		var owners []*layoutgraph.Node
		for owner := range base {
			owners = append(owners, owner)
		}
		var want []geo.Point
		for run := 0; run < 10; run++ {
			other := NewOVG(nil)
			for _, i := range rand.New(rand.NewSource(int64(run))).Perm(len(owners)) {
				other.Ports[owners[i]] = base[owners[i]]
			}
			guard, err := newOVGBuildGuard(ctx, defaultOVGBuildLimits())
			if err != nil {
				t.Fatal(err)
			}
			ovg := newBuildOVG(nil, guard)
			if err := ovg.mergePorts(other, guard); err != nil {
				t.Fatal(err)
			}
			var points []geo.Point
			for _, node := range ovg.Nodes {
				points = append(points, *node.Point)
			}
			if run == 0 {
				want = points
				continue
			}
			if !slices.Equal(points, want) {
				t.Fatalf("seed %d run %d: merged OVG nodes in a different order", seed, run)
			}
		}
	}
}
