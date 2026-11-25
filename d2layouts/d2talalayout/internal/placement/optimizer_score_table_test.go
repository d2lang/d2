package placement

import (
	"context"
	"math/rand"
	"runtime"
	"sync"
	"testing"
)

// scoreTableCacheContents lists the cached tables.
func scoreTableCacheContents() []*optimizerScoreTable {
	cache := &scoreTableCache
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	var tables []*optimizerScoreTable
	for k := range cache.tables {
		for _, table := range cache.tables[k] {
			if table != nil {
				tables = append(tables, table)
			}
		}
	}
	return tables
}

// checkScoreTableCache fails unless the cache holds at most
// maxCachedScoreTablesPerSize cleared tables of each size up to
// maxCachedScoreTableSlots slots, none of them twice.
func checkScoreTableCache(t *testing.T, when string) (tables, slots int) {
	t.Helper()
	seen := map[*optimizerScoreTable]bool{}
	perSize := map[int]int{}
	for _, table := range scoreTableCacheContents() {
		perSize[len(table.slots)]++
		if perSize[len(table.slots)] > maxCachedScoreTablesPerSize {
			t.Fatalf("%s: more than %d tables of %d slots cached", when, maxCachedScoreTablesPerSize, len(table.slots))
		}
		if seen[table] {
			t.Fatalf("%s: a table is cached twice", when)
		}
		seen[table] = true
		tables++
		slots += len(table.slots)
		if len(table.slots) < minScoreTableSlots || len(table.slots) > maxCachedScoreTableSlots {
			t.Fatalf("%s: a %d-slot table is cached (at most %d)", when, len(table.slots), maxCachedScoreTableSlots)
		}
		for i := range table.slots {
			if table.slots[i] != (optimizerScoreSlot{}) {
				t.Fatalf("%s: a cached %d-slot table is not cleared at slot %d", when, len(table.slots), i)
			}
		}
	}
	if most := maxCachedScoreTablesPerSize * (minScoreTableSlots<<scoreTableSizes - minScoreTableSlots); slots > most || maxCachedScoreTableSlots != minScoreTableSlots<<(scoreTableSizes-1) {
		t.Fatalf("%s: %d slots cached (at most %d)", when, slots, most)
	}
	return tables, slots
}

// TestScoreTableCacheBounded is a regression test:
// score memo tables were handed back to per-size sync.Pools whatever their
// size, and TinyGo's sync.Pool never frees anything, so d2's WebAssembly build
// kept every table it ever released. Releasing tables of every size, many
// times, must leave at most maxCachedScoreTablesPerSize cleared tables of each
// size up to maxCachedScoreTableSlots slots, by construction (no garbage
// collection involved); a table above that size is never kept; a kept table
// comes back for its size.
func TestScoreTableCacheBounded(t *testing.T) {
	var big []*optimizerScoreTable
	for size := minScoreTableSlots; size <= minScoreTableSlots<<9; size *= 2 {
		for range 5 {
			table := getScoreTable(size)
			if len(table.slots) != size {
				t.Fatalf("getScoreTable(%d) returned %d slots", size, len(table.slots))
			}
			for i := range table.slots {
				table.slots[i].version = 7
				table.slots[i].score = 1.5
			}
			if size > maxCachedScoreTableSlots {
				big = append(big, table)
			}
			putScoreTable(table)
			checkScoreTableCache(t, "after a release")
		}
	}
	for _, table := range scoreTableCacheContents() {
		for _, b := range big {
			if table == b {
				t.Fatalf("a %d-slot table is cached", len(b.slots))
			}
		}
	}
	tables, slots := checkScoreTableCache(t, "after every size")
	runtime.GC()
	runtime.GC()
	if after, _ := checkScoreTableCache(t, "after two collections"); after != tables {
		t.Fatalf("collections changed the cache: %d tables, then %d", tables, after)
	}
	t.Logf("cached after releasing 5 tables of each size up to %d slots: %d tables, %d slots (%d KiB)", minScoreTableSlots<<9, tables, slots, slots*88/1024)
	cached := scoreTableCacheContents()
	if len(cached) == 0 {
		t.Fatal("nothing is cached")
	}
	want := cached[0]
	if got := getScoreTable(len(want.slots)); got != want {
		putScoreTable(got)
		t.Fatalf("getScoreTable(%d) did not return the cached table", len(want.slots))
	}
	putScoreTable(want)
}

// TestScoreTableCacheAfterPlacementsAndConcurrentUse places random graphs on
// several goroutines at once (as d2's concurrent seeds do) and checks the
// cache's bounds after every placement and at the end.
func TestScoreTableCacheAfterPlacementsAndConcurrentUse(t *testing.T) {
	var wait sync.WaitGroup
	for worker := range 3 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for trial := range 4 {
				seed := int64(300 + 10*worker + trial)
				g := randomPlaceGraph(rand.New(rand.NewSource(seed)))
				ctx, cancel := context.WithCancel(context.Background())
				if err := prepareForPlacement(ctx, g, seed); err == nil {
					_ = Place(ctx, g, seed)
				}
				cancel()
			}
		}()
	}
	wait.Wait()
	tables, slots := checkScoreTableCache(t, "after placements")
	t.Logf("cached after placements: %d tables, %d slots (%d KiB)", tables, slots, slots*88/1024)
}
