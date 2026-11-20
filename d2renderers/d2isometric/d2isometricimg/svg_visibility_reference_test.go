package d2isometricimg

// This file freezes the original SVG visibility solver as a test oracle. The
// production solver must reproduce its fragments, their order and its work
// accounting exactly, so the two are compared on generated scenes. Only the
// identifiers are renamed; do not optimize this copy. When the solver's output
// or accounting is changed on purpose, make the same change here, or delete
// this file together with the tests that compare against it.

import (
	"context"
	"fmt"
	"math"
	"math/bits"
	"sort"
)

type refSvgVisibilityBudget struct {
	ctx                                context.Context
	limits                             svgVisibilityLimits
	work                               int
	face, faces, candidates, fragments int
}

func (b *refSvgVisibilityBudget) spend(n int) error {
	if n < 0 || n > b.limits.work-b.work {
		return fmt.Errorf("isometric SVG visibility exceeds work limit (%d faces, face %d, %d candidates, %d fragments)", b.faces, b.face, b.candidates, b.fragments)
	}
	b.work += n
	return b.ctx.Err()
}

type refSvgBox struct{ minX, minY, maxX, maxY float64 }

func refSvgPolygonBox(points []svgPoint) refSvgBox {
	b := refSvgBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range points {
		b.minX, b.minY = math.Min(b.minX, p.x), math.Min(b.minY, p.y)
		b.maxX, b.maxY = math.Max(b.maxX, p.x), math.Max(b.maxY, p.y)
	}
	return b
}

func (b refSvgBox) overlaps(c refSvgBox) bool {
	return b.minX < c.maxX && c.minX < b.maxX && b.minY < c.maxY && c.minY < b.maxY
}

// Depth planes use a nearby origin to avoid loss of precision after a large
// screen translation. The plane and every cut remain in the subject's depth.
type refSvgDepthPlane struct {
	origin svgPoint
	dx, dy float64
}

func (p refSvgDepthPlane) at(v svgPoint) float64 {
	// Explicit conversions round each product, forbidding implicit FMA here
	// and in the clipping predicates below even after compiler inlining.
	return p.origin.z + float64(p.dx*(v.x-p.origin.x)) + float64(p.dy*(v.y-p.origin.y))
}

