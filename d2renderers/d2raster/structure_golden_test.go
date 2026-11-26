package d2raster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/d2/d2renderers/d2scene"
)

// TestStructureGolden pins Render's result (pixel digest or exact error) over
// a deterministic set of generated scenes that mix valid and malformed nodes,
// primitives, paints, strokes, animations, filters, clips, masks and retained
// vector assets, including the pointer and nil-pointer forms of every scene
// interface value. Regenerate with D2RASTER_UPDATE_STRUCTURE_GOLDEN=1 only for
// an intended behavior change.
func TestStructureGolden(t *testing.T) {
	const cases = 6000
	path := filepath.Join("testdata", "structure_golden.txt")
	var got strings.Builder
	for seed := 0; seed < cases; seed++ {
		fmt.Fprintf(&got, "%d %s\n", seed, structureGoldenResult(int64(seed)))
	}
	if os.Getenv("D2RASTER_UPDATE_STRUCTURE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(got.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() == string(want) {
		return
	}
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(got.String(), "\n")
	mismatches := 0
	for i := range gotLines {
		if i >= len(wantLines) || gotLines[i] != wantLines[i] {
			mismatches++
			if mismatches <= 10 {
				seed := int64(i)
				t.Errorf("seed %d: got %q, want %q (%s)", seed, gotLines[i], wantLines[min(i, len(wantLines)-1)], structureGoldenDetail(seed))
			}
		}
	}
	t.Fatalf("%d of %d generated scenes changed", mismatches, cases)
}

func structureGoldenRender(seed int64) (document *d2scene.Document, err error, pixels []byte) {
	// A panic is recorded as a result too, so that the golden file also pins
	// the (pre-existing) inputs that crash the rasterizer.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	g := &sceneGen{rng: rand.New(rand.NewSource(seed)), strict: seed%2 == 0}
	document = g.document()
	options := testOptions()
	options.Time = time.Duration(g.rng.Intn(3)) * 400 * time.Millisecond
	options.Background = color.NRGBA{R: 10, G: 20, B: 30, A: uint8(g.rng.Intn(2) * 255)}
	if g.bad(8) {
		options.MaxNodes = 1 + g.rng.Intn(6)
	}
	if g.bad(8) {
		options.MaxDepth = 1 + g.rng.Intn(4)
	}
	if g.bad(8) {
		options.MaxImportDepth = 1 + g.rng.Intn(3)
	}
	frame, err := Render(context.Background(), document, options)
	if err != nil {
		return document, err, nil
	}
	return document, nil, frame.Pix
}

func structureGoldenResult(seed int64) string {
	_, err, pixels := structureGoldenRender(seed)
	if err != nil {
		sum := sha256.Sum256([]byte(err.Error()))
		return "E" + hex.EncodeToString(sum[:6])
	}
	sum := sha256.Sum256(pixels)
	return "P" + hex.EncodeToString(sum[:6])
}

func structureGoldenDetail(seed int64) string {
	_, err, _ := structureGoldenRender(seed)
	if err != nil {
		return "error: " + err.Error()
	}
	return "rendered"
}

type sceneGen struct {
	rng    *rand.Rand
	count  int
	strict bool
}

// bad reports, with probability 1/n, whether to emit a malformed value; strict
// generators never do, so that half of the scenes reach the rasterizer.
func (g *sceneGen) bad(n int) bool { return g.chance(n) && !g.strict }

// pickOK picks one of good, or, unless strict, one of good and bad together.
func (g *sceneGen) pickOK(good []float64, bad ...float64) float64 {
	if g.strict {
		return g.pick(good...)
	}
	return g.pick(append(append([]float64(nil), good...), bad...)...)
}

func (g *sceneGen) chance(n int) bool { return g.rng.Intn(n) == 0 }

func (g *sceneGen) pick(values ...float64) float64 { return values[g.rng.Intn(len(values))] }

