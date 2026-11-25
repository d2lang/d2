package placement

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
)

// TestQuietSkipsMatchFullSearch places random graphs (containers, hubs,
// clusters, labels) three ways: with the score memo off (every swap search
// and transpose runs), with the memo on and the swap searches and transposes
// known to find nothing skipped, and with the memo on and every skipped one
// run anyway and checked (verify). Geometry and errors must be identical, the
// checks must find no skipped search that would have found a swap and no
// skipped transpose that would have succeeded or changed the graph, and both
// kinds of skip must occur.
func TestQuietSkipsMatchFullSearch(t *testing.T) {
	skipSlowUnderRace(t, 57)
	skipping := scoreMemoStats{}
	verified := scoreMemoStats{verify: true}
	placed := 0
	for trial := 0; trial < 20; trial++ {
		seed := int64(900 + trial)
		run := func(stats *scoreMemoStats) (string, string) {
			g := randomPlaceGraph(rand.New(rand.NewSource(seed)))
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), scoreMemoContextKey{}, stats))
			defer cancel()
			if err := prepareForPlacement(ctx, g, seed); err != nil {
				return "prepare " + err.Error(), ""
			}
			err := Place(ctx, g, seed)
			return fmt.Sprint(err), placedGeometry(g)
		}
		wantErr, want := run(&scoreMemoStats{off: true})
		for _, stats := range []*scoreMemoStats{&skipping, &verified} {
			gotErr, got := run(stats)
			if gotErr != wantErr || got != want {
				t.Fatalf("trial %d (verify %v): with skips (%s)\n%s\nwithout (%s)\n%s", trial, stats.verify, gotErr, got, wantErr, want)
			}
		}
		if wantErr == "<nil>" {
			placed++
		}
	}
	t.Logf("%d placed; skipped swap searches %d, transposes %d; verified %d and %d", placed, skipping.quietSwaps, skipping.quietTransposes, verified.quietSwaps, verified.quietTransposes)
	if verified.mismatches != 0 {
		t.Fatalf("%d wrong skips or memo hits; first: %s", verified.mismatches, verified.firstMismatch)
	}
	if placed == 0 || skipping.quietSwaps == 0 || skipping.quietTransposes == 0 || verified.quietSwaps != skipping.quietSwaps || verified.quietTransposes != skipping.quietTransposes {
		t.Errorf("placed %d; skips %d/%d, verified %d/%d", placed, skipping.quietSwaps, skipping.quietTransposes, verified.quietSwaps, verified.quietTransposes)
	}
}

// TestIdleLevelsMatchFullPasses places random graphs (isolated nodes and
// containers give levels whose nodes the optimizers may not move) with and
// without skipping those levels' optimizer passes: geometry and errors must
// be identical.
func TestIdleLevelsMatchFullPasses(t *testing.T) {
	skipSlowUnderRace(t, 50)
	defer func() { skipIdlePasses = true }()
	placed, isolated := 0, 0
	for trial := 0; trial < 30; trial++ {
		seed := int64(1300 + trial)
		run := func(skip bool) (string, string) {
			skipIdlePasses = skip
			random := rand.New(rand.NewSource(seed))
			g := randomPlaceGraph(random)
			if skip {
				for _, n := range g.Nodes {
					if len(n.Edges) == 0 && len(n.Nears) == 0 {
						isolated++
					}
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := prepareForPlacement(ctx, g, seed); err != nil {
				return "prepare " + err.Error(), ""
			}
			err := Place(ctx, g, seed)
			return fmt.Sprint(err), placedGeometry(g)
		}
		wantErr, want := run(false)
		gotErr, got := run(true)
		if gotErr != wantErr || got != want {
			t.Fatalf("trial %d: skipping idle passes (%s)\n%s\nrunning them (%s)\n%s", trial, gotErr, got, wantErr, want)
		}
		if wantErr == "<nil>" {
			placed++
		}
	}
	t.Logf("%d placed; %d isolated nodes (single-node levels)", placed, isolated)
	if placed == 0 || isolated == 0 {
		t.Fatalf("placed %d graphs with %d isolated nodes", placed, isolated)
	}
}

// TestKnownNoOpCompactionsMatchFullSchedule places random graphs with every
// compaction run, with the compactions known to change nothing skipped, and
// with those run anyway and checked (verify): geometry and errors must be
// identical, no skipped compaction may change the level, and some must be
// skipped.
func TestKnownNoOpCompactionsMatchFullSchedule(t *testing.T) {
	skipSlowUnderRace(t, 66)
	defer func() { skipKnownNoOpCompactions = true }()
	skipping := scoreMemoStats{}
	verified := scoreMemoStats{verify: true}
	for trial := 0; trial < 20; trial++ {
		seed := int64(1700 + trial)
		run := func(skip bool, stats *scoreMemoStats) (string, string) {
			skipKnownNoOpCompactions = skip
			g := randomPlaceGraph(rand.New(rand.NewSource(seed)))
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), scoreMemoContextKey{}, stats))
			defer cancel()
			if err := prepareForPlacement(ctx, g, seed); err != nil {
				return "prepare " + err.Error(), ""
			}
			err := Place(ctx, g, seed)
			return fmt.Sprint(err), placedGeometry(g)
		}
		wantErr, want := run(false, &scoreMemoStats{})
		for _, stats := range []*scoreMemoStats{&skipping, &verified} {
			gotErr, got := run(true, stats)
			if gotErr != wantErr || got != want {
				t.Fatalf("trial %d (verify %v): with skips (%s)\n%s\nwithout (%s)\n%s", trial, stats.verify, gotErr, got, wantErr, want)
			}
		}
	}
	t.Logf("compactions skipped %d, verified %d", skipping.compactionSkips, verified.compactionSkips)
	if verified.mismatches != 0 {
		t.Fatalf("%d wrong skips; first: %s", verified.mismatches, verified.firstMismatch)
	}
	if skipping.compactionSkips == 0 || verified.compactionSkips != skipping.compactionSkips {
		t.Fatalf("skipped %d, verified %d", skipping.compactionSkips, verified.compactionSkips)
	}
}
