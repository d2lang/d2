package d2isometricimg

import (
	"context"
	"math"
	"math/rand"
	"testing"
)

// svgVisibilityRandomScene builds a mesh that mixes the cases the solver
// distinguishes: broad boards cut by many small faces (the indexed fragment
// set), stacked and intersecting planes (depth cuts), coplanar ties, opacity
// groups, contours, decals, degenerate faces and both windings.
func svgVisibilityRandomScene(r *rand.Rand, faces int) []svgVisibilityFace {
	groups := []*nativeOpacityGroup{nil, nil, nil, {Opacity: .5}, {Opacity: .25}}
	owners := []*nativePaintOwner{nil, nil, {}, {}}
	scene := make([]svgVisibilityFace, 0, faces)
	quad := func(x, y, w, h, z, dx, dy float64) []svgPoint {
		return svgVisibilityRectangle(x, y, x+w, y+h, z, dx, dy)
	}
	for len(scene) < faces {
		face := svgVisibilityFace{
			opaque: r.Intn(5) != 0, group: groups[r.Intn(len(groups))], owner: owners[r.Intn(len(owners))],
			contour: r.Intn(7) == 0, order: len(scene),
		}
		if r.Intn(6) == 0 {
			face.order = r.Intn(4)
		}
		x, y := r.Float64()*400, r.Float64()*300
		z := float64(r.Intn(12))
		var dx, dy float64
		if r.Intn(3) == 0 {
			dx, dy = (r.Float64()-.5)*.2, (r.Float64()-.5)*.2
		}
		switch r.Intn(12) {
		case 0: // Broad board: enough occluders to use the fragment index.
			face.points = quad(r.Float64()*40, r.Float64()*40, 300+r.Float64()*100, 200+r.Float64()*100, z, dx, dy)
		case 1: // Thin ribbon.
			face.points = quad(x, y, 60+r.Float64()*200, .5+r.Float64(), z, dx, dy)
		case 2: // Triangle with either winding.
			face.points = []svgPoint{{x, y, z}, {x + 5 + r.Float64()*30, y + r.Float64()*8, z + dx}, {x + r.Float64()*8, y + 5 + r.Float64()*30, z + dy}}
			if r.Intn(2) == 0 {
				face.points[1], face.points[2] = face.points[2], face.points[1]
			}
		case 3: // Exactly coplanar duplicate of an earlier face.
			if len(scene) > 0 {
				face.points = append([]svgPoint(nil), scene[r.Intn(len(scene))].points...)
			}
		case 4: // Degenerate: collinear, repeated or empty.
			face.points = [][]svgPoint{nil, {{x, y, z}, {x + 1, y + 1, z}, {x + 2, y + 2, z}}, {{x, y, z}, {x, y, z}, {x + 3, y, z}, {x + 3, y + 3, z}}}[r.Intn(3)]
		case 5: // Snapped to a lattice so edges and vertices coincide.
			gx, gy := float64(r.Intn(40))*10, float64(r.Intn(30))*10
			face.points = quad(gx, gy, float64(1+r.Intn(4))*10, float64(1+r.Intn(4))*10, z, 0, 0)
		default: // Ordinary node-sized face.
			face.points = quad(x, y, 4+r.Float64()*40, 4+r.Float64()*30, z, dx, dy)
		}
		scene = append(scene, face)
	}
	return scene
}

// svgVisibilityLatticeScene lays overlapping tiles on a lattice. Each tile has
// few neighbors, so a scene large enough for the bounding-box tree is still
// cheap to solve, and its many equal coordinates exercise every tie-break.
func svgVisibilityLatticeScene(r *rand.Rand, faces int) []svgVisibilityFace {
	columns := int(math.Ceil(math.Sqrt(float64(faces))))
	scene := make([]svgVisibilityFace, faces)
	for i := range scene {
		x, y := float64(i%columns)*10, float64(i/columns)*10
		scene[i] = svgVisibilityFace{points: svgVisibilityRectangle(x, y, x+12, y+12, float64(r.Intn(3)), 0, 0), opaque: r.Intn(8) != 0, order: i}
	}
	return scene
}

