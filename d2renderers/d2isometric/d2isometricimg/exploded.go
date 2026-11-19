package d2isometricimg

import (
	"context"
	"fmt"
	"image"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/d2lang/d2/d2renderers/d2isometric"
	"github.com/d2lang/d2/d2target"
)

type explodedDeck struct {
	path                              []string
	board                             *d2target.Diagram
	scene                             *d2isometric.Scene
	mesh                              *nativeScene
	offset                            Vec
	title                             labelSurface
	wires                             []stackWire
	width, depth, surfaceY, thickness float64
	height                            float64 // tallest artwork above the plate
}

func explodedLayers(ctx context.Context, root *d2target.Diagram, boardPath []string) ([]explodedDeck, error) {
	var decks []explodedDeck
	seen := map[*d2target.Diagram]bool{}
	var walk func(*d2target.Diagram, []string) error
	walk = func(board *d2target.Diagram, path []string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if board == nil || seen[board] {
			return fmt.Errorf("isometric exploded layers contain a nil or repeated board")
		}
		seen[board] = true
		if len(board.Name) > d2isometric.MaxIDBytes || !utf8.ValidString(board.Name) {
			return fmt.Errorf("isometric exploded board name exceeds the identifier limit or contains invalid UTF-8")
		}
		if len(seen) > MaxTimelineBoards || len(path) > d2isometric.MaxIDDepth {
			return fmt.Errorf("isometric exploded layers exceed board or depth limit")
		}
		if gap := board.IsometricLayerGap; gap != nil && (*gap < 0 || *gap > d2target.MaxIsometricLayerGap) {
			return fmt.Errorf("isometric layer gap must be between 0 and %d", d2target.MaxIsometricLayerGap)
		}
		if !board.IsFolderOnly {
			decks = append(decks, explodedDeck{board: board, path: path})
		}
		for _, child := range board.Layers {
			if child == nil {
				return fmt.Errorf("isometric exploded layers contain a nil board")
			}
			childPath := append(path[:len(path):len(path)], "layers", child.Name)
			if err := walk(child, childPath); err != nil {
				return err
			}
		}
		return nil
	}
	if len(boardPath) >= d2isometric.MaxIDDepth {
		return nil, fmt.Errorf("isometric exploded board path exceeds the depth limit")
	}
	for _, part := range boardPath {
		if len(part) > d2isometric.MaxIDBytes || !utf8.ValidString(part) {
			return nil, fmt.Errorf("isometric exploded board path contains an invalid identifier")
		}
	}
	if err := walk(root, append([]string{"root"}, boardPath...)); err != nil {
		return nil, err
	}
	if len(decks) == 0 {
		return nil, fmt.Errorf("isometric exploded view has no drawable layers")
	}
	boards := make([]*d2target.Diagram, len(decks))
	for i := range decks {
		boards[i] = decks[i].board
	}
	return decks, d2isometric.ValidateSceneBudget(ctx, boards)
}