func (g *sceneGen) document() *d2scene.Document {
	document := d2scene.NewDocument(d2scene.Box{Width: 24, Height: 18}, nil)
	document.Root = g.node(0, true)
	if g.strict || g.chance(2) {
		document.Assets["v1"] = d2scene.VectorAsset{ViewBox: d2scene.Box{Width: 10, Height: 10}, Root: g.node(1, false)}
	}
	if g.chance(3) {
		root := d2scene.NewNode(g.imagePrimitive())
		if g.chance(2) {
			root.Children = []*d2scene.Node{g.node(2, false)}
		}
		var asset d2scene.Asset = d2scene.VectorAsset{ViewBox: d2scene.Box{Width: 8, Height: 8}, Root: root}
		if g.chance(4) {
			asset = &d2scene.VectorAsset{ViewBox: d2scene.Box{Width: 8, Height: 8}, Root: root}
		}
		document.Assets["v2"] = asset
	}
	if g.bad(10) {
		document.Assets["v3"] = d2scene.VectorAsset{ViewBox: d2scene.Box{Width: 8, Height: 8}, Root: d2scene.NewNode(d2scene.Image{Asset: "v3", Box: d2scene.Box{Width: 4, Height: 4}})}
	}
	if g.bad(12) {
		document.Assets["v4"] = d2scene.VectorAsset{ViewBox: d2scene.Box{Width: 8, Height: 8}}
	}
	return document
}

func (g *sceneGen) node(depth int, allowImage bool) *d2scene.Node {
	g.count++
	node := d2scene.NewNode(nil)
	node.ID = fmt.Sprintf("n%d", g.count)
	switch g.rng.Intn(10) {
	case 0:
		node.Transform = d2scene.Translate(g.pick(1, -3, 2.5), g.pick(0, 4))
	case 1:
		node.Transform = d2scene.Scale(g.pickOK([]float64{0.5, 2, -1}, 0), g.pick(1, 0.75))
	case 2:
		if g.bad(6) {
			node.Transform = d2scene.Matrix{A: math.NaN(), D: 1}
		} else {
			node.Transform = d2scene.Matrix{A: 1, B: 0.3, C: -0.2, D: 1, E: 1, F: 2}
		}
	}
	if g.chance(5) {
		node.Opacity = g.pickOK([]float64{0.5, 0, 0.25}, 1.5, math.NaN())
	}
	if g.chance(6) {
		node.Blend = d2scene.BlendMode(g.pickOK([]float64{1, 2, 3, 4}, 6, 9, 30))
	}
	node.Primitive = g.primitive(allowImage)
	if g.chance(4) {
		node.Animations = g.tracks(node.Primitive != nil)
	}
	if g.chance(7) {
		node.Filters = g.filters()
	}
	if g.chance(8) {
		clip := &d2scene.Clip{Path: g.path(), Transform: d2scene.Identity()}
		if g.bad(8) {
			clip.Transform = d2scene.Matrix{A: math.Inf(1), D: 1}
		}
		if g.bad(6) {
			clip.Path.FillRule = 7
		}
		node.Clip = clip
	}
	if g.chance(9) {
		mask := &d2scene.Mask{Type: d2scene.MaskType(g.pickOK([]float64{0, 1}, 2)), Transform: d2scene.Identity()}
		if !g.bad(6) {
			mask.Root = g.node(depth+1, allowImage)
		}
		node.Mask = mask
	}
	if depth < 3 {
		children := g.rng.Intn(4 - depth)
		for i := 0; i < children; i++ {
			if g.bad(30) {
				node.Children = append(node.Children, nil)
				continue
			}
			node.Children = append(node.Children, g.node(depth+1, allowImage))
		}
	}
	if g.bad(40) && len(node.Children) > 0 {
		node.Children = append(node.Children, node)
	}
	return node
}

func (g *sceneGen) box() d2scene.Box {
	box := d2scene.Box{X: g.pick(1, 3, -2), Y: g.pick(1, 2), Width: g.pick(8, 12, 5), Height: g.pick(6, 9)}
	if g.bad(15) {
		box.Width = g.pick(-1, math.NaN())
	}
	return box
}

