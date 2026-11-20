package d2isometricimg

import (
	"fmt"
	"math"
	"math/bits"
	"slices"
	"sync"
	"unsafe"
)

type svgVisibleFragment struct {
	points []svgPoint
	box    svgBox
	// stamp is the last fragment-set query that examined this fragment. It
	// sits beside the bounds so a query touches one cache line per fragment.
	stamp  uint32
	pooled bool // points came from the worker's pool rather than the scene.
}

// The visible pieces of a broad board can number in the thousands. Index
// those pieces as they change so a small node or ink ribbon only visits the
// local pieces it can cut, rather than scanning every existing fragment.
//
// Fragments are numbered from one in creation order and stored by that number.
// A removed fragment keeps its slot with nil points; cells drop it the next
// time they are visited, while live counts stay exact for work accounting.
// When removed fragments far outnumber live ones, compact renumbers the
// survivors, so the set stays proportional to what is alive.
type svgVisibleFragmentSet struct {
	grid                                      svgVisibilityGrid
	cells                                     [][]int32
	cellLive                                  []int32
	large                                     []int32
	largeLive                                 int
	fragments                                 []svgVisibleFragment
	live, vertices, maxFragments, maxVertices int
	budget                                    *svgVisibilityBudget
	pool                                      *svgPointPool
	references                                int
	stamp                                     uint32 // Number of the current query.
	ids                                       []int32
}

// A fragment grid has at most this many cells along either axis.
const svgFragmentGridSide = 32

// reset empties the set and sizes its grid for a subject with the given bounds.
func (s *svgVisibleFragmentSet) reset(box svgBox, budget *svgVisibilityBudget, pool *svgPointPool, maxFragments, maxVertices int) {
	w, h := box.maxX-box.minX, box.maxY-box.minY
	nx := max(1, min(svgFragmentGridSide, int(math.Ceil(math.Sqrt(256*w/h)))))
	ny := max(1, min(svgFragmentGridSide, int(math.Ceil(256/float64(nx)))))
	s.grid = svgVisibilityGrid{bounds: box, nx: nx, ny: ny}
	if s.cells == nil {
		s.cells = make([][]int32, svgFragmentGridSide*svgFragmentGridSide)
		s.cellLive = make([]int32, svgFragmentGridSide*svgFragmentGridSide)
	}
	s.cells, s.cellLive = s.cells[:nx*ny], s.cellLive[:nx*ny]
	for i := range s.cells {
		s.cells[i] = s.cells[i][:0]
	}
	clear(s.cellLive)
	s.clear()
	s.maxFragments, s.maxVertices = maxFragments, maxVertices
	s.budget, s.pool = budget, pool
}

// clear drops every fragment, and with them the set's references to storage
// it does not own.
func (s *svgVisibleFragmentSet) clear() {
	s.large, s.largeLive = s.large[:0], 0
	clear(s.fragments)
	s.fragments = s.fragments[:0]
	s.live, s.vertices, s.references = 0, 0, 0
	s.stamp = 0
	s.budget, s.pool = nil, nil
}

func (s *svgVisibleFragmentSet) changeCells(id int32, box svgBox, add bool) error {
	x0, y0, x1, y1, ok := s.grid.cellsFor(box)
	if !ok {
		return nil
	}
	count := (x1 - x0 + 1) * (y1 - y0 + 1)
	large := count > 64
	if large {
		count = 1
	}
	if err := s.budget.spend(count); err != nil {
		return err
	}
	if add {
		if count > s.budget.limits.gridReferences-s.references {
			return fmt.Errorf("isometric SVG visibility exceeds fragment spatial index limit")
		}
		s.references += count
	} else {
		s.references -= count
	}
	if large {
		if add {
			s.large = append(s.large, id)
			s.largeLive++
		} else {
			s.largeLive--
		}
		return nil
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			index := y*s.grid.nx + x
			if add {
				s.cells[index] = append(s.cells[index], id)
				s.cellLive[index]++
			} else {
				s.cellLive[index]--
			}
		}
	}
	return nil
}

func (s *svgVisibleFragmentSet) add(fragment svgVisibleFragment) error {
	if err := s.budget.spend(len(fragment.points) + 1); err != nil {
		return err
	}
	if s.live >= s.maxFragments || len(fragment.points) > s.maxVertices-s.vertices || len(s.fragments) >= math.MaxInt32 {
		return fmt.Errorf("isometric SVG visibility exceeds fragment limit")
	}
	id := int32(len(s.fragments) + 1)
	if err := s.changeCells(id, fragment.box, true); err != nil {
		return err
	}
	fragment.stamp = 0
	s.fragments = append(s.fragments, fragment)
	s.live++
	s.vertices += len(fragment.points)
	return nil
}

