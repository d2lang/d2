package d2isometricimg

import (
	"context"
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/d2lang/d2/lib/xsort"
)

// SVG visibility is solved in the final orthographic camera. Depth is affine
// over a projected face, so even intersecting faces can be cut exactly without
// a global BSP or a centroid painter approximation. Larger z is nearer. The
// caller includes Triangle.DepthBias in z; it never changes projected x/y.
type svgPoint struct{ x, y, z float64 }

// Canonicalize the camera boundary before topology-sensitive clipping. Fused
// multiply-add is available on some CPUs but not others; its last-bit changes
// must not change which coplanar polygons are split or their paint order. The
// binary grids preserve far more precision than the SVG's six decimal places
// (and the depth tie tolerance), without changing the source layout.
func svgProjectedPoint(x, y, z float64) svgPoint {
	return svgPoint{
		math.Round(x*(1<<24)) / (1 << 24),
		math.Round(y*(1<<24)) / (1 << 24),
		math.Round(z*(1<<36)) / (1 << 36),
	}
}

type svgVisibilityFace struct {
	points  []svgPoint // Convex, planar projected polygon, either winding.
	opaque  bool       // A fully opaque, depth-writing surface, not a decal.
	group   *nativeOpacityGroup
	owner   *nativePaintOwner
	contour bool
	order   int // Later source paint wins an exactly coplanar tie.
}

type svgVisibilityLimits struct {
	faces, vertices, fragments, work, gridReferences int
}

var svgDefaultVisibilityLimits = svgVisibilityLimits{
	faces: rasterMaxTriangles, vertices: 8_000_000, fragments: 2_000_000,
	work: 100_000_000, gridReferences: 8_000_000,
}

// The innermost clipping loops charge work for every vertex they touch. Ask
// the context whether it was canceled once per this many units instead of on
// every charge; a unit is a few nanoseconds of work.
const svgVisibilityPollWork = 4096

type svgVisibilityBudget struct {
	ctx                                context.Context
	limits                             svgVisibilityLimits
	work                               int
	face, faces, candidates, fragments int
	// next is the work total at which a charge must consult the limit and
	// the context: one past the limit, or the next poll, whichever comes
	// first. It is never below work, and starts equal to it so that the first
	// charge, even of nothing, is checked.
	next int
}

// spend charges n units of work. A charge that stays below next is one
// comparison, which the compiler inlines into the clipping loops.
func (b *svgVisibilityBudget) spend(n int) error {
	if uint(n) < uint(b.next-b.work) {
		b.work += n
		return nil
	}
	return b.spendSlow(n)
}

func (b *svgVisibilityBudget) spendSlow(n int) error {
	if n < 0 || n > b.limits.work-b.work {
		return fmt.Errorf("isometric SVG visibility exceeds work limit (%d faces, face %d, %d candidates, %d fragments)", b.faces, b.face, b.candidates, b.fragments)
	}
	b.work += n
	b.next = b.work + min(svgVisibilityPollWork-1, b.limits.work-b.work) + 1
	return b.ctx.Err()
}

type svgBox struct{ minX, minY, maxX, maxY float64 }

// Every coordinate that reaches the solver has been validated as finite, so
// the built-in min and max select the same values as math.Min and math.Max.
func svgPolygonBox(points []svgPoint) svgBox {
	b := svgBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range points {
		b.minX, b.minY = min(b.minX, p.x), min(b.minY, p.y)
		b.maxX, b.maxY = max(b.maxX, p.x), max(b.maxY, p.y)
	}
	return b
}

func (b svgBox) overlaps(c svgBox) bool {
	return b.minX < c.maxX && c.minX < b.maxX && b.minY < c.maxY && c.minY < b.maxY
}

// Depth planes use a nearby origin to avoid loss of precision after a large
// screen translation. The plane and every cut remain in the subject's depth.
type svgDepthPlane struct {
	origin svgPoint
	dx, dy float64
}

