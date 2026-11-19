package d2isometricimg

import (
	"math"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2isometric"
)

// Only links to other included layers can change the issuing object. An empty
// entry requests a hatch, and is replaced with its actual mouth after painting.
func requestedStackApertures(scene *d2isometric.Scene, floors stackFloorIndex, from int) map[string]stackAperture {
	var openings map[string]stackAperture
	for _, n := range scene.Nodes {
		if n.Link == "" || n.SequenceRole == "span" || n.SequenceRole == "group" || n.Opacity*255 < .5 || hierarchySpacer(n) || min(n.Size.X, n.Size.Z) <= .01 {
			continue
		}
		if _, ok := floors.destination(n.Link, from); !ok {
			continue
		}
		if openings == nil {
			openings = make(map[string]stackAperture)
		}
		openings[n.ID] = stackAperture{}
	}
	return openings
}

const hatchLidRise = .26

func leafStackOpening(n d2isometric.Node) stackAperture {
	return stackAperture{center: nv(n.Position.X, n.Position.Y, n.Position.Z+n.Size.Z*.235), width: n.Size.X * .86, depth: n.Size.Z * .51, opacity: n.Opacity}
}

// Cut only the mounting boards underneath the issuing object. Siblings or
// overlapping foreground containers keep their own printed surfaces.
func (b *meshBuilder) planStackHatches(nodes []d2isometric.Node, boards []d2isometric.Board, captions *routeCaptionPlacer) {
	if len(b.options.stackApertures) == 0 {
		return
	}
	byID, byOwner := make(map[string]d2isometric.Board, len(boards)), make(map[string]d2isometric.Board, len(boards))
	for _, board := range boards {
		byID[board.ID], byOwner[board.SourceID] = board, board
	}
	direct := make(map[string][]stackAperture)
	// Header placement uses presentation copies of the nodes. Include the
	// lifted rear edge in the obstacle envelope before placing any headings.
	for i := range nodes {
		n := &nodes[i]
		if _, ok := b.options.stackApertures[n.ID]; ok && !n.Container {
			lift := hatchLidRise * n.Size.Z
			n.Position.Y += lift / 2
			n.Size.Y += lift
		}
	}
	for _, n := range nodes {
		if _, ok := b.options.stackApertures[n.ID]; !ok {
			continue
		}
		opening := leafStackOpening(n)
		support := n.BoardID
		if n.Container {
			board, ok := byOwner[n.ID]
			if !ok {
				continue
			}
			// Reserve a modest mouth inside the container, leaving all
			// mounted children in place. The header uses the remaining room.
			n.Metadata.Original.LabelPosition = "INSIDE_BOTTOM_RIGHT"
			s := labelSurface{center: board.Position, width: min(1.25, board.Size.X*.34), depth: min(.65, board.Size.Z*.24)}
			s.center.Y = hierarchySurfaceY(board) + .00006
			s, ok = placeHierarchyHeader(s, board, n, nodes, b.scale, boards, captions)
			if !ok {
				continue
			}
			captions.Avoid(s.center, s.width, s.depth)
			opening = stackAperture{center: s.center, width: s.width * .88, depth: s.depth * .84, opacity: n.Opacity}
			opening.center.Y += s.depth * .08
			b.options.stackApertures[n.ID] = opening
			support = board.ID
		}
		if support != "" {
			direct[support] = append(direct[support], opening)
		}
	}
	// Each subtree occupies one range of a shared buffer, even for deeply
	// nested containers with many linked descendants.
	children := make(map[string][]string, len(boards))
	for _, board := range boards {
		children[board.ParentID] = append(children[board.ParentID], board.ID)
	}
	ordered := make([]stackAperture, 0, len(b.options.stackApertures))
	b.stackBoardCuts = make(map[string][]stackAperture, len(boards))
	var visit func(string)
	visit = func(id string) {
		if _, done := b.stackBoardCuts[id]; done {
			return
		}
		b.stackBoardCuts[id] = nil
		start := len(ordered)
		ordered = append(ordered, direct[id]...)
		for _, child := range children[id] {
			visit(child)
		}
		b.stackBoardCuts[id] = ordered[start:]
	}
	for _, board := range boards {
		if _, hasParent := byID[board.ParentID]; !hasParent {
			visit(board.ID)
		}
	}
}

// A container face is a textured quad, including any transparent margin
// around the original shape. Tile it around the wells in its own coordinates.
func (b *meshBuilder) cutStackBoardFace(s labelSurface, cuts []stackAperture, first int) {
	if len(cuts) == 0 || len(b.triangles) != first+2 {
		return
	}
	left, back := s.center.X-s.width/2, s.center.Z-s.depth/2
	right, front := left+s.width, back+s.depth
	rows, edges := stockCutGrid(left, right, back, front, slices.Values(cuts))
	if len(edges) == 0 {
		return
	}
	face := b.triangles[first]
	b.triangles = b.triangles[:first]
	materials := [256]*Material{255: face.Material}
	b.stockStrips(left, right, rows, edges, func(strip stockSpan, z0, z1 float64) {
		if strip.alpha == 0 {
			return
		}
		m := materials[strip.alpha]
		if m == nil {
			v := *face.Material
			v.Color.A = uint8(math.Round(float64(v.Color.A) * float64(strip.alpha) / 255))
			m = &v
			materials[strip.alpha] = m
		}
		vertex := func(x, z float64) Vertex {
			return Vertex{Position: nv(x, s.center.Y, z), Normal: nv(0, 1, 0), U: (x - left) / s.width, V: (z - back) / s.depth}
		}
		a, c, d, e := vertex(strip.left, z0), vertex(strip.left, z1), vertex(strip.right, z1), vertex(strip.right, z0)
		first := len(b.triangles)
		b.triangle(a, c, d, m, face.CastShadow)
		b.triangle(a, d, e, m, face.CastShadow)
		for i := first; i < len(b.triangles); i++ {
			b.triangles[i].DepthBias = face.DepthBias
		}
	}, nil)
}