// remove takes fragment id out of the set and recycles its points.
func (s *svgVisibleFragmentSet) remove(id int32) error {
	fragment := &s.fragments[id-1]
	if err := s.changeCells(id, fragment.box, false); err != nil {
		return err
	}
	s.live--
	s.vertices -= len(fragment.points)
	if fragment.pooled {
		s.pool.release(fragment.points)
	}
	*fragment = svgVisibleFragment{}
	return nil
}

// visit appends the unseen fragments of one bucket that overlap box to s.ids.
// The bucket is returned without the fragments removed since its last visit.
func (s *svgVisibleFragmentSet) visit(bucket []int32, live int, box svgBox) ([]int32, error) {
	if err := s.budget.spend(live); err != nil {
		return bucket, err
	}
	kept := 0
	for _, id := range bucket {
		fragment := &s.fragments[id-1]
		if fragment.points == nil {
			continue
		}
		bucket[kept] = id
		kept++
		if fragment.stamp == s.stamp {
			continue
		}
		fragment.stamp = s.stamp
		if fragment.box.overlaps(box) {
			s.ids = append(s.ids, id)
		}
	}
	return bucket[:kept], nil
}

// query returns, in creation order, the fragments whose bounds overlap box.
func (s *svgVisibleFragmentSet) query(box svgBox) ([]int32, error) {
	if len(s.fragments) > 4*s.live+1024 {
		s.compact()
	}
	s.stamp++
	if s.stamp == 0 {
		for i := range s.fragments {
			s.fragments[i].stamp = 0
		}
		s.stamp = 1
	}
	s.ids = s.ids[:0]
	if x0, y0, x1, y1, ok := s.grid.cellsFor(box); ok {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				index := y*s.grid.nx + x
				bucket, err := s.visit(s.cells[index], int(s.cellLive[index]), box)
				s.cells[index] = bucket
				if err != nil {
					return nil, err
				}
			}
		}
		bucket, err := s.visit(s.large, s.largeLive, box)
		s.large = bucket
		if err != nil {
			return nil, err
		}
	}
	if err := s.budget.spend(len(s.ids) * bits.Len(uint(len(s.ids)))); err != nil {
		return nil, err
	}
	slices.Sort(s.ids)
	return s.ids, nil
}

// compact renumbers the surviving fragments in creation order and rebuilds
// the cells, dropping the slots and cell entries of removed fragments. Live
// counts and references are unchanged, so no work is charged. Fragment ids
// from an earlier query are invalid afterwards.
func (s *svgVisibleFragmentSet) compact() {
	kept := 0
	for i := range s.fragments {
		if s.fragments[i].points == nil {
			continue
		}
		s.fragments[kept] = s.fragments[i]
		s.fragments[kept].stamp = 0
		kept++
	}
	clear(s.fragments[kept:])
	s.fragments = s.fragments[:kept]
	for i := range s.cells {
		s.cells[i] = s.cells[i][:0]
	}
	s.large = s.large[:0]
	s.stamp = 0
	for i := range s.fragments {
		id := int32(i + 1)
		x0, y0, x1, y1, ok := s.grid.cellsFor(s.fragments[i].box)
		if !ok {
			continue
		}
		// The same rule as changeCells: a fragment over many cells is large.
		if (x1-x0+1)*(y1-y0+1) > 64 {
			s.large = append(s.large, id)
			continue
		}
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				s.cells[y*s.grid.nx+x] = append(s.cells[y*s.grid.nx+x], id)
			}
		}
	}
}