func (p svgDepthPlane) at(v svgPoint) float64 {
	// Explicit conversions round each product, forbidding implicit FMA here
	// and in the clipping predicates below even after compiler inlining.
	return p.origin.z + float64(p.dx*(v.x-p.origin.x)) + float64(p.dy*(v.y-p.origin.y))
}

func svgPolygonArea(points []svgPoint) float64 {
	if len(points) < 3 {
		return 0
	}
	var area float64
	o := points[0]
	for i := 1; i+1 < len(points); i++ {
		a, b := points[i], points[i+1]
		area += float64((a.x-o.x)*(b.y-o.y)) - float64((a.y-o.y)*(b.x-o.x))
	}
	return area / 2
}

func svgPolygonPlane(points []svgPoint) (svgDepthPlane, bool) {
	if len(points) < 3 {
		return svgDepthPlane{}, false
	}
	o := points[0]
	best, index := 0.0, 0
	for i := 1; i+1 < len(points); i++ {
		a, b := points[i], points[i+1]
		d := float64((a.x-o.x)*(b.y-o.y)) - float64((a.y-o.y)*(b.x-o.x))
		if math.Abs(d) > math.Abs(best) {
			best, index = d, i
		}
	}
	if math.Abs(best) <= 1e-14 {
		return svgDepthPlane{}, false
	}
	a, b := points[index], points[index+1]
	return svgDepthPlane{o,
		(float64((a.z-o.z)*(b.y-o.y)) - float64((b.z-o.z)*(a.y-o.y))) / best,
		(float64((a.x-o.x)*(b.z-o.z)) - float64((b.x-o.x)*(a.z-o.z))) / best,
	}, true
}

type svgVisibilityPolygon struct {
	points []svgPoint
	plane  svgDepthPlane
	box    svgBox
	area   float64
}

// A uniform grid keeps ordinary diagram meshes local. Very large polygons use
// one overflow bucket instead of occupying thousands of cells. All candidate
// visits, including duplicates and overflow visits, consume the work budget.
type svgVisibilityGrid struct {
	bounds svgBox
	nx, ny int
	cells  [][]int
	large  []int
}

func (g *svgVisibilityGrid) cellsFor(b svgBox) (x0, y0, x1, y1 int, ok bool) {
	if !g.bounds.overlaps(b) {
		return 0, 0, 0, 0, false
	}
	x := func(v float64) int {
		return max(0, min(g.nx-1, int(math.Floor((v-g.bounds.minX)/(g.bounds.maxX-g.bounds.minX)*float64(g.nx)))))
	}
	y := func(v float64) int {
		return max(0, min(g.ny-1, int(math.Floor((v-g.bounds.minY)/(g.bounds.maxY-g.bounds.minY)*float64(g.ny)))))
	}
	return x(b.minX), y(b.minY), x(b.maxX), y(b.maxY), true
}

func svgNewVisibilityGrid(polygons []svgVisibilityPolygon, faces []svgVisibilityFace, budget *svgVisibilityBudget) (*svgVisibilityGrid, error) {
	bounds := svgBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	count := 0
	for i := range polygons {
		p := &polygons[i]
		if err := budget.spend(1); err != nil {
			return nil, err
		}
		if len(p.points) == 0 || !faces[i].opaque {
			continue
		}
		bounds.minX, bounds.minY = min(bounds.minX, p.box.minX), min(bounds.minY, p.box.minY)
		bounds.maxX, bounds.maxY = max(bounds.maxX, p.box.maxX), max(bounds.maxY, p.box.maxY)
		count++
	}
	if count == 0 {
		return nil, nil
	}
	w, h := bounds.maxX-bounds.minX, bounds.maxY-bounds.minY
	cells := math.Max(1, math.Min(65536, float64(count)/4))
	nx := max(1, min(256, int(math.Ceil(math.Sqrt(cells*w/h)))))
	ny := max(1, min(256, int(math.Ceil(cells/float64(nx)))))
	g := &svgVisibilityGrid{bounds: bounds, nx: nx, ny: ny, cells: make([][]int, nx*ny)}
	references := 0
	for i := range polygons {
		p := &polygons[i]
		if err := budget.spend(1); err != nil {
			return nil, err
		}
		if len(p.points) == 0 || !faces[i].opaque {
			continue
		}
		x0, y0, x1, y1, _ := g.cellsFor(p.box)
		n := (x1 - x0 + 1) * (y1 - y0 + 1)
		if n > 64 {
			g.large = append(g.large, i)
			n = 1
		} else {
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					g.cells[y*nx+x] = append(g.cells[y*nx+x], i)
				}
			}
		}
		references += n
		if references > budget.limits.gridReferences {
			return nil, fmt.Errorf("isometric SVG visibility exceeds spatial index limit")
		}
	}
	return g, nil
}