func preflightExplodedLinks(ctx context.Context, decks []explodedDeck, options nativeSceneOptions) error {
	if options.links == nil {
		return nil
	}
	room := *options.links
	index := indexStackFloors(decks)
	for i, deck := range decks {
		var err error
		if options.vector {
			err = preflightSVGMetadata(ctx, deck.board, &room)
		} else {
			err = preflightPageMetadata(ctx, deck.board, &room, func(link string) bool {
				_, ok := index.destination(link, i)
				return ok
			})
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func buildExplodedScene(ctx context.Context, diagram *d2target.Diagram, o Options, options nativeSceneOptions) (*nativeScene, error) {
	decks, err := explodedLayers(ctx, diagram, o.BoardPath)
	if err != nil {
		return nil, err
	}
	if err := preflightExplodedLinks(ctx, decks, options); err != nil {
		return nil, err
	}
	settings := o.Render
	if diagram.Config != nil {
		if settings.ThemeID == nil {
			settings.ThemeID = diagram.Config.ThemeID
		}
		if settings.ThemeOverrides == nil {
			settings.ThemeOverrides = diagram.Config.ThemeOverrides
		}
	}

	width, depth := 3.0, 1.0
	triangles, pixels := 0, int64(0)
	textures := make(map[image.Image]bool)
	for i := range decks {
		deck := &decks[i]
		floorSettings := o.Render
		if deck.board.Config == nil || deck.board.Config.ThemeID == nil {
			floorSettings.ThemeID = settings.ThemeID
		}
		if deck.board.Config == nil || deck.board.Config.ThemeOverrides == nil {
			floorSettings.ThemeOverrides = settings.ThemeOverrides
		}
		deck.scene, err = d2isometric.BuildScene(deck.board, &floorSettings)
		if err != nil {
			return nil, err
		}
		deck.wires = make([]stackWire, len(deck.scene.Edges))
		perFloor := options
		perFloor.stackWires = deck.wires
		perFloor.deferRaster = true
		perFloor.camera = nil
		perFloor.outputDensity = sceneOutputDensity(deck.scene, o.Width, max(64, o.Height/len(decks)), nil)
		deck.mesh, err = newNativeSceneWithOptions(ctx, deck.scene, o.Width, o.Height, o.Assets, o.Fonts, perFloor)
		if err != nil {
			return nil, err
		}
		triangles += len(deck.mesh.triangles)
		if triangles > maxNativeTriangles {
			return nil, fmt.Errorf("isometric exploded geometry exceeds %d triangles", maxNativeTriangles)
		}
		bounds := nativeMeshBounds(deck.mesh.triangles)
		floor, ceiling := 0., 0.
		for j, t := range deck.mesh.triangles {
			if j%1024 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			for _, v := range t.V {
				floor = min(floor, v.Position.Y)
				ceiling = max(ceiling, v.Position.Y)
			}
			if t.Material != nil && t.Material.Texture != nil && !textures[t.Material.Texture] {
				texture := t.Material.Texture
				textures[texture] = true
				pixels += int64(texture.Bounds().Dx()) * int64(texture.Bounds().Dy())
			}
		}
		// Individual source surfaces have their own bounded raster caches.
		// The stack retains every floor until the common visibility pass.
		if pixels > 64<<20 {
			return nil, fmt.Errorf("isometric exploded surfaces exceed %d retained texture pixels", 64<<20)
		}
		deck.offset = nv(-bounds.X-bounds.Width/2, -floor, -bounds.Y-bounds.Height/2)
		deck.height = ceiling - floor
		width, depth = max(width, bounds.Width), max(depth, bounds.Height)
	}
	margin := max(.5, min(.72, width*.045))
	width += 2 * margin
	depth += 2 * margin
	thickness := max(.055, min(.18, width*.012))
	placeExplodedDecks(decks, width, depth, thickness)

	b := &meshBuilder{ctx: ctx, scale: d2isometric.SceneScale, options: options}
	anchors := make(map[*nativePaintOwner]string, len(decks))
	targets := make(map[string]string, len(decks))
	var packets []packetRoute
	for i := range decks {
		d := &decks[i]
		anchor := "deck-" + strconv.Itoa(i)
		targets[stackPathKey(d.path)] = anchor
		fill := explodedDeckFill(d.scene)
		first := len(b.triangles)
		b.explodedSlab(d, fill)
		if b.err != nil {
			return nil, b.err
		}
		b.markPaintOwner(first)
		anchors[b.triangles[first].PaintOwner] = anchor
		for j := first; j < len(b.triangles); j++ {
			// Register the casting floor locally so raising a deck doesn't
			// stretch a dark footprint across all the other levels.
			b.triangles[j].ShadowGround = &d.surfaceY
		}
		d.title = d.mesh.rootTitle
		d.title.center = nadd(d.title.center, d.offset)
		if err := b.appendDeck(d, &packets); err != nil {
			return nil, err
		}
		d.mesh = nil
	}
	if err := b.addStackConnectors(decks); err != nil {
		return nil, err
	}
	if options.vector {
		for i := range b.links {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			r := &b.links[i].region
			if r.Target != "" {
				r.Target = targets[stackLinkKey(r.Target)]
			}
		}
	}
	// Use the selected board's canvas on the combined document. Individual
	// floors retain their own shape, gradient and theme materials.
	canvas, err := d2isometric.BuildScene(&d2target.Diagram{Root: diagram.Root, Config: diagram.Config}, &settings)
	if err != nil {
		return nil, err
	}
	if canvas.ThemeID != 0 || canvas.ThemeOverrides != nil {
		b.palettePaper = canvas.Background
	}
	result, err := finishNativeScene(b, canvas, o.Width, o.Height, packets)
	if err != nil {
		return nil, err
	}
	result.deckIDs = anchors
	return result, nil
}

// The native default is warm stock. Authored fills (including literal theme
// tokens other than the default canvas token) remain the material of the slab.
func explodedDeckFill(scene *d2isometric.Scene) string {
	if scene.Root.Fill != "" && scene.Root.Fill != d2target.BG_COLOR {
		return scene.Background
	}
	if scene.ThemeID == 0 && scene.ThemeOverrides == nil {
		return "#e9dfc7"
	}
	// Tint the default stock toward the palette paper. Maintain a visible
	// boundary against both a pale canvas and a dark theme's canvas.
	c := nativePaint(scene.Background, "#f5f7fa")
	weight := .25
	if readableSurfaceInk(scene.Background, 1) == "#f5f8fc" {
		weight = .16
	}
	mix := func(v uint8, neutral float64) uint8 {
		return uint8(math.Round(float64(v)*(1-weight) + neutral*weight))
	}
	return fmt.Sprintf("#%02x%02x%02x", mix(c.R, 157), mix(c.G, 142), mix(c.B, 110))
}

func (b *meshBuilder) explodedSlab(d *explodedDeck, fill string) {
	b.box(nv(0, d.surfaceY-d.thickness/2, 0), nv(d.width, d.thickness, d.depth), nativeMaterial(fill, .85, 0, 1), 0)
	// Ink the cut perimeter on the top face. The narrow ring gives the rear
	// edges the same precise silhouette even where they face away from light.
	ink := nativeMaterial(readableSurfaceInk(fill, 1), 1, 0, .45)
	ink.Unlit = true
	y := d.surfaceY + .001
	corners := func(inset float64) [4]Vec {
		x, z := d.width/2-inset, d.depth/2-inset
		return [4]Vec{nv(-x, y, -z), nv(x, y, -z), nv(x, y, z), nv(-x, y, z)}
	}
	outer := corners(0)
	inner := corners(max(.016, min(.03, d.width*.002)))
	for i, a := range outer {
		j := (i + 1) % len(outer)
		b.flat(outer[j], a, inner[i], ink, false)
		b.flat(outer[j], inner[i], inner[j], ink, false)
	}
}

func placeExplodedDecks(decks []explodedDeck, width, depth, thickness float64) {
	up := nativeCameraAxes().up
	// Convert the complete projected footprint into vertical world clearance.
	// An author's per-pair gap is additional clear air between silhouettes.
	projected := (math.Abs(up.X)*width + math.Abs(up.Z)*depth) / up.Y
	for i := len(decks) - 1; i >= 0; i-- {
		d := &decks[i]
		d.width, d.depth, d.thickness = width, depth, thickness
		if i+1 < len(decks) {
			air := max(.45, width*.085)
			if d.board.IsometricLayerGap != nil {
				air = float64(*d.board.IsometricLayerGap) * d2isometric.SceneScale
			}
			lower := &decks[i+1]
			d.surfaceY = lower.surfaceY + lower.height + thickness + projected + air
		}
		d.offset.Y += d.surfaceY + .007
	}
}

func (b *meshBuilder) appendDeck(d *explodedDeck, packets *[]packetRoute) error {
	mesh := d.mesh
	start := len(b.triangles)
	if len(mesh.triangles) > maxNativeTriangles-start {
		return fmt.Errorf("isometric exploded geometry exceeds %d triangles", maxNativeTriangles)
	}
	for i, t := range mesh.triangles {
		if i%1024 == 0 {
			if err := b.ctx.Err(); err != nil {
				return err
			}
		}
		for j := range t.V {
			t.V[j].Position = nadd(t.V[j].Position, d.offset)
		}
		if t.ShadowGround != nil {
			ground := *t.ShadowGround + d.offset.Y
			t.ShadowGround = &ground
		} else {
			t.ShadowGround = &d.surfaceY
		}
		b.triangles = append(b.triangles, t)
	}
	for _, a := range mesh.animatedNodes {
		a.first += start
		a.last += start
		b.animatedNodes = append(b.animatedNodes, a)
	}
	for _, p := range mesh.panels {
		p.first += start
		p.last += start
		b.panels = append(b.panels, p)
	}
	for _, p := range mesh.packets {
		for j := range p.points {
			p.points[j] = nadd(p.points[j], d.offset)
		}
		*packets = append(*packets, p)
	}
	for _, link := range mesh.links {
		if link.meshEnd > 0 {
			link.meshStart += start
			link.meshEnd += start
		} else {
			for j := range link.points {
				link.points[j] = nadd(link.points[j], d.offset)
			}
		}
		b.addNativeLink(link)
	}
	return b.err
}