func refSvgPolygonArea(points []svgPoint) float64 {
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

func refSvgPolygonPlane(points []svgPoint) (refSvgDepthPlane, bool) {
	if len(points) < 3 {
		return refSvgDepthPlane{}, false
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
		return refSvgDepthPlane{}, false
	}
	a, b := points[index], points[index+1]
	return refSvgDepthPlane{o,
		(float64((a.z-o.z)*(b.y-o.y)) - float64((b.z-o.z)*(a.y-o.y))) / best,
		(float64((a.x-o.x)*(b.z-o.z)) - float64((b.x-o.x)*(a.z-o.z))) / best,
	}, true
}

type refSvgVisibilityPolygon struct {
	points []svgPoint
	plane  refSvgDepthPlane
	box    refSvgBox
	area   float64
}

// A uniform grid keeps ordinary diagram meshes local. Very large polygons use
// one overflow bucket instead of occupying thousands of cells. All candidate
// visits, including duplicates and overflow visits, consume the work budget.
type refSvgVisibilityGrid struct {
	bounds refSvgBox
	nx, ny int
	cells  [][]int
	large  []int
}

func (g *refSvgVisibilityGrid) cellsFor(b refSvgBox) (x0, y0, x1, y1 int, ok bool) {
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

func refSvgNewVisibilityGrid(polygons []refSvgVisibilityPolygon, faces []svgVisibilityFace, budget *refSvgVisibilityBudget) (*refSvgVisibilityGrid, error) {
	bounds := refSvgBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	count := 0
	for i, p := range polygons {
		if err := budget.spend(1); err != nil {
			return nil, err
		}
		if len(p.points) == 0 || !faces[i].opaque {
			continue
		}
		bounds.minX, bounds.minY = math.Min(bounds.minX, p.box.minX), math.Min(bounds.minY, p.box.minY)
		bounds.maxX, bounds.maxY = math.Max(bounds.maxX, p.box.maxX), math.Max(bounds.maxY, p.box.maxY)
		count++
	}
	if count == 0 {
		return nil, nil
	}
	w, h := bounds.maxX-bounds.minX, bounds.maxY-bounds.minY
	cells := math.Max(1, math.Min(65536, float64(count)/4))
	nx := max(1, min(256, int(math.Ceil(math.Sqrt(cells*w/h)))))
	ny := max(1, min(256, int(math.Ceil(cells/float64(nx)))))
	g := &refSvgVisibilityGrid{bounds: bounds, nx: nx, ny: ny, cells: make([][]int, nx*ny)}
	references := 0
	for i, p := range polygons {
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

// refSvgSplitPolygon cuts a convex polygon into two convex pieces. New vertices
// interpolate all three coordinates, so the output remains on its own face.
func refSvgSplitPolygon(points []svgPoint, distance func(svgPoint) float64) (inside, outside []svgPoint) {
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
	previous := points[len(points)-1]
	dp := distances[len(points)-1]
	for i, current := range points {
		dc := distances[i]
		if (dp < 0 && dc > 0) || (dp > 0 && dc < 0) {
			t := dp / (dp - dc)
			p := svgPoint{previous.x + float64(t*(current.x-previous.x)), previous.y + float64(t*(current.y-previous.y)), previous.z + float64(t*(current.z-previous.z))}
			inside, outside = append(inside, p), append(outside, p)
		}
		if dc >= 0 {
			inside = append(inside, current)
		}
		if dc <= 0 {
			outside = append(outside, current)
		}
		previous, dp = current, dc
	}
	return refSvgValidFragment(inside), refSvgValidFragment(outside)
}

func refSvgValidFragment(points []svgPoint) []svgPoint {
	if len(points) < 3 {
		return nil
	}
	// A depth cut through a shared vertex can create two identical projected
	// points with a last-bit difference in z. Remove those zero-length edges
	// before they become half-plane constraints in a subsequent subtraction.
	same := func(a, b svgPoint) bool {
		epsilon := math.Max(1e-9, math.Max(math.Max(math.Abs(a.x), math.Abs(a.y)), math.Max(math.Abs(b.x), math.Abs(b.y)))*1e-15)
		return math.Abs(a.x-b.x) <= epsilon && math.Abs(a.y-b.y) <= epsilon
	}
	for i, p := range points {
		if !same(p, points[(i+1)%len(points)]) {
			continue
		}
		clean := make([]svgPoint, 0, len(points))
		for _, point := range points {
			if len(clean) == 0 || !same(point, clean[len(clean)-1]) {
				clean = append(clean, point)
			}
		}
		if len(clean) > 1 && same(clean[0], clean[len(clean)-1]) {
			clean = clean[:len(clean)-1]
		}
		points = clean
		break
	}
	if len(points) < 3 {
		return nil
	}
	box := refSvgPolygonBox(points)
	span := math.Max(box.maxX-box.minX, box.maxY-box.minY)
	// Projection rounding can leave almost-collinear slivers at shared edges.
	// They are narrower than SVG serialization precision, but retaining them
	// would multiply subsequent cuts and exhaust the visibility work budget.
	if math.Abs(refSvgPolygonArea(points)) <= math.Max(span*1e-8, span*span*1e-13) {
		return nil
	}
	return points
}

func refSvgEdgeDistance(a, b svgPoint) func(svgPoint) float64 {
	return func(p svgPoint) float64 { return float64((b.x-a.x)*(p.y-a.y)) - float64((b.y-a.y)*(p.x-a.x)) }
}

// A convex subtraction yields disjoint convex fragments. Checking intersection
// first is essential: subtracting a remote polygon must not fragment a face
// along infinitely extended edges that never actually cover any of it.
func refSvgSubtractPolygon(subject, cut []svgPoint, budget *refSvgVisibilityBudget) ([][]svgPoint, error) {
	intersection := subject
	for i, a := range cut {
		if err := budget.spend(len(intersection)); err != nil {
			return nil, err
		}
		intersection, _ = refSvgSplitPolygon(intersection, refSvgEdgeDistance(a, cut[(i+1)%len(cut)]))
		if len(intersection) == 0 {
			return [][]svgPoint{subject}, nil
		}
	}
	remaining := subject
	var visible [][]svgPoint
	for i, a := range cut {
		if err := budget.spend(len(remaining)); err != nil {
			return nil, err
		}
		var outside []svgPoint
		remaining, outside = refSvgSplitPolygon(remaining, refSvgEdgeDistance(a, cut[(i+1)%len(cut)]))
		if len(outside) != 0 {
			visible = append(visible, outside)
		}
		if len(remaining) == 0 {
			break
		}
	}
	return visible, nil
}

func refSvgVisibleFaces(ctx context.Context, faces []svgVisibilityFace) ([][][]svgPoint, error) {
	limits := svgDefaultVisibilityLimits
	limits.work = svgVisibilityWorkLimit(len(faces))
	return refSvgVisibleFacesWithLimits(ctx, faces, limits)
}

func refSvgVisibleFacesWithLimits(ctx context.Context, faces []svgVisibilityFace, limits svgVisibilityLimits) ([][][]svgPoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(faces) > limits.faces {
		return nil, fmt.Errorf("isometric SVG visibility exceeds face limit")
	}
	budget := &refSvgVisibilityBudget{ctx: ctx, limits: limits, faces: len(faces)}
	polygons := make([]refSvgVisibilityPolygon, len(faces))
	inputVertices := 0
	for i, face := range faces {
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
		points := make([]svgPoint, len(face.points))
		for j, p := range face.points {
			if math.IsNaN(p.x) || math.IsNaN(p.y) || math.IsNaN(p.z) || math.Abs(p.x) > rasterCoordinateLimit || math.Abs(p.y) > rasterCoordinateLimit || math.Abs(p.z) > rasterCoordinateLimit {
				return nil, fmt.Errorf("isometric SVG visibility face %d has invalid coordinates", i)
			}
			points[j] = svgProjectedPoint(p.x, p.y, p.z)
		}
		points = refSvgValidFragment(points)
		plane, ok := refSvgPolygonPlane(points)
		if !ok {
			continue
		}
		if refSvgPolygonArea(points) < 0 {
			for a, b := 0, len(points)-1; a < b; a, b = a+1, b-1 {
				points[a], points[b] = points[b], points[a]
			}
		}
		// Area is only a clipping-order heuristic. Group subpixel-equivalent
		// areas so symmetric faces use the stable source-order tie break.
		polygons[i] = refSvgVisibilityPolygon{points, plane, refSvgPolygonBox(points), math.Round(refSvgPolygonArea(points)*1e4) / 1e4}
	}
	var grid *refSvgVisibilityGrid
	var tree *refSvgVisibilityTree
	var err error
	if len(faces) > 8192 {
		tree, err = refSvgNewVisibilityTree(polygons, faces, budget)
	} else {
		grid, err = refSvgNewVisibilityGrid(polygons, faces, budget)
	}
	if err != nil {
		return nil, err
	}
	output := make([][][]svgPoint, len(faces))
	seen := make([]int, len(faces))
	outputVertices, outputFragments := 0, 0
	for index, subject := range polygons {
		budget.face, budget.candidates, budget.fragments = index, 0, 1
		if err := budget.spend(1); err != nil {
			return nil, err
		}
		if len(subject.points) == 0 {
			continue
		}
		fragments := [][]svgPoint{subject.points}
		var candidates []int
		visit := func(indices []int) error {
			if err := budget.spend(len(indices)); err != nil {
				return err
			}
			for _, other := range indices {
				if other == index || seen[other] == index+1 {
					continue
				}
				seen[other] = index + 1
				if faces[other].group != nil && faces[other].group != faces[index].group {
					continue
				}
				if faces[other].contour && !faces[index].contour && ((faces[index].owner != nil && faces[index].owner == faces[other].owner) || (faces[index].group != nil && faces[index].group == faces[other].group)) {
					continue
				}
				if subject.box.overlaps(polygons[other].box) {
					candidates = append(candidates, other)
				}
			}
			return nil
		}
		if tree != nil {
			indices, err := tree.query(subject.box, budget)
			if err != nil {
				return nil, err
			}
			if err := visit(indices); err != nil {
				return nil, err
			}
		} else if grid != nil {
			if x0, y0, x1, y1, ok := grid.cellsFor(subject.box); ok {
				for y := y0; y <= y1; y++ {
					for x := x0; x <= x1; x++ {
						if err := visit(grid.cells[y*grid.nx+x]); err != nil {
							return nil, err
						}
					}
				}
				if err := visit(grid.large); err != nil {
					return nil, err
				}
			}
		}
		// Remove broad solid faces before the narrow ink ribbons around them.
		// Cutting thousands of ribbons first would unnecessarily partition a
		// large board into slivers that its few broad caps subsequently hide.
		budget.candidates = len(candidates)
		if err := budget.spend(len(candidates) * bits.Len(uint(len(candidates)))); err != nil {
			return nil, err
		}
		sort.Slice(candidates, func(a, b int) bool {
			i, j := candidates[a], candidates[b]
			if polygons[i].area != polygons[j].area {
				return polygons[i].area > polygons[j].area
			}
			if faces[i].order == faces[j].order {
				return i > j
			}
			return faces[i].order > faces[j].order
		})
		var indexedFragments *refSvgVisibleFragmentSet
		if len(candidates) > 64 {
			indexedFragments, err = refSvgNewVisibleFragmentSet(subject.points, budget, limits.fragments-outputFragments, limits.vertices-outputVertices)
			if err != nil {
				return nil, err
			}
		}
		for _, other := range candidates {
			occluder := polygons[other]
			if err := budget.spend(len(occluder.points) + 4); err != nil {
				return nil, err
			}
			cut := occluder.points
			difference := func(p svgPoint) float64 { return occluder.plane.at(p) - subject.plane.at(p) }
			lo, hi, depthMagnitude := math.Inf(1), math.Inf(-1), 1.0
			// The overlap bbox bounds both affine depth extrema without requiring
			// another polygon allocation merely to classify a coplanar pair.
			box := refSvgBox{math.Max(subject.box.minX, occluder.box.minX), math.Max(subject.box.minY, occluder.box.minY), math.Min(subject.box.maxX, occluder.box.maxX), math.Min(subject.box.maxY, occluder.box.maxY)}
			for _, p := range []svgPoint{{x: box.minX, y: box.minY}, {x: box.maxX, y: box.minY}, {x: box.maxX, y: box.maxY}, {x: box.minX, y: box.maxY}} {
				d := difference(p)
				lo, hi = math.Min(lo, d), math.Max(hi, d)
				depthMagnitude = math.Max(depthMagnitude, math.Max(math.Abs(subject.plane.at(p)), math.Abs(occluder.plane.at(p))))
			}
			epsilon := math.Max(1e-9, depthMagnitude*2e-14)
			if lo >= -epsilon && hi <= epsilon {
				if faces[other].order < faces[index].order || (faces[other].order == faces[index].order && other < index) {
					continue
				}
			} else if hi <= 0 {
				continue
			} else if lo < 0 {
				cut, _ = refSvgSplitPolygon(cut, difference)
				if len(cut) == 0 {
					continue
				}
			}
			if indexedFragments != nil {
				if err := indexedFragments.subtract(cut); err != nil {
					return nil, err
				}
				budget.fragments = len(indexedFragments.fragments)
				if budget.fragments == 0 {
					break
				}
				continue
			}
			cutBox := refSvgPolygonBox(cut)
			var next [][]svgPoint
			vertices := 0
			for _, fragment := range fragments {
				if err := budget.spend(len(fragment) + 1); err != nil {
					return nil, err
				}
				pieces := [][]svgPoint{fragment}
				if refSvgPolygonBox(fragment).overlaps(cutBox) {
					pieces, err = refSvgSubtractPolygon(fragment, cut, budget)
					if err != nil {
						return nil, err
					}
				}
				for _, piece := range pieces {
					vertices += len(piece)
				}
				next = append(next, pieces...)
				if len(next) > limits.fragments-outputFragments || vertices > limits.vertices-outputVertices {
					return nil, fmt.Errorf("isometric SVG visibility exceeds fragment limit")
				}
			}
			fragments = next
			budget.fragments = len(fragments)
			if len(fragments) == 0 {
				break
			}
		}
		if indexedFragments != nil {
			fragments, err = indexedFragments.polygons()
			if err != nil {
				return nil, err
			}
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

type refSvgVisibleFragment struct {
	points []svgPoint
	box    refSvgBox
}

// The visible pieces of a broad board can number in the thousands. Index
// those pieces as they change so a small node or ink ribbon only visits the
// local pieces it can cut, rather than scanning every existing fragment.
type refSvgVisibleFragmentSet struct {
	grid                                      refSvgVisibilityGrid
	cells                                     []map[int]struct{}
	large                                     map[int]struct{}
	fragments                                 map[int]refSvgVisibleFragment
	next, vertices, maxFragments, maxVertices int
	budget                                    *refSvgVisibilityBudget
	references                                int
}

func refSvgNewVisibleFragmentSet(points []svgPoint, budget *refSvgVisibilityBudget, maxFragments, maxVertices int) (*refSvgVisibleFragmentSet, error) {
	box := refSvgPolygonBox(points)
	w, h := box.maxX-box.minX, box.maxY-box.minY
	nx := max(1, min(32, int(math.Ceil(math.Sqrt(256*w/h)))))
	ny := max(1, min(32, int(math.Ceil(256/float64(nx)))))
	set := &refSvgVisibleFragmentSet{
		grid:  refSvgVisibilityGrid{bounds: box, nx: nx, ny: ny},
		cells: make([]map[int]struct{}, nx*ny), large: make(map[int]struct{}), fragments: make(map[int]refSvgVisibleFragment),
		maxFragments: maxFragments, maxVertices: maxVertices, budget: budget,
	}
	return set, set.add(points)
}

func (s *refSvgVisibleFragmentSet) changeCells(id int, box refSvgBox, add bool) error {
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
			s.large[id] = struct{}{}
		} else {
			delete(s.large, id)
		}
		return nil
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			index := y*s.grid.nx + x
			if add {
				if s.cells[index] == nil {
					s.cells[index] = make(map[int]struct{})
				}
				s.cells[index][id] = struct{}{}
			} else {
				delete(s.cells[index], id)
			}
		}
	}
	return nil
}

func (s *refSvgVisibleFragmentSet) add(points []svgPoint) error {
	if err := s.budget.spend(len(points) + 1); err != nil {
		return err
	}
	if len(s.fragments) >= s.maxFragments || len(points) > s.maxVertices-s.vertices {
		return fmt.Errorf("isometric SVG visibility exceeds fragment limit")
	}
	fragment := refSvgVisibleFragment{points: points, box: refSvgPolygonBox(points)}
	if err := s.changeCells(s.next+1, fragment.box, true); err != nil {
		return err
	}
	s.next++
	s.fragments[s.next] = fragment
	s.vertices += len(points)
	return nil
}

func (s *refSvgVisibleFragmentSet) query(box refSvgBox) ([]int, error) {
	seen := make(map[int]bool)
	var result []int
	visit := func(bucket map[int]struct{}) error {
		if err := s.budget.spend(len(bucket)); err != nil {
			return err
		}
		for id := range bucket {
			if seen[id] {
				continue
			}
			seen[id] = true
			if s.fragments[id].box.overlaps(box) {
				result = append(result, id)
			}
		}
		return nil
	}
	if x0, y0, x1, y1, ok := s.grid.cellsFor(box); ok {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				if err := visit(s.cells[y*s.grid.nx+x]); err != nil {
					return nil, err
				}
			}
		}
		if err := visit(s.large); err != nil {
			return nil, err
		}
	}
	if err := s.budget.spend(len(result) * bits.Len(uint(len(result)))); err != nil {
		return nil, err
	}
	sort.Ints(result)
	return result, nil
}

func (s *refSvgVisibleFragmentSet) subtract(cut []svgPoint) error {
	ids, err := s.query(refSvgPolygonBox(cut))
	if err != nil {
		return err
	}
	for _, id := range ids {
		fragment := s.fragments[id]
		pieces, err := refSvgSubtractPolygon(fragment.points, cut, s.budget)
		if err != nil {
			return err
		}
		if len(pieces) == 1 && len(pieces[0]) > 0 && &pieces[0][0] == &fragment.points[0] {
			continue
		}
		if err := s.changeCells(id, fragment.box, false); err != nil {
			return err
		}
		delete(s.fragments, id)
		s.vertices -= len(fragment.points)
		for _, piece := range pieces {
			if err := s.add(piece); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *refSvgVisibleFragmentSet) polygons() ([][]svgPoint, error) {
	if err := s.budget.spend(len(s.fragments) * bits.Len(uint(len(s.fragments)))); err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(s.fragments))
	for id := range s.fragments {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	polygons := make([][]svgPoint, 0, len(ids))
	for _, id := range ids {
		polygons = append(polygons, s.fragments[id].points)
	}
	return polygons, nil
}

type refSvgVisibilityTreeNode struct {
	box                     refSvgBox
	left, right, start, end int
}

// Dense meshes can place thousands of small faces in a uniform-grid cell.
// A median bounding-box tree keeps those queries proportional to actual local
// overlap, rather than the unrelated faces sharing a coarse spatial bucket.
type refSvgVisibilityTree struct {
	polygons []refSvgVisibilityPolygon
	indices  []int
	nodes    []refSvgVisibilityTreeNode
}

func refSvgNewVisibilityTree(polygons []refSvgVisibilityPolygon, faces []svgVisibilityFace, budget *refSvgVisibilityBudget) (*refSvgVisibilityTree, error) {
	tree := &refSvgVisibilityTree{polygons: polygons}
	for i, p := range polygons {
		if len(p.points) > 0 && faces[i].opaque {
			tree.indices = append(tree.indices, i)
		}
	}
	if len(tree.indices) == 0 {
		return tree, nil
	}
	var build func(int, int) (int, error)
	build = func(start, end int) (int, error) {
		if err := budget.spend(end - start); err != nil {
			return 0, err
		}
		box := refSvgBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		centers := box
		for _, id := range tree.indices[start:end] {
			b := polygons[id].box
			box.minX, box.minY = math.Min(box.minX, b.minX), math.Min(box.minY, b.minY)
			box.maxX, box.maxY = math.Max(box.maxX, b.maxX), math.Max(box.maxY, b.maxY)
			x, y := (b.minX+b.maxX)/2, (b.minY+b.maxY)/2
			centers.minX, centers.minY = math.Min(centers.minX, x), math.Min(centers.minY, y)
			centers.maxX, centers.maxY = math.Max(centers.maxX, x), math.Max(centers.maxY, y)
		}
		index := len(tree.nodes)
		tree.nodes = append(tree.nodes, refSvgVisibilityTreeNode{box: box, left: -1, right: -1, start: start, end: end})
		if end-start <= 16 {
			return index, nil
		}
		xAxis := centers.maxX-centers.minX >= centers.maxY-centers.minY
		center := func(id int) float64 {
			b := polygons[id].box
			if xAxis {
				return b.minX + b.maxX
			}
			return b.minY + b.maxY
		}
		if err := budget.spend((end - start) * bits.Len(uint(end-start))); err != nil {
			return 0, err
		}
		sort.Slice(tree.indices[start:end], func(i, j int) bool {
			a, b := tree.indices[start+i], tree.indices[start+j]
			ca, cb := center(a), center(b)
			if ca == cb {
				return a < b
			}
			return ca < cb
		})
		middle := (start + end) / 2
		left, err := build(start, middle)
		if err != nil {
			return 0, err
		}
		right, err := build(middle, end)
		if err != nil {
			return 0, err
		}
		tree.nodes[index].left, tree.nodes[index].right = left, right
		return index, nil
	}
	_, err := build(0, len(tree.indices))
	return tree, err
}

func (t *refSvgVisibilityTree) query(box refSvgBox, budget *refSvgVisibilityBudget) ([]int, error) {
	if len(t.nodes) == 0 {
		return nil, nil
	}
	stack := []int{0}
	var result []int
	for len(stack) > 0 {
		if err := budget.spend(1); err != nil {
			return nil, err
		}
		index := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := t.nodes[index]
		if !node.box.overlaps(box) {
			continue
		}
		if node.left >= 0 {
			stack = append(stack, node.right, node.left)
			continue
		}
		if err := budget.spend(node.end - node.start); err != nil {
			return nil, err
		}
		for _, id := range t.indices[node.start:node.end] {
			if t.polygons[id].box.overlaps(box) {
				result = append(result, id)
			}
		}
	}
	return result, nil
}