// svgSplitPolygon cuts a convex polygon into two convex pieces. New vertices
// interpolate all three coordinates, so the output remains on its own face.
func svgSplitPolygon(points []svgPoint, distance func(svgPoint) float64) (inside, outside []svgPoint) {
	if len(points) == 0 {
		return nil, nil
	}
	distances := make([]float64, len(points))
	positive, negative := false, false
	for i, point := range points {
		d := distance(point)
		distances[i] = d
		positive, negative = positive || d > 0, negative || d < 0
	}
	// An exactly coincident or zero-length clipping edge contains the whole
	// face. It must not also emit the whole face as an outside fragment: that
	// would resurrect previously hidden geometry and multiply later cuts.
	if !negative {
		return points, nil
	}
	if !positive {
		return nil, points
	}
	inside, outside = svgSplitRaw(points, distances, make([]svgPoint, 0, len(points)+2), make([]svgPoint, 0, len(points)+2))
	return svgValidFragment(inside), svgValidFragment(outside)
}

// svgSplitRaw appends the two sides of a cut to in and out. dist holds the
// signed distance of every point and has both a positive and a negative entry.
// Neither side has been through svgValidFragment yet.
func svgSplitRaw(points []svgPoint, dist []float64, in, out []svgPoint) (inside, outside []svgPoint) {
	previous := points[len(points)-1]
	dp := dist[len(points)-1]
	for i, current := range points {
		dc := dist[i]
		if (dp < 0 && dc > 0) || (dp > 0 && dc < 0) {
			t := dp / (dp - dc)
			p := svgPoint{previous.x + float64(t*(current.x-previous.x)), previous.y + float64(t*(current.y-previous.y)), previous.z + float64(t*(current.z-previous.z))}
			in, out = append(in, p), append(out, p)
		}
		if dc >= 0 {
			in = append(in, current)
		}
		if dc <= 0 {
			out = append(out, current)
		}
		previous, dp = current, dc
	}
	return in, out
}

// svgSplitInside is svgSplitRaw for callers that discard the outside.
func svgSplitInside(points []svgPoint, dist []float64, in []svgPoint) []svgPoint {
	previous := points[len(points)-1]
	dp := dist[len(points)-1]
	for i, current := range points {
		dc := dist[i]
		if (dp < 0 && dc > 0) || (dp > 0 && dc < 0) {
			t := dp / (dp - dc)
			in = append(in, svgPoint{previous.x + float64(t*(current.x-previous.x)), previous.y + float64(t*(current.y-previous.y)), previous.z + float64(t*(current.z-previous.z))})
		}
		if dc >= 0 {
			in = append(in, current)
		}
		previous, dp = current, dc
	}
	return in
}

// A depth cut through a shared vertex can create two identical projected
// points with a last-bit difference in z. svgSamePoint identifies those
// zero-length edges before they become half-plane constraints in a subsequent
// subtraction.
func svgSamePoint(a, b svgPoint) bool {
	epsilon := max(1e-9, max(max(math.Abs(a.x), math.Abs(a.y)), max(math.Abs(b.x), math.Abs(b.y)))*1e-15)
	return math.Abs(a.x-b.x) <= epsilon && math.Abs(a.y-b.y) <= epsilon
}