func svgVisibilitySameFragments(a, b [][][]svgPoint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		// A face without fragments is nil or empty exactly as it was.
		if len(a[i]) != len(b[i]) || (a[i] == nil) != (b[i] == nil) {
			return false
		}
		for j := range a[i] {
			if len(a[i][j]) != len(b[i][j]) {
				return false
			}
			for k, p := range a[i][j] {
				q := b[i][j][k]
				// Bit patterns distinguish values that == conflates, such as
				// the sign of zero.
				if math.Float64bits(p.x) != math.Float64bits(q.x) || math.Float64bits(p.y) != math.Float64bits(q.y) || math.Float64bits(p.z) != math.Float64bits(q.z) {
					return false
				}
			}
		}
	}
	return true
}

// svgVisibilityCompare solves faces with the solver and with the reference
// solver, and fails unless they agree on the error and on every fragment.
func svgVisibilityCompare(t *testing.T, name string, faces []svgVisibilityFace, limits svgVisibilityLimits) {
	t.Helper()
	want, wantErr := refSvgVisibleFacesWithLimits(context.Background(), faces, limits)
	got, gotErr := svgVisibleFacesWithLimits(context.Background(), faces, limits)
	if (wantErr == nil) != (gotErr == nil) || (wantErr != nil && wantErr.Error() != gotErr.Error()) {
		t.Fatalf("%s, limits %+v: error %v, reference %v", name, limits, gotErr, wantErr)
	}
	if !svgVisibilitySameFragments(got, want) {
		t.Fatalf("%s, limits %+v: fragments differ from the reference solver", name, limits)
	}
}

// The solver's fragments are serialized directly into SVG paths, so it must
// reproduce the reference solver's vertices bit for bit and in the same order.
func TestSVGVisibilityMatchesReferenceSolver(t *testing.T) {
	sizes := []int{0, 1, 2, 7, 40, 150, 400, 900}
	if !testing.Short() {
		sizes = append(sizes, 9000) // More than 8192 faces use the bounding-box tree.
	}
	for seed := int64(1); seed <= 4; seed++ {
		for _, size := range sizes {
			if size > 1000 && seed > 1 {
				continue
			}
			faces := svgVisibilityRandomScene(rand.New(rand.NewSource(seed*1000+int64(size))), size)
			limits := svgDefaultVisibilityLimits
			limits.work = svgVisibilityWorkLimit(len(faces))
			svgVisibilityCompare(t, "random scene", faces, limits)
		}
	}

	// A small face under more occluders than the fragment index needs is
	// hidden entirely, which is an empty list of fragments rather than none.
	hidden := []svgVisibilityFace{{points: svgVisibilityRectangle(40, 40, 50, 50, 0, 0, 0), opaque: true}}
	for i := 1; i <= 70; i++ {
		hidden = append(hidden, svgVisibilityFace{points: svgVisibilityRectangle(0, 0, 100+float64(i), 100, float64(i), 0, 0), opaque: true, order: i})
	}
	svgVisibilityCompare(t, "hidden face", hidden, svgDefaultVisibilityLimits)

	// A degenerate face produces nothing, so it cannot exceed an output limit.
	limits := svgDefaultVisibilityLimits
	limits.fragments = -1
	svgVisibilityCompare(t, "degenerate face", []svgVisibilityFace{{}}, limits)
	svgVisibilityCompare(t, "degenerate then ordinary face", []svgVisibilityFace{{}, hidden[0]}, limits)
}