func (g *sceneGen) path() d2scene.Path {
	path := d2scene.Path{Commands: []d2scene.PathCommand{
		{Kind: d2scene.MoveCommand, P1: d2scene.Point{X: 2, Y: 2}},
		{Kind: d2scene.LineCommand, P1: d2scene.Point{X: g.pick(14, 9), Y: 3}},
		{Kind: d2scene.CubicCommand, P1: d2scene.Point{X: 16, Y: 8}, P2: d2scene.Point{X: 10, Y: 14}, P3: d2scene.Point{X: 4, Y: g.pick(12, 15)}},
	}}
	if g.chance(2) {
		path.Commands = append(path.Commands, d2scene.PathCommand{Kind: d2scene.CloseCommand})
	}
	if g.chance(3) {
		path.Commands = append(path.Commands,
			d2scene.PathCommand{Kind: d2scene.MoveCommand, P1: d2scene.Point{X: 5, Y: 5}},
			d2scene.PathCommand{Kind: d2scene.ArcCommand, P1: d2scene.Point{X: 9, Y: 9}, RadiusX: 3, RadiusY: 2, Sweep: true},
			d2scene.PathCommand{Kind: d2scene.QuadraticCommand, P1: d2scene.Point{X: 12, Y: 4}, P2: d2scene.Point{X: 6, Y: 6}},
		)
	}
	if g.bad(15) {
		path.Commands = append(path.Commands, d2scene.PathCommand{Kind: d2scene.LineCommand, P1: d2scene.Point{X: math.Inf(-1)}})
	}
	if g.bad(15) {
		path.Commands = append([]d2scene.PathCommand{{Kind: d2scene.LineCommand, P1: d2scene.Point{X: 1, Y: 1}}}, path.Commands...)
	}
	path.FillRule = d2scene.FillRule(g.rng.Intn(2))
	if g.bad(20) {
		path.FillRule = 3
	}
	return path
}

func (g *sceneGen) imagePrimitive() d2scene.Primitive {
	image := d2scene.Image{Asset: d2scene.AssetID([]string{"v1", "v2", "v3", "v4", "missing"}[g.rng.Intn(5)]), Box: g.box()}
	if g.strict {
		image.Asset = "v1"
	}
	image.Aspect.Align = d2scene.AspectAlign(g.rng.Intn(3))
	image.Aspect.Fit = d2scene.AspectFit(g.rng.Intn(2))
	switch g.rng.Intn(4) {
	case 0:
		return &image
	case 1:
		if g.chance(3) {
			return (*d2scene.Image)(nil)
		}
	}
	return image
}

func (g *sceneGen) primitive(allowImage bool) d2scene.Primitive {
	pointer := g.chance(3)
	nilPointer := g.chance(12)
	kind := g.rng.Intn(8)
	if g.strict && kind == 6 {
		kind = 1
	}
	switch kind {
	case 0:
		return nil
	case 1, 2:
		rect := d2scene.Rect{Box: g.box(), Fill: g.paint(), Stroke: g.stroke()}
		if g.chance(3) {
			rect.RadiusX, rect.RadiusY = g.pickOK([]float64{1, 2}, -1, math.NaN()), g.pick(1, 3)
		}
		if nilPointer {
			return (*d2scene.Rect)(nil)
		}
		if pointer {
			return &rect
		}
		return rect
	case 3:
		ellipse := d2scene.Ellipse{Center: d2scene.Point{X: 10, Y: g.pick(8, 6)}, RadiusX: g.pickOK([]float64{5, 3}, -1, math.NaN()), RadiusY: g.pick(4, 6), Fill: g.paint(), Stroke: g.stroke()}
		if nilPointer {
			return (*d2scene.Ellipse)(nil)
		}
		if pointer {
			return &ellipse
		}
		return ellipse
	case 4, 5:
		path := g.path()
		path.Fill, path.Stroke = g.paint(), g.stroke()
		if nilPointer {
			return (*d2scene.Path)(nil)
		}
		if pointer {
			return &path
		}
		return path
	case 6:
		text := d2scene.TextRun{Origin: d2scene.Point{X: 2, Y: 10}, Text: "x"}
		text.Font.Size = g.pickOK([]float64{12}, 0, math.NaN(), -3)
		if nilPointer {
			return (*d2scene.TextRun)(nil)
		}
		if pointer {
			return &text
		}
		return text
	default:
		if !allowImage && (g.strict || !g.chance(3)) {
			return d2scene.Rect{Box: g.box(), Fill: d2scene.SolidPaint{Color: color.NRGBA{R: 200, A: 255}}}
		}
		return g.imagePrimitive()
	}
}