// Projection rounding can leave almost-collinear slivers at shared edges.
// They are narrower than SVG serialization precision, but retaining them
// would multiply subsequent cuts and exhaust the visibility work budget.
func svgSliver(points []svgPoint, box svgBox) bool {
	span := max(box.maxX-box.minX, box.maxY-box.minY)
	return math.Abs(svgPolygonArea(points)) <= max(span*1e-8, span*span*1e-13)
}

func svgValidFragment(points []svgPoint) []svgPoint {
	if len(points) < 3 {
		return nil
	}
	for i, p := range points {
		if !svgSamePoint(p, points[(i+1)%len(points)]) {
			continue
		}
		clean := make([]svgPoint, 0, len(points))
		for _, point := range points {
			if len(clean) == 0 || !svgSamePoint(point, clean[len(clean)-1]) {
				clean = append(clean, point)
			}
		}
		if len(clean) > 1 && svgSamePoint(clean[0], clean[len(clean)-1]) {
			clean = clean[:len(clean)-1]
		}
		points = clean
		break
	}
	if len(points) < 3 {
		return nil
	}
	if svgSliver(points, svgPolygonBox(points)) {
		return nil
	}
	return points
}

// svgValidFragmentOwned is svgValidFragment for a slice the caller owns. It
// removes zero-length edges in place and also reports the fragment's bounds,
// which the solver would otherwise recompute for every later cut.
func svgValidFragmentOwned(points []svgPoint) ([]svgPoint, svgBox) {
	n := len(points)
	if n < 3 {
		return nil, svgBox{}
	}
	duplicate := svgSamePoint(points[n-1], points[0])
	for i := 0; !duplicate && i+1 < n; i++ {
		duplicate = svgSamePoint(points[i], points[i+1])
	}
	if duplicate {
		kept := 0
		for _, point := range points {
			if kept == 0 || !svgSamePoint(point, points[kept-1]) {
				points[kept] = point
				kept++
			}
		}
		if kept > 1 && svgSamePoint(points[0], points[kept-1]) {
			kept--
		}
		if kept < 3 {
			return nil, svgBox{}
		}
		points = points[:kept]
	}
	box := svgPolygonBox(points)
	if svgSliver(points, box) {
		return nil, svgBox{}
	}
	return points, box
}

// svgEdgeSide is the cross product of an edge's direction (ex, ey) with the
// vector from the edge's start a to p: zero when p is on the edge's line, and
// of opposite signs on its two sides. Every clipping predicate goes through
// it, so the conversions that forbid a fused multiply-add are written, and
// tested, in one place.
func svgEdgeSide(ex, ey float64, a, p svgPoint) float64 {
	return float64(ex*(p.y-a.y)) - float64(ey*(p.x-a.x))
}

func svgEdgeDistance(a, b svgPoint) func(svgPoint) float64 {
	ex, ey := b.x-a.x, b.y-a.y
	return func(p svgPoint) float64 { return svgEdgeSide(ex, ey, a, p) }
}

func svgVisibleFaces(ctx context.Context, faces []svgVisibilityFace) ([][][]svgPoint, error) {
	limits := svgDefaultVisibilityLimits
	limits.work = svgVisibilityWorkLimit(len(faces))
	return svgVisibleFacesWithLimits(ctx, faces, limits)
}

// Preserve the ordinary diagram budget while giving large meshes a linear
// amount of work per input face for indexing, local depth tests and clipping.
// Fragment indexing controls combinatorial growth independently. The native
// raster work ceiling remains an absolute bound for every visibility pass.
func svgVisibilityWorkLimit(faces int) int {
	if faces > rasterMaxWork/2304 {
		return rasterMaxWork
	}
	return max(svgDefaultVisibilityLimits.work, faces*2304)
}

