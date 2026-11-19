package d2isometricimg

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"image/color"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2target"
)

// SVG consumes the same physical scene as PNG. Visibility is resolved in
// projected geometry; neither a framebuffer nor a screen-sized image is used.
func renderNativeSVG(ctx context.Context, diagram *d2target.Diagram, o Options) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = nativeVectorContext(ctx)
	if o.LinkBudget == nil {
		o.LinkBudget = &d2scenebuild.LinkBudget{MaxRegions: 4096, MaxStringBytes: 1 << 20}
	}
	if !o.Exploded {
		room := *o.LinkBudget
		if err := preflightSVGMetadata(ctx, diagram, &room); err != nil {
			return nil, err
		}
	}
	native, err := buildOutputScene(ctx, diagram, o, true)
	if err != nil {
		return nil, fmt.Errorf("initialize native SVG: %w", err)
	}
	return nativeSceneSVG(ctx, native)
}

func preflightSVGMetadata(ctx context.Context, diagram *d2target.Diagram, budget *d2scenebuild.LinkBudget) error {
	if diagram == nil {
		return fmt.Errorf("native SVG requires a diagram")
	}
	bytes, regions := 0, 0
	check := func(link, tooltip string) error {
		if link == "" && tooltip == "" {
			return ctx.Err()
		}
		regions++
		if regions > budget.MaxRegions {
			return fmt.Errorf("native SVG exceeds link region budget")
		}
		for _, value := range []string{link, tooltip} {
			if len(value) > budget.MaxStringBytes-bytes {
				return fmt.Errorf("native SVG exceeds link text budget")
			}
			bytes += len(value)
			if err := validatePageLinkText(ctx, value); err != nil {
				return err
			}
		}
		return validateNativeLink(link)
	}
	for _, s := range diagram.Shapes {
		if err := check(s.Link, s.Tooltip); err != nil {
			return err
		}
	}
	for _, e := range diagram.Connections {
		if err := check(e.Link, e.Tooltip); err != nil {
			return err
		}
	}
	budget.MaxRegions -= regions
	budget.MaxStringBytes -= bytes
	return ctx.Err()
}

type nativeSVGWriter struct {
	ctx           context.Context
	buf           bytes.Buffer
	err           error
	surfaces      map[*nativeVectorSurface]string
	lightingStops map[string]string
}

func (w *nativeSVGWriter) write(format string, args ...any) {
	if w.err != nil {
		return
	}
	if w.err = w.ctx.Err(); w.err != nil {
		return
	}
	fmt.Fprintf(&w.buf, format, args...)
	if w.buf.Len() > MaxOutputBytes {
		w.err = fmt.Errorf("native SVG exceeds %d output bytes", MaxOutputBytes)
	}
}

func svgNumber(v float64) string {
	if math.Abs(v) < .0000005 {
		return "0"
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 6, 64), "0"), ".")
}

func svgColor(r, g, b float64) string {
	return fmt.Sprintf("#%02x%02x%02x", uint8(math.Round(rasterClamp(r)*255)), uint8(math.Round(rasterClamp(g)*255)), uint8(math.Round(rasterClamp(b)*255)))
}

// Normalize at the same precision used to serialize the SVG. Matching edges
// may be cancelled only after their endpoints have been rounded and cleaned.
func svgCanonicalLoop(polygon []svgPoint) []svgPoint {
	p := make([]svgPoint, 0, len(polygon))
	for _, v := range polygon {
		v = svgPoint{x: math.Round(v.x*1e6) / 1e6, y: math.Round(v.y*1e6) / 1e6}
		if len(p) == 0 || v != p[len(p)-1] {
			p = append(p, v)
		}
	}
	if len(p) > 1 && p[0] == p[len(p)-1] {
		p = p[:len(p)-1]
	}
	p = svgOmitCollinearSplits(p)
	if len(p) < 3 {
		return nil
	}
	area := 2 * svgPolygonArea(p)
	if math.Abs(area) < 1e-10 {
		return nil
	}
	if area < 0 {
		slices.Reverse(p)
	}
	first := 0
	for i := range p {
		if p[i].x < p[first].x || p[i].x == p[first].x && p[i].y < p[first].y {
			first = i
		}
	}
	return append(p[first:len(p):len(p)], p[:first]...)
}

func svgWriteLoop(s *strings.Builder, loop []svgPoint) {
	for i, p := range loop {
		if i == 0 {
			s.WriteByte('M')
		} else {
			s.WriteByte('L')
		}
		s.WriteString(svgNumber(p.x))
		s.WriteByte(' ')
		s.WriteString(svgNumber(p.y))
	}
	if len(loop) > 0 {
		s.WriteByte('Z')
	}
}