func (g *sceneGen) stops() []d2scene.GradientStop {
	stops := []d2scene.GradientStop{{Offset: 0, Color: color.NRGBA{R: 255, A: 255}}, {Offset: 1, Color: color.NRGBA{B: 255, G: 80, A: 200}}}
	if g.bad(10) {
		stops[1].Offset = math.NaN()
	}
	if g.chance(10) {
		stops = stops[:g.rng.Intn(2)]
	}
	return stops
}

func (g *sceneGen) paint() d2scene.Paint {
	pointer := g.chance(3)
	nilPointer := g.chance(12)
	units := d2scene.PaintUnits(g.rng.Intn(2))
	if g.bad(25) {
		units = 9
	}
	transform := d2scene.Identity()
	if g.bad(12) {
		transform = d2scene.Scale(0, 1)
	}
	switch g.rng.Intn(7) {
	case 0:
		return nil
	case 1, 2:
		paint := d2scene.SolidPaint{Color: color.NRGBA{R: uint8(g.rng.Intn(256)), G: 128, B: 40, A: uint8(128 + g.rng.Intn(128))}}
		if nilPointer {
			return (*d2scene.SolidPaint)(nil)
		}
		if pointer {
			return &paint
		}
		return paint
	case 3:
		paint := d2scene.LinearGradient{Start: d2scene.Point{X: 0, Y: 0}, End: d2scene.Point{X: g.pick(1, 0), Y: 1}, Stops: g.stops(), Units: units, Transform: transform, Spread: d2scene.SpreadMethod(g.rng.Intn(3))}
		if units == d2scene.UserSpaceOnUse {
			paint.End = d2scene.Point{X: 20, Y: 10}
		}
		if nilPointer {
			return (*d2scene.LinearGradient)(nil)
		}
		if pointer {
			return &paint
		}
		return paint
	case 4:
		paint := d2scene.RadialGradient{Center: d2scene.Point{X: 0.5, Y: 0.5}, Radius: g.pickOK([]float64{0.5, 0}, -1), Focal: d2scene.Point{X: 0.4, Y: 0.5}, Stops: g.stops(), Units: units, Transform: transform, Spread: d2scene.SpreadMethod(g.pickOK([]float64{0, 1, 2}, 3))}
		if nilPointer {
			return (*d2scene.RadialGradient)(nil)
		}
		if pointer {
			return &paint
		}
		return paint
	case 5:
		root := d2scene.NewNode(d2scene.Rect{Box: d2scene.Box{Width: 2, Height: 2}, Fill: d2scene.SolidPaint{Color: color.NRGBA{G: 255, A: 255}}})
		if g.bad(4) {
			root.Primitive = g.imagePrimitive()
		}
		paint := d2scene.PatternPaint{Tile: d2scene.Box{Width: g.pickOK([]float64{4, 0.25}, -1), Height: 4}, Root: root, Units: units, Transform: transform}
		if units == d2scene.ObjectBoundingBox {
			paint.Tile = d2scene.Box{Width: 0.3, Height: g.pick(0.4, 0)}
		}
		if g.bad(10) {
			paint.Root = nil
		}
		if nilPointer {
			return (*d2scene.PatternPaint)(nil)
		}
		if pointer {
			return &paint
		}
		return paint
	default:
		return nil
	}
}