// svgVisibilityScene is the read-only input shared by every subject face.
type svgVisibilityScene struct {
	faces    []svgVisibilityFace
	polygons []svgVisibilityPolygon
	grid     *svgVisibilityGrid
	tree     *svgVisibilityTree
	// Occluders are subtracted in an order that does not depend on the subject,
	// so the scene ranks every face once and each subject merely sorts the
	// ranks of its candidates. ranked[rank[i]] == i.
	rank, ranked []int32
}

// Remove broad solid faces before the narrow ink ribbons around them. Cutting
// thousands of ribbons first would unnecessarily partition a large board into
// slivers that its few broad caps subsequently hide. Indices are distinct, so
// this is a total order and every sorting algorithm agrees on the result.
func (scene *svgVisibilityScene) rankOccluders() {
	scene.ranked = make([]int32, len(scene.faces))
	for i := range scene.ranked {
		scene.ranked[i] = int32(i)
	}
	polygons, faces := scene.polygons, scene.faces
	xsort.Func(scene.ranked, func(a, b int32) int {
		if polygons[a].area != polygons[b].area {
			if polygons[a].area > polygons[b].area {
				return -1
			}
			return 1
		}
		if faces[a].order != faces[b].order {
			if faces[a].order > faces[b].order {
				return -1
			}
			return 1
		}
		return int(b - a)
	})
	scene.rank = make([]int32, len(scene.faces))
	for rank, index := range scene.ranked {
		scene.rank[index] = int32(rank)
	}
}

func svgVisibleFacesWithLimits(ctx context.Context, faces []svgVisibilityFace, limits svgVisibilityLimits) ([][][]svgPoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(faces) > limits.faces {
		return nil, fmt.Errorf("isometric SVG visibility exceeds face limit")
	}
	budget := &svgVisibilityBudget{ctx: ctx, limits: limits, faces: len(faces)}
	polygons := make([]svgVisibilityPolygon, len(faces))
	// The solver snaps and reorders a private copy of each face, never the
	// caller's. One slab holds every copy; the vertex limit bounds its size.
	vertices := 0
	for i := range faces {
		vertices += len(faces[i].points)
	}
	storage := make([]svgPoint, max(0, min(vertices, limits.vertices)))
	inputVertices := 0
	for i := range faces {
		face := &faces[i]
		if err := budget.spend(len(face.points) + 1); err != nil {
			return nil, err
		}
		inputVertices += len(face.points)
		if inputVertices > limits.vertices {
			return nil, fmt.Errorf("isometric SVG visibility exceeds vertex limit")
		}
		// Clipping a ground caster uses its exact world height. Snap the
		// resulting intersections here, before fitting any depth planes,
		// just as for projected mesh faces. The caller keeps its exact
		// coordinates for clipping against the next receiver-height band.
		points := storage[:len(face.points):len(face.points)]
		storage = storage[len(face.points):]
		for j, p := range face.points {
			if math.IsNaN(p.x) || math.IsNaN(p.y) || math.IsNaN(p.z) || math.Abs(p.x) > rasterCoordinateLimit || math.Abs(p.y) > rasterCoordinateLimit || math.Abs(p.z) > rasterCoordinateLimit {
				return nil, fmt.Errorf("isometric SVG visibility face %d has invalid coordinates", i)
			}
			points[j] = svgProjectedPoint(p.x, p.y, p.z)
		}
		points, box := svgValidFragmentOwned(points)
		plane, ok := svgPolygonPlane(points)
		if !ok {
			continue
		}
		if svgPolygonArea(points) < 0 {
			slices.Reverse(points)
		}
		// Area is only a clipping-order heuristic. Group subpixel-equivalent
		// areas so symmetric faces use the stable source-order tie break.
		polygons[i] = svgVisibilityPolygon{points: points, plane: plane, box: box, area: math.Round(svgPolygonArea(points)*1e4) / 1e4}
	}
	scene := &svgVisibilityScene{faces: faces, polygons: polygons}
	var err error
	if len(faces) > 8192 {
		scene.tree, err = svgNewVisibilityTree(polygons, faces, budget)
	} else {
		scene.grid, err = svgNewVisibilityGrid(polygons, faces, budget)
	}
	if err != nil {
		return nil, err
	}
	scene.rankOccluders()
	output := make([][][]svgPoint, len(faces))
	worker := svgBorrowVisibilityWorker(scene, budget)
	defer svgReturnVisibilityWorker(worker)
	outputVertices, outputFragments := 0, 0
	for index := range polygons {
		fragments, err := worker.visible(index, limits.fragments-outputFragments, limits.vertices-outputVertices)
		if err != nil {
			return nil, err
		}
		if len(polygons[index].points) == 0 {
			continue
		}
		outputFragments += len(fragments)
		for _, fragment := range fragments {
			outputVertices += len(fragment)
		}
		if outputFragments > limits.fragments || outputVertices > limits.vertices {
			return nil, fmt.Errorf("isometric SVG visibility exceeds fragment limit")
		}
		output[index] = fragments
	}
	return output, nil
}