func svgPolygonPath(polygons [][]svgPoint) string {
	var s strings.Builder
	for _, polygon := range polygons {
		svgWriteLoop(&s, svgCanonicalLoop(polygon))
	}
	return s.String()
}

// A nonzero fill depends only on the net directed edges. Two neighboring
// fragments with a shared edge traverse it in opposite directions. Cancel
// such edges before serializing large fills, keeping every exterior and hole
// boundary at full precision. Stroked paths still need their interior edges.
func svgFillPath(polygons [][]svgPoint) string {
	if len(polygons) < 8 {
		return svgPolygonPath(polygons)
	}
	// Bound the extra edge tables on large scenes. The ordinary compound
	// path has the same fill and can be written one polygon at a time.
	const maxEdges = 1 << 16
	remaining := maxEdges
	for _, polygon := range polygons {
		if len(polygon) > remaining {
			return svgPolygonPath(polygons)
		}
		remaining -= len(polygon)
	}
	type edge struct{ from, to svgPoint }
	var edges []edge
	pending := make(map[edge]int)
	for _, polygon := range polygons {
		p := svgCanonicalLoop(polygon)
		for i, a := range p {
			e := edge{a, p[(i+1)%len(p)]}
			back := edge{e.to, e.from}
			if pending[back] > 0 {
				pending[back]--
				continue
			}
			pending[e]++
			edges = append(edges, e)
		}
	}
	// Build adjacency in input order. Avoid map iteration so snapshots and
	// path winding remain deterministic even at point-touching components.
	next := make(map[svgPoint][]svgPoint)
	for _, e := range edges {
		if pending[e] > 0 {
			next[e.from] = append(next[e.from], e.to)
			pending[e]--
		}
	}
	var s strings.Builder
	for _, e := range edges {
		start := e.from
		for len(next[start]) > 0 {
			var loop []svgPoint
			for at := start; ; {
				loop = append(loop, at)
				out := next[at]
				atNext := out[len(out)-1]
				next[at] = out[:len(out)-1]
				at = atNext
				if at == start {
					break
				}
			}
			svgWriteLoop(&s, loop)
		}
	}
	return s.String()
}

// Clipping nearly coincident planes can place a redundant intersection at
// different positions along the same straight edge on different CPUs. Drop
// such splits only after projection, at the SVG's six-decimal output precision;
// the visibility mesh and its depth values remain unchanged. Work in place on
// the serializer's private, rounded copy, retaining at least three vertices.
func svgOmitCollinearSplits(points []svgPoint) []svgPoint {
	if len(points) <= 3 {
		return points
	}
	redundant := func(a, b, c svgPoint) bool {
		dx, dy := c.x-a.x, c.y-a.y
		px, py := b.x-a.x, b.y-a.y
		length2 := float64(dx*dx) + float64(dy*dy)
		along := float64(px*dx) + float64(py*dy)
		if length2 == 0 || along < 0 || along > length2 {
			return false
		}
		cross := float64(px*dy) - float64(py*dx)
		return math.Abs(cross) <= 1e-6*math.Sqrt(length2)
	}
	clean := points[:0]
	for i, p := range points {
		for len(clean) > 1 && len(clean)+len(points)-i > 3 && redundant(clean[len(clean)-2], clean[len(clean)-1], p) {
			clean = clean[:len(clean)-1]
		}
		clean = append(clean, p)
	}
	for len(clean) > 3 {
		if redundant(clean[len(clean)-1], clean[0], clean[1]) {
			clean = clean[1:]
		} else if redundant(clean[len(clean)-2], clean[len(clean)-1], clean[0]) {
			clean = clean[:len(clean)-1]
		} else {
			break
		}
	}
	return clean
}

type svgPaintBatch struct {
	cover    *svgPaintCover
	triangle Triangle
	polygons [][]svgPoint
	depth    float64
	first    int
	affine   [6]float64
	texture  bool
	color    string
	gradient [3]Vec
	points   [3]svgPoint
}

type svgPaintUnit struct {
	link    *nativeLink
	batches []*svgPaintBatch
	depth   float64
	count   int
	first   int
	opacity float64
	opaque  bool
}

type svgBatchKey struct {
	material *Material
	affine   [6]int64
	color    string
	unique   int
}