func (g *sceneGen) stroke() *d2scene.Stroke {
	if g.chance(2) {
		return nil
	}
	stroke := &d2scene.Stroke{Paint: g.paint(), Width: g.pickOK([]float64{1, 2.5, 0}, -1, math.NaN()), Cap: d2scene.LineCap(g.rng.Intn(3)), Join: d2scene.LineJoin(g.rng.Intn(3)), MiterLimit: g.pickOK([]float64{4, 1}, 0)}
	if g.chance(3) {
		stroke.Dashes = []float64{g.pickOK([]float64{2, 3}, 0, -1), 1}
		stroke.DashOffset = g.pick(0, 1.5)
	}
	if g.bad(20) {
		stroke.Cap = 9
	}
	return stroke
}

func (g *sceneGen) tracks(hasPrimitive bool) []d2scene.Track {
	var tracks []d2scene.Track
	used := map[d2scene.AnimationProperty]bool{}
	for i := g.rng.Intn(3) + 1; i > 0; i-- {
		property := d2scene.AnimationProperty(g.pickOK([]float64{0, 1, 2, 3, 4, 0}, 5, 6))
		if g.strict && (!hasPrimitive || g.chance(2)) && property >= d2scene.AnimateStrokeDashOffset {
			property = d2scene.AnimateTransform
		}
		if g.strict && used[property] {
			continue
		}
		used[property] = true
		var from, to d2scene.AnimationValue
		switch property {
		case d2scene.AnimateOpacity, d2scene.AnimateStrokeDashOffset:
			from, to = d2scene.NumberValue(g.pick(0, 0.2)), d2scene.NumberValue(g.pick(1, 2, 0.6))
		case d2scene.AnimateTransform:
			from, to = d2scene.TransformValue(d2scene.Identity()), d2scene.TransformValue(d2scene.Translate(4, g.pick(2, 0)))
		case d2scene.AnimateFillColor, d2scene.AnimateStrokeColor:
			from, to = d2scene.ColorValue(color.NRGBA{R: 255, A: 255}), d2scene.ColorValue(color.NRGBA{G: 255, A: 128})
		default:
			from = d2scene.ShadowValue(d2scene.DropShadow{OffsetX: 1, OffsetY: 1, SigmaX: 1, SigmaY: 1, Color: color.NRGBA{A: 255}})
			to = d2scene.ShadowValue(d2scene.DropShadow{OffsetX: 2, OffsetY: 3, SigmaX: 0.5, SigmaY: 1, Color: color.NRGBA{B: 255, A: 200}})
		}
		if g.bad(12) {
			to = d2scene.NumberValue(1)
		}
		track := d2scene.Track{Property: property, Duration: time.Second, Repeat: g.chance(2), Keyframes: []d2scene.Keyframe{{Offset: 0, Value: from}, {Offset: 1, Value: to}}}
		if g.bad(6) {
			track.TargetIndex = g.rng.Intn(3)
		}
		if g.chance(15) {
			track.Keyframes[1].Offset = 0.5
		}
		tracks = append(tracks, track)
		if g.bad(12) {
			tracks = append(tracks, track)
		}
	}
	return tracks
}

func (g *sceneGen) filters() []d2scene.Filter {
	var filters []d2scene.Filter
	for i := g.rng.Intn(2) + 1; i > 0; i-- {
		switch g.rng.Intn(5) {
		case 0:
			filters = append(filters, d2scene.GaussianBlur{SigmaX: g.pickOK([]float64{1, 0}, -1), SigmaY: 1, LinearRGB: g.chance(2)})
		case 1:
			filters = append(filters, &d2scene.GaussianBlur{SigmaX: 0.5, SigmaY: 2})
		case 2:
			filters = append(filters, d2scene.DropShadow{OffsetX: 1, OffsetY: 2, SigmaX: 1, SigmaY: 1, Color: color.NRGBA{A: 180}})
		case 3:
			if g.strict {
				continue
			}
			filters = append(filters, (*d2scene.DropShadow)(nil))
		default:
			filters = append(filters, &d2scene.DropShadow{OffsetX: -1, SigmaX: 0, SigmaY: 0, Color: color.NRGBA{R: 255, A: 255}})
		}
	}
	return filters
}
