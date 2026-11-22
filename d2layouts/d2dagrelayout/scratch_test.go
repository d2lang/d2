package d2dagrelayout

import (
	"context"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/textmeasure"
)

// TestScratchReleaseDropsLongLists checks that the scratch kept between calls
// does not keep a cross-rank list that grew longer than scratchMaxList: the
// queue holds an object once for every time it is queued in a shift, so its
// length is not bounded by the number of objects.
func TestScratchReleaseDropsLongLists(t *testing.T) {
	t.Parallel()
	s := new(layoutScratch)
	s.queue = make([]int32, 0, scratchMaxList+1)
	s.seenOrder = make([]int32, 0, scratchMaxList+1)
	s.movedOrder = make([]int32, 0, scratchMaxList+1)
	s.increased = make([]int32, 0, scratchMaxList+1)
	s.crossBlock = make([]int32, 0, 100)
	if !s.release() {
		t.Fatal("release dropped a scratch of a small graph")
	}
	if s.queue != nil || s.seenOrder != nil || s.movedOrder != nil || s.increased != nil {
		t.Fatalf("kept lists of capacity %d, %d, %d, %d", cap(s.queue), cap(s.seenOrder), cap(s.movedOrder), cap(s.increased))
	}
	if cap(s.crossBlock) != 100 {
		t.Fatal("dropped a buffer sized by the graph")
	}

	s.queue = make([]int32, 0, scratchMaxList)
	if !s.release() || cap(s.queue) != scratchMaxList {
		t.Fatal("dropped a queue within the bound")
	}
}

// TestPutScratchBoundsKeptStorage lays out graphs of the size the layout
// guard admits and checks the scratch Layout keeps afterwards: at most 2 MB,
// and no pointer to anything of the caller's graph. It is not parallel: it
// inspects scratchCache, which the other tests of the package use.
func TestPutScratchBoundsKeptStorage(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, source string
	}{
		{"cross-rank chain", benchmarkCrossRankSource(500)},
		{"nested", benchmarkNestedSource(4, 5)},
	} {
		g := prepareBenchmarkGraph(t, tc.source, ruler)
		if err := DefaultLayout(context.Background(), g); err != nil {
			t.Fatal(err)
		}
		s := scratchCache.Load()
		if s == nil {
			t.Fatalf("%s: no scratch kept", tc.name)
		}
		graph := graphPointers(g)
		w := scratchWalk{arrays: map[uintptr]int64{}, graph: graph}
		w.walk(reflect.ValueOf(s).Elem(), "scratch")
		if w.leak != "" {
			t.Fatalf("%s: the kept scratch holds %s", tc.name, w.leak)
		}
		total := int64(0)
		for _, n := range w.arrays {
			total += n
		}
		t.Logf("%s: %d objects, %d edges; the kept scratch holds %d bytes", tc.name, len(g.Objects), len(g.Edges), total)
		if total > 2<<20 {
			t.Fatalf("%s: the kept scratch holds %d bytes", tc.name, total)
		}
	}
}

// graphPointers returns the addresses of g's objects, edges, boxes and points.
func graphPointers(g *d2graph.Graph) map[uintptr]bool {
	ptrs := map[uintptr]bool{}
	add := func(v any) { ptrs[reflect.ValueOf(v).Pointer()] = true }
	add(g)
	for _, obj := range append([]*d2graph.Object{g.Root}, g.Objects...) {
		add(obj)
		if obj.Box != nil {
			add(obj.Box)
			if obj.TopLeft != nil {
				add(obj.TopLeft)
			}
		}
	}
	for _, e := range g.Edges {
		add(e)
		for _, p := range e.Route {
			add(p)
		}
	}
	return ptrs
}

// scratchWalk visits everything reachable from the scratch through slices,
// structs, pointers, interfaces and maps, recording the size of every slice's
// backing array and the first pointer into the caller's graph it finds.
type scratchWalk struct {
	arrays map[uintptr]int64
	graph  map[uintptr]bool
	seen   map[uintptr]bool
	leak   string
}

func (w *scratchWalk) walk(v reflect.Value, path string) {
	if w.leak != "" {
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.UnsafePointer:
		if v.IsNil() {
			return
		}
		p := v.Pointer()
		if w.graph[p] {
			w.leak = path
			return
		}
		if w.seen == nil {
			w.seen = map[uintptr]bool{}
		}
		if w.seen[p] {
			return
		}
		w.seen[p] = true
		w.walk(v.Elem(), path+".*")
	case reflect.Interface:
		if !v.IsNil() {
			w.walk(v.Elem(), path+".(any)")
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			w.walk(v.Field(i), path+"."+v.Type().Field(i).Name)
		}
	case reflect.Slice:
		if v.Cap() == 0 {
			return
		}
		full := v.Slice(0, v.Cap())
		p := full.Pointer()
		if w.graph[p] {
			w.leak = path
			return
		}
		size := int64(v.Cap()) * int64(v.Type().Elem().Size())
		if size > w.arrays[p] {
			w.arrays[p] = size
		}
		for i := 0; i < full.Len(); i++ {
			w.walk(full.Index(i), path+"[]")
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			w.walk(v.Index(i), path+"[]")
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			w.walk(iter.Key(), path+"{key}")
			w.walk(iter.Value(), path+"{}")
		}
	}
}