// subtract removes cut from every fragment it overlaps. Pieces are appended
// after the fragments that existed when the cut began, so they are not cut
// again by the same polygon.
func (s *svgVisibleFragmentSet) subtract(w *svgVisibilityWorker, cut []svgPoint, cutBox svgBox) error {
	ids, err := s.query(cutBox)
	if err != nil {
		return err
	}
	for _, id := range ids {
		changed, err := w.subtract(s.fragments[id-1].points, cut)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		// The pieces are still in the worker's scratch, so the fragment they
		// came from can be recycled before they are stored.
		if err := s.remove(id); err != nil {
			return err
		}
		start := 0
		for _, end := range w.pieceEnds {
			piece, box := svgValidFragmentOwned(w.pieces[start:end])
			start = end
			if piece == nil {
				continue
			}
			if err := s.add(svgVisibleFragment{points: s.pool.clone(piece), box: box, pooled: true}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Clipping a board creates and discards many small polygons. The pool hands
// out their storage from large chunks and reuses the storage of discarded
// fragments of the same length, so solving a face allocates nothing once the
// chunks exist. Everything it hands out is invalid after reset.
type svgPointPool struct {
	chunks [][]svgPoint
	chunk  int // Chunk being filled.
	used   int // Points handed out of that chunk.
	free   [svgPoolSizes][][]svgPoint
}

const (
	svgPoolSizes = 33 // Fragments of fewer points are recycled by length.
	svgPoolChunk = 4096
)

func (p *svgPointPool) clone(points []svgPoint) []svgPoint {
	n := len(points)
	if n < svgPoolSizes {
		if free := p.free[n]; len(free) > 0 {
			storage := free[len(free)-1][:n]
			p.free[n] = free[:len(free)-1]
			copy(storage, points)
			return storage
		}
	}
	for {
		if p.chunk == len(p.chunks) {
			p.chunks = append(p.chunks, make([]svgPoint, max(svgPoolChunk, n)))
		}
		chunk := p.chunks[p.chunk]
		if n <= len(chunk)-p.used {
			storage := chunk[p.used : p.used+n : p.used+n]
			p.used += n
			copy(storage, points)
			return storage
		}
		p.chunk++
		p.used = 0
	}
}

func (p *svgPointPool) release(points []svgPoint) {
	if n := cap(points); n > 0 && n < svgPoolSizes {
		p.free[n] = append(p.free[n], points[:0])
	}
}

func (p *svgPointPool) reset() {
	p.chunk, p.used = 0, 0
	for i := range p.free {
		p.free[i] = p.free[i][:0]
	}
}

// svgVisibilityWorker owns the scratch storage for solving faces one at a
// time. The fragments it returns live in points and lists, which are never
// reused; a face that was never cut is returned as the scene's own polygon.
type svgVisibilityWorker struct {
	scene  *svgVisibilityScene
	budget *svgVisibilityBudget

	seen       []int32 // Face last collected as a candidate of face seen-1.
	candidates []int32 // Ranks of the current face's occluders.
	treeStack  []int32
	treeHits   []int
	dist       []float64
	chain      [2][]svgPoint // Alternating intersections of a subtraction.
	cut        []svgPoint
	pieces     []svgPoint // Unvalidated pieces of the last subtraction.
	pieceEnds  []int
	fragments  []svgVisibleFragment
	next       []svgVisibleFragment
	set        svgVisibleFragmentSet
	pool       svgPointPool

	points []svgPoint
	lists  [][]svgPoint
}

var svgVisibilityWorkers = sync.Pool{New: func() any { return new(svgVisibilityWorker) }}

// A worker whose scratch storage has grown beyond this many bytes is released
// instead of being kept for the next render. A pool may hold a worker for as
// long as the process lives, so this also bounds what an idle process keeps.
const svgVisibilityWorkerRetainedBytes = 8 << 20

// retainedBytes is the storage a pooled worker keeps alive.
func (w *svgVisibilityWorker) retainedBytes() int {
	const point, fragment = int(unsafe.Sizeof(svgPoint{})), int(unsafe.Sizeof(svgVisibleFragment{}))
	points := cap(w.chain[0]) + cap(w.chain[1]) + cap(w.cut) + cap(w.pieces)
	for _, chunk := range w.pool.chunks {
		points += cap(chunk)
	}
	// Removed fragments keep their slot until the next face, so the slots of
	// one broad face can outweigh the points of its pieces.
	fragments := cap(w.fragments) + cap(w.next) + cap(w.set.fragments)
	ids := cap(w.seen) + cap(w.candidates) + cap(w.treeStack) + cap(w.set.large) + cap(w.set.ids) + cap(w.set.cellLive)
	for _, cell := range w.set.cells[:cap(w.set.cells)] {
		ids += cap(cell)
	}
	words := cap(w.treeHits) + cap(w.dist) + cap(w.pieceEnds)
	for i := range w.pool.free {
		words += 3 * cap(w.pool.free[i])
	}
	return points*point + fragments*fragment + ids*4 + words*8
}

func svgBorrowVisibilityWorker(scene *svgVisibilityScene, budget *svgVisibilityBudget) *svgVisibilityWorker {
	w := svgVisibilityWorkers.Get().(*svgVisibilityWorker)
	w.scene, w.budget = scene, budget
	if cap(w.seen) < len(scene.faces) {
		w.seen = make([]int32, len(scene.faces))
	} else {
		w.seen = w.seen[:len(scene.faces)]
		clear(w.seen)
	}
	return w
}

func svgReturnVisibilityWorker(w *svgVisibilityWorker) {
	if w.retainedBytes() > svgVisibilityWorkerRetainedBytes {
		// Give the storage up, but still hand the worker back: a pool that
		// keeps its entries in a slice may otherwise go on referring to it.
		*w = svgVisibilityWorker{}
	} else {
		// Returned fragments stay with the caller; scene polygons must not
		// be kept alive by the pool.
		w.scene, w.budget = nil, nil
		w.points, w.lists = nil, nil
		clear(w.fragments[:cap(w.fragments)])
		clear(w.next[:cap(w.next)])
		w.set.clear()
	}
	svgVisibilityWorkers.Put(w)
}