// The artwork is the lid of the issuing object. Slide and incline it over
// the rear of its original footprint, preserving the printed face intact.
// The front is an actual opening. Its walls run down to the slab cutout.
func (b *meshBuilder) openStackHatch(n d2isometric.Node, first, firstLink int) {
	if _, ok := b.options.stackApertures[n.ID]; !ok || first == len(b.triangles) {
		return
	}
	w, d := n.Size.X, n.Size.Z
	back := n.Position.Z - d/2
	front := back + d
	const depthScale, rise = .44, hatchLidRise
	transform := func(p Vec) Vec {
		return nv(p.X, p.Y+(front-p.Z)*rise, back+d*.04+(p.Z-back)*depthScale)
	}
	// A caption behind the rear hinge keeps the clearance it had above
	// the original flat artwork. Its separate hit region moves with it.
	northCaption := strings.HasPrefix(n.Metadata.Original.LabelPosition, "OUTSIDE_TOP")
	captionOffset := nv(0, 0, 0)
	if northCaption {
		captionOffset = nv(0, rise*d, .04*d)
	}
	bottom, top := math.Inf(1), math.Inf(-1)
	var owner *nativePaintOwner
	var group *nativeOpacityGroup
	for i := first; i < len(b.triangles); i++ {
		tri := &b.triangles[i]
		if tri.Material.outsideCaption {
			for j := range tri.V {
				tri.V[j].Position = nadd(tri.V[j].Position, captionOffset)
			}
			continue
		}
		group, owner = tri.OpacityGroup, tri.PaintOwner
		for j := range tri.V {
			v := &tri.V[j]
			bottom, top = min(bottom, v.Position.Y), max(top, v.Position.Y)
			v.Position = transform(v.Position)
			// Transform normals by the inverse transpose of the lid's shear.
			v.Normal = nunit(nv(v.Normal.X, v.Normal.Y, (v.Normal.Z+rise*v.Normal.Y)/depthScale))
		}
	}
	if math.IsInf(bottom, 1) {
		return
	}
	for i := firstLink; i < len(b.links); i++ {
		link := &b.links[i]
		if link.meshEnd > 0 {
			continue
		}
		mid := nlerp(link.points[0], link.points[2], .5)
		if mid.Z < back || mid.Z > front || math.Abs(mid.X-n.Position.X) > w/2 {
			if northCaption && mid.Z < back {
				for j, p := range link.points {
					link.points[j] = nadd(p, captionOffset)
				}
			}
			continue
		}
		for j, p := range link.points {
			link.points[j] = transform(p)
		}
	}
	h := max(d*.055, min(d*.12, (top-bottom)*.39))
	lipY := bottom + h
	opening := leafStackOpening(n)
	opening.center.Y = lipY
	b.options.stackApertures[n.ID] = opening
	firstRim := len(b.triangles)
	opacity := n.Opacity
	if group != nil {
		opacity = 1
	}
	b.stackHatchWalls(opening, max(.008, min(w, d)*.042), min(0, bottom), opacity)
	for i := firstRim; i < len(b.triangles); i++ {
		b.triangles[i].OpacityGroup = group
		b.triangles[i].PaintOwner = owner
	}
}

func (b *meshBuilder) stackHatchWalls(s stackAperture, rail, floor, opacity float64) {
	rect := func(extra, y float64) [4]Vec {
		x, z := s.width/2+extra, s.depth/2+extra
		p := s.center
		return [4]Vec{nv(p.X-x, y, p.Z-z), nv(p.X-x, y, p.Z+z), nv(p.X+x, y, p.Z+z), nv(p.X+x, y, p.Z-z)}
	}
	face := func(a, c, d, e Vec, m *Material) {
		b.flat(a, c, d, m, true)
		b.flat(a, d, e, m, true)
	}
	lip := nativeMaterial("#8e958a", .82, .08, opacity)
	bevel := nativeMaterial("#535f59", .8, .12, opacity)
	inside := nativeMaterial("#34423f", .9, 0, opacity)
	outside := nativeMaterial("#717b70", .88, .06, opacity)
	outer := rect(rail, s.center.Y)
	step := rect(rail*.39, s.center.Y)
	throat := rect(0, s.center.Y-rail*.36)
	base, foot := rect(0, floor), rect(rail, floor)
	if b.options.vector {
		// SVG clips this invisible hit surface against the same occluders
		// as the physical rim, including other objects and higher floors.
		first := len(b.triangles)
		hit := nativeMaterial("white", 1, 0, 1)
		hit.svgHitOnly = true
		b.flat(throat[0], throat[1], throat[2], hit, false)
		b.flat(throat[0], throat[2], throat[3], hit, false)
		for i := first; i < len(b.triangles); i++ {
			b.triangles[i].NoDepthWrite = true
		}
	}
	for i, a := range outer {
		j := (i + 1) % 4
		face(a, outer[j], step[j], step[i], lip)
		face(step[i], step[j], throat[j], throat[i], bevel)
		face(throat[i], throat[j], base[j], base[i], inside)
		face(a, foot[i], foot[j], outer[j], outside)
	}
}