// visible returns the fragments of face index that no nearer opaque face
// covers. The fragment and vertex allowances are what remains of the limits
// after the faces already solved.
func (w *svgVisibilityWorker) visible(index, maxFragments, maxVertices int) ([][]svgPoint, error) {
	scene, budget := w.scene, w.budget
	budget.face, budget.candidates, budget.fragments = index, 0, 1
	if err := budget.spend(1); err != nil {
		return nil, err
	}
	subject := &scene.polygons[index]
	if len(subject.points) == 0 {
		return nil, nil
	}
	w.candidates = w.candidates[:0]
	if tree := scene.tree; tree != nil {
		var err error
		w.treeStack, w.treeHits, err = tree.query(subject.box, budget, w.treeStack, w.treeHits[:0])
		if err != nil {
			return nil, err
		}
		if err := w.collect(w.treeHits, index); err != nil {
			return nil, err
		}
	} else if grid := scene.grid; grid != nil {
		if x0, y0, x1, y1, ok := grid.cellsFor(subject.box); ok {
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					if err := w.collect(grid.cells[y*grid.nx+x], index); err != nil {
						return nil, err
					}
				}
			}
			if err := w.collect(grid.large, index); err != nil {
				return nil, err
			}
		}
	}
	candidates := w.candidates
	budget.candidates = len(candidates)
	if err := budget.spend(len(candidates) * bits.Len(uint(len(candidates)))); err != nil {
		return nil, err
	}
	xsort.Ordered(candidates)

	w.pool.reset()
	indexed := len(candidates) > 64
	set := &w.set
	fragments := append(w.fragments[:0], svgVisibleFragment{points: subject.points, box: subject.box})
	next := w.next[:0]
	if indexed {
		set.reset(subject.box, budget, &w.pool, maxFragments, maxVertices)
		if err := set.add(svgVisibleFragment{points: subject.points, box: subject.box}); err != nil {
			return nil, err
		}
	}
	order := scene.faces[index].order
	for _, rank := range candidates {
		other := int(scene.ranked[rank])
		occluder := &scene.polygons[other]
		if err := budget.spend(len(occluder.points) + 4); err != nil {
			return nil, err
		}
		// The overlap bbox bounds both affine depth extrema without requiring
		// another polygon allocation merely to classify a coplanar pair.
		box := svgBox{max(subject.box.minX, occluder.box.minX), max(subject.box.minY, occluder.box.minY), min(subject.box.maxX, occluder.box.maxX), min(subject.box.maxY, occluder.box.maxY)}
		lo, hi, depthMagnitude := math.Inf(1), math.Inf(-1), 1.0
		for _, p := range [4]svgPoint{{x: box.minX, y: box.minY}, {x: box.maxX, y: box.minY}, {x: box.maxX, y: box.maxY}, {x: box.minX, y: box.maxY}} {
			near, far := occluder.plane.at(p), subject.plane.at(p)
			d := near - far
			lo, hi = min(lo, d), max(hi, d)
			depthMagnitude = max(depthMagnitude, max(math.Abs(far), math.Abs(near)))
		}
		cut, cutBox := occluder.points, occluder.box
		epsilon := max(1e-9, depthMagnitude*2e-14)
		if lo >= -epsilon && hi <= epsilon {
			if occluderOrder := scene.faces[other].order; occluderOrder < order || (occluderOrder == order && other < index) {
				continue
			}
		} else if hi <= 0 {
			continue
		} else if lo < 0 {
			cut, cutBox = w.nearer(cut, cutBox, occluder.plane, subject.plane)
			if len(cut) == 0 {
				continue
			}
		}
		if indexed {
			if err := set.subtract(w, cut, cutBox); err != nil {
				return nil, err
			}
			budget.fragments = set.live
			if budget.fragments == 0 {
				break
			}
			continue
		}
		next = next[:0]
		vertices := 0
		for _, fragment := range fragments {
			if err := budget.spend(len(fragment.points) + 1); err != nil {
				return nil, err
			}
			changed := false
			if fragment.box.overlaps(cutBox) {
				var err error
				changed, err = w.subtract(fragment.points, cut)
				if err != nil {
					return nil, err
				}
			}
			if changed {
				start := 0
				for _, end := range w.pieceEnds {
					piece, pieceBox := svgValidFragmentOwned(w.pieces[start:end])
					start = end
					if piece == nil {
						continue
					}
					vertices += len(piece)
					next = append(next, svgVisibleFragment{points: w.pool.clone(piece), box: pieceBox, pooled: true})
				}
				if fragment.pooled {
					w.pool.release(fragment.points)
				}
			} else {
				vertices += len(fragment.points)
				next = append(next, fragment)
			}
			if len(next) > maxFragments || vertices > maxVertices {
				return nil, fmt.Errorf("isometric SVG visibility exceeds fragment limit")
			}
		}
		fragments, next = next, fragments
		budget.fragments = len(fragments)
		if len(fragments) == 0 {
			break
		}
	}
	w.fragments, w.next = fragments, next
	if indexed {
		if err := budget.spend(set.live * bits.Len(uint(set.live))); err != nil {
			return nil, err
		}
		if set.live == 0 {
			return [][]svgPoint{}, nil
		}
		return w.retain(set.fragments, set.live), nil
	}
	return w.retain(fragments, len(fragments)), nil
}