func nativeSceneSVG(ctx context.Context, scene *nativeScene) ([]byte, error) {
	if scene == nil {
		return nil, fmt.Errorf("native SVG requires a scene")
	}
	if err := rasterValidate(ctx, scene.triangles); err != nil {
		return nil, err
	}
	camera := scene.camera
	faces := make([]svgVisibilityFace, len(scene.triangles))
	for i, t := range scene.triangles {
		f := &faces[i]
		f.order, f.group = i, t.OpacityGroup
		f.owner = t.PaintOwner
		m := t.Material
		f.contour = m != nil && m.svgContour
		paint := rasterMaterial(m)
		if paint.alpha == 0 {
			continue
		}
		if paint.alpha < .999 || paint.multiply {
			n := nadd(nadd(t.V[0].Normal, t.V[1].Normal), t.V[2].Normal)
			if ndot(n, n) > 1e-20 && ndot(n, camera.direction) <= 0 {
				continue
			}
		}
		f.opaque = !t.NoDepthWrite && paint.alpha == 1 && (paint.texture == nil || m.svgSolidTexture) && !paint.multiply
		for _, v := range t.V {
			p := camera.project(v.Position)
			f.points = append(f.points, svgProjectedPoint(p.x, p.y, p.z+t.DepthBias))
		}
	}
	visible, err := svgVisibleFaces(ctx, faces)
	if err != nil {
		return nil, err
	}
	type unitKey struct {
		link     *nativeLink
		group    *nativeOpacityGroup
		owner    *nativePaintOwner
		material *Material
	}
	units := []*svgPaintUnit{}
	unitIndex := make(map[unitKey]int)
	batchIndexes := []map[svgBatchKey]*svgPaintBatch{}
	// Object spans follow mesh construction order. Put the link around
	// the visible paint so gaps between a body and its outside label and
	// areas occluded by other objects keep their own hit targets.
	var meshLinks []*nativeLink
	for i := range scene.links {
		if scene.links[i].meshEnd > 0 {
			meshLinks = append(meshLinks, &scene.links[i])
		}
	}
	shade := &Raster{camera: camera}
	for i, t := range scene.triangles {
		if t.svgCoverageOnly || len(visible[i]) == 0 || t.Material != nil && t.Material.Color.A == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m := t.Material
		if m == nil {
			m = &Material{Color: color.NRGBA{240, 243, 249, 255}, Roughness: .35}
			t.Material = m
		}
		key := unitKey{group: t.OpacityGroup}
		for len(meshLinks) > 0 && i >= meshLinks[0].meshEnd {
			meshLinks = meshLinks[1:]
		}
		if len(meshLinks) > 0 && i >= meshLinks[0].meshStart {
			key.link = meshLinks[0]
		}
		if key.group == nil {
			key.owner = t.PaintOwner
		}
		if key.group == nil && key.owner == nil {
			key.material = m
		}
		unit, exists := unitIndex[key]
		if !exists {
			unit = len(units)
			unitIndex[key] = unit
			u := &svgPaintUnit{link: key.link, first: i, opacity: 1, opaque: faces[i].opaque}
			if key.owner != nil {
				u.opaque = key.owner.Opaque
			}
			if key.group != nil {
				u.opacity, u.opaque = key.group.Opacity, false
			}
			units = append(units, u)
			batchIndexes = append(batchIndexes, make(map[svgBatchKey]*svgPaintBatch))
		}
		u := units[unit]
		depth := 0.
		for _, p := range faces[i].points {
			depth += p.z / 3
		}
		u.depth, u.count = u.depth+depth, u.count+1
		batch := &svgPaintBatch{triangle: t, polygons: visible[i], depth: depth, first: i}
		copy(batch.points[:], faces[i].points)
		bk := svgBatchKey{material: m}
		if m.Texture != nil {
			if m.Vector == nil {
				return nil, fmt.Errorf("native SVG surface %d has no retained vector source", i)
			}
			batch.texture = true
			batch.affine, err = svgTextureAffine(t, batch.points)
			if err != nil {
				return nil, err
			}
			for j, v := range batch.affine {
				bk.affine[j] = int64(math.Round(v * 1e8))
			}
		} else {
			flat := true
			normal := nunit(ncross(nsub(t.V[1].Position, t.V[0].Position), nsub(t.V[2].Position, t.V[0].Position)))
			var firstNormal Vec
			for j, v := range t.V {
				n := v.Normal
				if nlen(n) < 1e-12 {
					n = normal
				}
				r, g, b := shade.shade(rasterMaterial(m), v.Position, n)
				batch.gradient[j] = nv(r, g, b)
				if j == 0 {
					firstNormal = nunit(n)
				} else if !m.Unlit && ndot(firstNormal, nunit(n)) < 1-1e-10 {
					flat = false
				}
				if j > 0 && nlen(nsub(batch.gradient[0], batch.gradient[j])) > 1./510 {
					flat = false
				}
			}
			if flat {
				c := batch.gradient[0]
				batch.color = svgColor(c.X, c.Y, c.Z)
				bk.color = batch.color
			} else {
				bk.unique = i + 1
			}
		}
		if prior := batchIndexes[unit][bk]; prior != nil {
			prior.polygons = append(prior.polygons, batch.polygons...)
		} else {
			u.batches = append(u.batches, batch)
			batchIndexes[unit][bk] = batch
		}
	}
	for _, u := range units {
		u.depth /= float64(u.count)
		sort.SliceStable(u.batches, func(i, j int) bool {
			a, b := u.batches[i], u.batches[j]
			pa, pb := svgIsDecal(a.triangle), svgIsDecal(b.triangle)
			if pa != pb {
				return !pa
			}
			if pa {
				return a.first < b.first
			}
			return a.depth < b.depth
		})
		for batch, cover := range svgPreparePaintCovers(u.batches) {
			batch.cover = cover
		}
	}
	if err := svgOrderPaintUnits(ctx, units); err != nil {
		return nil, err
	}

	w := &nativeSVGWriter{ctx: ctx}
	w.write(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="%d" height="%d" viewBox="0 0 %d %d" role="img"><title>D2 isometric diagram</title>`, scene.width, scene.height, scene.width, scene.height)
	if scene.backgroundPaint != nil {
		// Stretch the normalized gradient and its opaque backing together
		// when the document wrapper expands or crops the background.
		surface := &nativeVectorSurface{document: nativePaintDocument(scene.backgroundPaint), capBackground: &scene.background}
		fragment, err := nativeSurfaceSVG(ctx, surface, "background")
		if err != nil {
			return nil, err
		}
		w.write(`<svg width="100%%" height="100%%" viewBox="0 0 1 1" preserveAspectRatio="none">%s</svg>`, fragment)
	} else {
		w.write(`<rect width="100%%" height="100%%" fill="%s"/>`, nativeSVGColor(scene.background))
	}
	writeSVGGroundShadow(w, scene)
	id := 0
	seenLinks := make(map[*nativeLink]bool)
	for _, u := range units {
		var closeLink string
		if u.link != nil {
			// Depth ordering can split one shape into several paint groups.
			// All groups respond to clicks, but only the first is a tab stop.
			closeLink = writeSVGLinkStart(w, *u.link, seenLinks[u.link])
			seenLinks[u.link] = true
		}
		if anchor := scene.deckIDs[u.batches[0].triangle.PaintOwner]; anchor != "" {
			w.write(`<g id="%s" opacity="%s">`, anchor, svgNumber(u.opacity))
		} else {
			w.write(`<g opacity="%s">`, svgNumber(u.opacity))
		}
		for _, b := range u.batches {
			id++
			writeSVGBatch(w, b, id, camera)
			if w.err != nil {
				return nil, w.err
			}
		}
		w.write(`</g>%s`, closeLink)
	}
	writeSVGLinks(w, scene)
	w.write(`</svg>`)
	if w.err != nil {
		return nil, w.err
	}
	return w.buf.Bytes(), nil
}

func svgIsDecal(t Triangle) bool {
	return t.NoDepthWrite || t.Material != nil && (t.Material.svgContour || t.Material.Texture != nil || t.Material.Multiply || t.Material.Color.A < 255)
}

func svgTextureAffine(t Triangle, p [3]svgPoint) ([6]float64, error) {
	u, v := t.V[1].U-t.V[0].U, t.V[1].V-t.V[0].V
	x, y := t.V[2].U-t.V[0].U, t.V[2].V-t.V[0].V
	det := float64(u*y) - float64(x*v)
	if math.Abs(det) < 1e-15 {
		return [6]float64{}, fmt.Errorf("native SVG texture has a degenerate UV mapping")
	}
	dx, dy, ex, ey := p[1].x-p[0].x, p[1].y-p[0].y, p[2].x-p[0].x, p[2].y-p[0].y
	a, b, c, d := (float64(dx*y)-float64(ex*v))/det, (float64(dy*y)-float64(ey*v))/det, (float64(u*ex)-float64(x*dx))/det, (float64(u*ey)-float64(x*dy))/det
	return [6]float64{a, b, c, d, p[0].x - float64(a*t.V[0].U) - float64(c*t.V[0].V), p[0].y - float64(b*t.V[0].U) - float64(d*t.V[0].V)}, nil
}

func writeSVGBatch(w *nativeSVGWriter, b *svgPaintBatch, id int, camera rasterCamera) {
	if b.cover != nil {
		writeSVGPaintCover(w, b.cover, camera)
		return
	}
	path := svgFillPath(b.polygons)
	if path == "" {
		return
	}
	m := b.triangle.Material
	alpha := float64(m.Color.A) / 255
	blend := ""
	if m.Multiply {
		blend = ` style="mix-blend-mode:multiply"`
	}
	if b.texture {
		surfaceID := w.surfaceDefinition(m.Vector)
		if w.err != nil {
			return
		}
		w.write(`<defs><clipPath id="face-%d" clipPathUnits="userSpaceOnUse"><path d="%s"/></clipPath></defs>`, id, path)
		filter := ""
		if !m.Unlit {
			slope, intercept := svgTextureLighting(camera, b.triangle)
			w.write(`<defs><filter id="light-%d" color-interpolation-filters="linearRGB"><feComponentTransfer><feFuncR type="linear" slope="%s" intercept="%s"/><feFuncG type="linear" slope="%s" intercept="%s"/><feFuncB type="linear" slope="%s" intercept="%s"/></feComponentTransfer></filter></defs>`, id, svgNumber(slope[0]), svgNumber(intercept[0]), svgNumber(slope[1]), svgNumber(intercept[1]), svgNumber(slope[2]), svgNumber(intercept[2]))
			filter = fmt.Sprintf(` filter="url(#light-%d)"`, id)
		}
		a := b.affine
		w.write(`<g clip-path="url(#face-%d)" opacity="%s"%s><g transform="matrix(%s %s %s %s %s %s)"%s>`, id, svgNumber(alpha), blend, svgNumber(a[0]), svgNumber(a[1]), svgNumber(a[2]), svgNumber(a[3]), svgNumber(a[4]), svgNumber(a[5]), filter)
		if m.outsideCaption {
			// A complete label is one target, including whitespace between
			// glyphs. The surrounding clip limits it to the visible surface.
			w.write(`<rect width="1" height="1" fill="transparent" pointer-events="all"/>`)
		}
		w.write(`<use xlink:href="#%s"/></g></g>`, surfaceID)
		return
	}
	fill := b.color
	if fill == "" {
		fill = writeSVGFaceGradient(w, b, id, camera)
	}
	w.write(`<path d="%s" fill="%s" fill-opacity="%s"%s/>`, path, fill, svgNumber(alpha), blend)
}

func writeSVGLinks(w *nativeSVGWriter, s *nativeScene) {
	type projectedLink struct {
		link   nativeLink
		points []svgPoint
		area   float64
	}
	links := make([]projectedLink, 0, len(s.links))
	for _, link := range s.links {
		if link.meshEnd > 0 {
			continue
		}
		links = append(links, projectedLink{})
		projected := &links[len(links)-1]
		projected.link = link
		for _, v := range link.points {
			p := s.camera.project(v)
			projected.points = append(projected.points, svgProjectedPoint(p.x, p.y, p.z))
		}
		// Hit regions use the same canonical projection and rounded area
		// arithmetic as physical faces. Tiny FMA residuals must not reorder
		// equal-size links, changing which overlapping target wins a click.
		projected.area = math.Abs(svgPolygonArea(projected.points))
	}
	sort.SliceStable(links, func(i, j int) bool { return links[i].area > links[j].area })
	for _, projected := range links {
		link, points := projected.link, projected.points
		closeLink := writeSVGLinkStart(w, link, false)
		w.write(`<path d="%s" fill="transparent" pointer-events="all"/>%s`, svgPolygonPath([][]svgPoint{points}), closeLink)
	}
}

func writeSVGLinkStart(w *nativeSVGWriter, link nativeLink, continued bool) string {
	destination := link.region.URL
	if destination == "" && link.region.Target != "" {
		destination = "#" + link.region.Target
	}
	closeTag := "</g>"
	if destination != "" {
		tabIndex := ""
		if continued {
			tabIndex = ` tabindex="-1"`
		}
		w.write(`<a xlink:href="%s"%s>`, html.EscapeString(destination), tabIndex)
		closeTag = "</a>"
	} else {
		w.write(`<g>`)
	}
	if link.region.Tooltip != "" {
		w.write(`<title>%s</title>`, html.EscapeString(link.region.Tooltip))
	}
	return closeTag
}