// Work accounting decides which meshes are rejected, so a limit must trip for
// the same inputs and report the same error as in the reference solver,
// wherever in the solve it is reached.
func TestSVGVisibilityLimitsMatchReferenceSolver(t *testing.T) {
	type limit struct {
		name string
		set  func(*svgVisibilityLimits, int)
		max  int
	}
	all := []limit{
		{"work", func(l *svgVisibilityLimits, v int) { l.work = v }, svgDefaultVisibilityLimits.work},
		{"fragments", func(l *svgVisibilityLimits, v int) { l.fragments = v }, svgDefaultVisibilityLimits.fragments},
		{"vertices", func(l *svgVisibilityLimits, v int) { l.vertices = v }, svgDefaultVisibilityLimits.vertices},
		{"gridReferences", func(l *svgVisibilityLimits, v int) { l.gridReferences = v }, svgDefaultVisibilityLimits.gridReferences},
	}
	scenes := []struct {
		name   string
		faces  []svgVisibilityFace
		limits []limit
		probes int
	}{
		{"random scene", svgVisibilityRandomScene(rand.New(rand.NewSource(7)), 300), all, 300},
	}
	if !testing.Short() {
		// More than 8192 faces use the bounding-box tree, whose build and
		// queries are charged differently from the grid's.
		scenes = append(scenes, struct {
			name   string
			faces  []svgVisibilityFace
			limits []limit
			probes int
		}{"lattice scene", svgVisibilityLatticeScene(rand.New(rand.NewSource(11)), 8300), all[:1], 16})
	}
	for _, scene := range scenes {
		base := svgDefaultVisibilityLimits
		r := rand.New(rand.NewSource(int64(len(scene.faces))))
		for _, limit := range scene.limits {
			// Find the least value the reference solver succeeds with by
			// bisection, then probe around it and at random values below it,
			// which stop the solve at as many different places.
			lo, hi := 0, limit.max
			for lo < hi {
				mid := lo + (hi-lo)/2
				limits := base
				limit.set(&limits, mid)
				if _, err := refSvgVisibleFacesWithLimits(context.Background(), scene.faces, limits); err != nil {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			probes := []int{-1, 0, 1, lo / 2, lo - 1, lo, lo + 1}
			count := scene.probes
			if limit.name != "work" {
				count /= 6
			}
			for range count {
				probes = append(probes, r.Intn(lo+1))
			}
			for _, value := range probes {
				limits := base
				limit.set(&limits, value)
				svgVisibilityCompare(t, scene.name+", "+limit.name, scene.faces, limits)
			}
		}
		limits := base
		limits.faces = len(scene.faces) - 1
		svgVisibilityCompare(t, scene.name+", faces", scene.faces, limits)
	}
}

// A worker that grew large on one render is not kept for the next.
func TestSVGVisibilityWorkerRetentionCountsAllScratch(t *testing.T) {
	var worker svgVisibilityWorker
	if worker.retainedBytes() != 0 {
		t.Fatal("an unused worker retains storage")
	}
	// Removed fragments keep their slots, so the slots alone can be large.
	worker.set.fragments = make([]svgVisibleFragment, 0, svgVisibilityWorkerRetainedBytes/32)
	if worker.retainedBytes() <= svgVisibilityWorkerRetainedBytes {
		t.Fatal("fragment slots are not counted toward the retained storage")
	}
	worker = svgVisibilityWorker{}
	worker.pool.chunks = [][]svgPoint{make([]svgPoint, svgPoolChunk)}
	worker.seen = make([]int32, 1000)
	if got := worker.retainedBytes(); got < svgPoolChunk*24+4000 || got > svgVisibilityWorkerRetainedBytes {
		t.Fatalf("a small worker retains %d bytes", got)
	}
}

func BenchmarkSVGVisibility(b *testing.B) {
	faces := svgVisibilityRandomScene(rand.New(rand.NewSource(3)), 3000)
	limits := svgDefaultVisibilityLimits
	limits.work = svgVisibilityWorkLimit(len(faces))
	b.Run("solver", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := svgVisibleFacesWithLimits(context.Background(), faces, limits); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("reference", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := refSvgVisibleFacesWithLimits(context.Background(), faces, limits); err != nil {
				b.Fatal(err)
			}
		}
	})
}