// collect appends the occluder candidates of face index among indices.
func (w *svgVisibilityWorker) collect(indices []int, index int) error {
	if err := w.budget.spend(len(indices)); err != nil {
		return err
	}
	scene := w.scene
	face, subject := &scene.faces[index], &scene.polygons[index]
	stamp := int32(index + 1)
	for _, other := range indices {
		if other == index || w.seen[other] == stamp {
			continue
		}
		w.seen[other] = stamp
		occluder := &scene.faces[other]
		if occluder.group != nil && occluder.group != face.group {
			continue
		}
		if occluder.contour && !face.contour && ((face.owner != nil && face.owner == occluder.owner) || (face.group != nil && face.group == occluder.group)) {
			continue
		}
		if subject.box.overlaps(scene.polygons[other].box) {
			w.candidates = append(w.candidates, scene.rank[other])
		}
	}
	return nil
}

// nearer returns the part of an occluder that is in front of the subject's
// plane, with its bounds. The result lives in the worker until the next call.
func (w *svgVisibilityWorker) nearer(cut []svgPoint, cutBox svgBox, occluder, subject svgDepthPlane) ([]svgPoint, svgBox) {
	dist := w.distances(len(cut))
	positive, negative := false, false
	for i, p := range cut {
		d := occluder.at(p) - subject.at(p)
		dist[i] = d
		positive, negative = positive || d > 0, negative || d < 0
	}
	if !negative {
		return cut, cutBox
	}
	if !positive {
		return nil, svgBox{}
	}
	w.cut = svgSplitInside(cut, dist, w.cut[:0])
	return svgValidFragmentOwned(w.cut)
}

func (w *svgVisibilityWorker) distances(n int) []float64 {
	if cap(w.dist) < n {
		w.dist = make([]float64, max(2*n, 32))
	}
	return w.dist[:n]
}

// subtract removes a convex cut from a convex subject. A convex subtraction
// yields disjoint convex fragments. It reports false when the two do not
// overlap: subtracting a remote polygon must not fragment a face along
// infinitely extended edges that never actually cover any of it. Otherwise the
// visible pieces are left in w.pieces, delimited by w.pieceEnds, and have not
// been through svgValidFragment yet.
//
// The subject is clipped against each cut edge once. Work limits are
// calibrated to two passes over this chain, one to establish that an
// intersection exists and one to collect what lies outside it, as the
// reference solver in svg_visibility_reference_test.go makes them. Both passes
// would produce the same intermediate polygons, so the second is charged to
// the budget without being run.
func (w *svgVisibilityWorker) subtract(subject, cut []svgPoint) (bool, error) {
	budget := w.budget
	w.pieces, w.pieceEnds = w.pieces[:0], w.pieceEnds[:0]
	remaining := subject
	scratch := 0
	charged := 0
	for i, a := range cut {
		// Edge i runs from cut[i] to its successor.
		b := cut[0]
		if i+1 < len(cut) {
			b = cut[i+1]
		}
		if err := budget.spend(len(remaining)); err != nil {
			return false, err
		}
		charged += len(remaining)
		ex, ey := b.x-a.x, b.y-a.y
		dist := w.distances(len(remaining))
		positive, negative := false, false
		for j, p := range remaining {
			d := svgEdgeSide(ex, ey, a, p)
			dist[j] = d
			positive, negative = positive || d > 0, negative || d < 0
		}
		if !negative {
			continue
		}
		if !positive {
			return false, nil
		}
		inside, pieces := svgSplitRaw(remaining, dist, w.chain[scratch][:0], w.pieces)
		w.chain[scratch] = inside[:0]
		inside, _ = svgValidFragmentOwned(inside)
		if len(inside) == 0 {
			return false, nil
		}
		w.pieces = pieces
		w.pieceEnds = append(w.pieceEnds, len(pieces))
		remaining = inside
		scratch ^= 1
	}
	if err := budget.spend(charged); err != nil {
		return false, err
	}
	return true, nil
}

// retain copies the surviving fragments of one face out of the worker's
// scratch storage. A face that was never cut keeps its own polygon.
func (w *svgVisibilityWorker) retain(fragments []svgVisibleFragment, live int) [][]svgPoint {
	if live == 0 {
		return nil
	}
	// Returned storage is carved from chunks sized to the scene, so a small
	// diagram does not clear kilobytes of slab for a handful of fragments.
	faces := len(w.scene.faces)
	if live > len(w.lists) {
		w.lists = make([][]svgPoint, max(live, min(faces, 1024)))
	}
	list := w.lists[:live:live]
	w.lists = w.lists[live:]
	n := 0
	for i := range fragments {
		fragment := &fragments[i]
		if fragment.points == nil {
			continue
		}
		points := fragment.points
		if fragment.pooled {
			if len(points) > len(w.points) {
				w.points = make([]svgPoint, max(len(points), min(4*faces, 4096)))
			}
			points = w.points[:len(fragment.points):len(fragment.points)]
			w.points = w.points[len(points):]
			copy(points, fragment.points)
		}
		list[n] = points
		n++
	}
	return list
}
