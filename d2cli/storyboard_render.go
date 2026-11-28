package d2cli

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

const (
	storyboardMaxFrames = 64
	storyboardWidth     = 416
	storyboardHeight    = 248
	storyboardCaption   = 32
	storyboardGap       = 8
)

type storyboardFrameInfo struct {
	Board  string `json:"board"`
	TimeMS int64  `json:"time_ms"`
}

type storyboardSample struct {
	storyboardFrameInfo
	board      *d2target.Diagram // Authored board, used to find its viewport.
	diagram    *d2target.Diagram // Sampled artwork, including the departing board during a zoom.
	arrival    *d2target.Diagram // Arriving artwork at the start of its local clock.
	group      *d2target.Diagram // Boards registered in the same world coordinate system.
	filmBounds image.Rectangle   // Includes unsampled tween geometry in this group.
	leg        d2animate.LayerLeg
	progress   float64
}

// planStoryboard seeks D2's native animation schedule. Every authored board
// gets a sample during its endpoint hold. Remaining cells can show the middle
// of transitions, or a second phase of an independently animated board.
func planStoryboard(ctx context.Context, root *d2target.Diagram, intervalMS int) ([]storyboardSample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if intervalMS <= 0 {
		return nil, fmt.Errorf("storyboard requires a positive animation interval")
	}
	icons, err := d2animate.NewIconTimeline(root, intervalMS)
	if err != nil {
		return nil, err
	}
	var boards []*d2target.Diagram
	names := make(map[*d2target.Diagram]string)
	groups := make(map[*d2target.Diagram]*d2target.Diagram)
	var visit func(*d2target.Diagram, []string, *d2target.Diagram) error
	visit = func(b *d2target.Diagram, path []string, layer *d2target.Diagram) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !b.IsFolderOnly {
			if len(boards) == storyboardMaxFrames {
				return fmt.Errorf("storyboard supports up to %d renderable boards", storyboardMaxFrames)
			}
			name := "root"
			if len(path) > 0 {
				name = d2format.Format(d2ast.MakeKeyPath(path))
			} else if b.Name != "" {
				name = d2format.Format(d2ast.MakeKeyPath([]string{b.Name}))
			}
			groups[b] = layer
			boards = append(boards, b)
			names[b] = name
		}
		for _, group := range []struct {
			kind     string
			children []*d2target.Diagram
		}{{"layers", b.Layers}, {"scenarios", b.Scenarios}, {"steps", b.Steps}} {
			for _, child := range group.children {
				childLayer := layer
				if group.kind == "layers" {
					childLayer = child
				}
				if err := visit(child, append(path, group.kind, child.Name), childLayer); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(root, nil, root); err != nil {
		return nil, err
	}
	if len(boards) == 0 {
		return nil, fmt.Errorf("storyboard requires at least one renderable board")
	}
	zoomMS, err := d2animate.LayerDuration(root)
	if err != nil {
		return nil, err
	}
	nav := d2animate.NewLayerNavigation(root, nil)
	routes := make([][]d2animate.LayerLeg, len(boards))
	// A layer portal gives each layer its own centered coordinate system.
	// Within a layer, retain world positions from every step and tween.
	bounds := make(map[*d2target.Diagram]image.Rectangle)
	if nav.HasLayerPortal() {
		for i, board := range boards {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			next := boards[(i+1)%len(boards)]
			if zoomMS > 0 && len(next.TweenFrames) == 0 {
				routes[i] = nav.Route(board, next)
			}
			group := groups[board]
			include := func(d *d2target.Diagram) {
				tl, br := d.BoundingBox()
				bounds[group] = bounds[group].Union(image.Rect(tl.X, tl.Y, br.X, br.Y))
			}
			include(board)
			for _, tween := range board.TweenFrames {
				include(tween)
			}
		}
	} else {
		tl, br := root.NestedBoundingBox()
		bounds[nil] = image.Rect(tl.X, tl.Y, br.X, br.Y)
		clear(groups)
	}
	var startMS int64
	var samples []storyboardSample
	extra := storyboardMaxFrames - len(boards)
	var frames []d2animate.Frame
	for i, b := range boards {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		frames = d2animate.AppendBoardFrames(frames[:0], b, intervalMS)
		endpoint := frames[len(frames)-1]
		holdMS := endpoint.DurationMS
		route := routes[i]
		if len(route) > 0 {
			endpoint.DurationMS = max(holdMS, zoomMS)
			holdMS = endpoint.DurationMS - zoomMS
		}
		duration := endpoint.ElapsedMS + endpoint.DurationMS
		if int64(duration) > math.MaxInt64/int64(time.Millisecond)-startMS {
			return nil, fmt.Errorf("storyboard duration exceeds the supported time range")
		}
		add := func(diagram *d2target.Diagram, at int) *storyboardSample {
			samples = append(samples, storyboardSample{
				storyboardFrameInfo: storyboardFrameInfo{Board: names[b], TimeMS: startMS + int64(at)},
				board:               b, diagram: diagram, group: groups[b], filmBounds: bounds[groups[b]],
			})
			return &samples[len(samples)-1]
		}
		hold := endpoint.ElapsedMS + holdMS/2
		if extra > 0 && len(frames) > 1 {
			at := min(b.TweenDurationMS/2, endpoint.ElapsedMS-1)
			for _, frame := range frames {
				if at < frame.ElapsedMS+frame.DurationMS {
					add(icons.Sample(b, frame.Diagram, at), at)
					extra--
					break
				}
			}
		} else if extra > 0 && hold > 0 {
			if period := d2scenebuild.ShortestAnimationPeriod(b); period > 0 {
				first, second := storyboardEffectSamples(startMS, hold, period)
				add(icons.Sample(b, b, first), first)
				hold = second
				extra--
			}
		}
		add(icons.Sample(b, b, hold), hold)
		if extra > 0 && len(route) > 0 && zoomMS > 1 {
			mid := zoomMS / 2
			t := float64(mid) * float64(len(route)) / float64(zoomMS)
			n := min(int(t), len(route)-1)
			leg := route[n]
			at := duration - zoomMS + mid
			departureMS := 0
			if n == 0 {
				departureMS = at
			}
			// On later legs the camera passes through an intermediate board
			// at its resting pose, as it does when rendering the GIF.
			sample := add(icons.Sample(leg.From, leg.From, departureMS), at)
			sample.arrival = icons.Sample(leg.To, leg.To, 0)
			sample.leg, sample.progress = leg, t-float64(n)
			// The middle of a sibling transition can show the common parent.
			// Identify the visible artwork with an exact D2 board path.
			parent, child := leg.Zoom.Poses(sample.filmBounds, sample.progress)
			departure, arrival := parent.Opacity, child.Opacity
			if !leg.Zoom.Inward {
				departure, arrival = arrival, departure
			}
			sample.Board = names[leg.From]
			if arrival > departure {
				sample.Board = names[leg.To]
			}
			extra--
		}
		startMS += int64(duration)
	}
	return samples, nil
}

// A visible dash pattern or pulsing shape repeats within one period. Keep
// both samples on a single half-cycle: symmetric points on either side of
// a pulse extremum can look identical even with two different timestamps.
func storyboardEffectSamples(startMS int64, holdMS int, period time.Duration) (int, int) {
	phase := time.Duration(startMS) * time.Millisecond % period
	turn := period - phase
	if phase < period/2 {
		turn = period/2 - phase
	}
	var start time.Duration
	end := min(time.Duration(holdMS)*time.Millisecond, period/2)
	if turn < end {
		// Use the longer visible portion before or after the turning point.
		if turn >= end-turn {
			end = turn
		} else {
			start = turn
		}
	}
	last := max(1, int(end.Round(time.Millisecond).Milliseconds()))
	return min(last-1, int(start.Round(time.Millisecond).Milliseconds())), last
}

func storyboardSceneCount(plan []storyboardSample) int {
	count := len(plan)
	for _, sample := range plan {
		if sample.leg.From != nil {
			count++
		}
	}
	return count
}

type storyboardTile struct {
	pixels image.Image
	bounds image.Rectangle // world bounds within the shared playback viewport
}

func renderStoryboard(ctx context.Context, plan []storyboardSample, opts d2svg.RenderOpts, forceAppendix *bool, assets *d2scenebuild.AssetOptions, fonts *d2scenebuild.FontFallbackOptions) ([]byte, []storyboardFrameInfo, error) {
	if len(plan) == 0 || len(plan) > storyboardMaxFrames {
		return nil, nil, fmt.Errorf("invalid storyboard frame count %d", len(plan))
	}
	session, err := newGIFRenderSession()
	if err != nil {
		return nil, nil, err
	}
	paintCount := storyboardSceneCount(plan)
	links, err := gifLinkBudget(paintCount)
	if err != nil {
		return nil, nil, err
	}
	opts.Scale = new(1.0) // Rasterize directly at thumbnail resolution.
	tiles := make([]storyboardTile, len(plan))
	pad := d2svg.DEFAULT_PADDING
	if opts.Pad != nil {
		pad = int(*opts.Pad)
	}
	groups := make(map[*d2target.Diagram]image.Rectangle)
	boardGroups := make(map[*d2target.Diagram]*d2target.Diagram)
	build := func(diagram *d2target.Diagram) (*d2scene.Document, error) {
		sceneOpts := sceneOptionsForFrame(gifFrameOptions(0, paintCount))
		sceneOpts.omitAppendix = forceAppendix == nil || !*forceAppendix
		return buildSceneWithResourcesAndLinks(ctx, diagram, opts, assets, fonts, links, sceneOpts)
	}
	for i, sample := range plan {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if sample.leg.From != nil {
			continue
		}
		boardGroups[sample.board] = sample.group
		renderOpts := gifFrameOptions(time.Duration(sample.TimeMS)*time.Millisecond, paintCount)
		doc, err := build(sample.diagram)
		if err != nil {
			return nil, nil, fmt.Errorf("storyboard %s at %dms: %w", sample.Board, sample.TimeMS, err)
		}
		renderOpts.Scale = storyboardScale(doc.LogicalWidth, doc.LogicalHeight)
		renderOpts.MaxWidth, renderOpts.MaxHeight = storyboardWidth, storyboardHeight
		pixels, err := session.Render(ctx, doc, renderOpts)
		if err != nil {
			return nil, nil, fmt.Errorf("storyboard %s at %dms: %w", sample.Board, sample.TimeMS, err)
		}
		if err := validateRenderSession("storyboard", session); err != nil {
			return nil, nil, err
		}
		// Keep only bounded thumbnails. Include scene decorations extending
		// beyond diagram bounds (for example, legends) in the shared camera.
		x, y := int(doc.ViewBox.X), int(doc.ViewBox.Y)
		bounds := image.Rect(x, y, x+int(doc.LogicalWidth), y+int(doc.LogicalHeight))
		tiles[i] = storyboardTile{pixels, bounds}
		groups[sample.group] = groups[sample.group].Union(sample.filmBounds.Inset(-pad)).Union(bounds)
	}
	var canvas image.Rectangle
	for _, group := range groups {
		canvas = canvas.Union(image.Rect(0, 0, group.Dx(), group.Dy()))
	}
	offset := func(group *d2target.Diagram) image.Point {
		box := groups[group]
		return image.Pt((canvas.Dx()-box.Dx())/2, (canvas.Dy()-box.Dy())/2).Sub(box.Min)
	}
	scale := storyboardScale(float64(canvas.Dx()), float64(canvas.Dy()))
	// Rasterize zoom poses directly on the bounded thumbnail canvas so a
	// portal's outline and text stay sharp as they grow.
	zoomCanvas := image.Rect(0, 0, max(1, int(math.Round(float64(canvas.Dx())*scale))), max(1, int(math.Round(float64(canvas.Dy())*scale))))
	cameraBoard := func(board, picture *d2target.Diagram) (*d2scene.Document, image.Point, error) {
		doc, err := build(picture)
		if err != nil {
			return nil, image.Point{}, err
		}
		shift := offset(boardGroups[board])
		at := image.Pt(int(math.Round((doc.ViewBox.X+float64(shift.X))*scale)), int(math.Round((doc.ViewBox.Y+float64(shift.Y))*scale)))
		doc.LogicalWidth *= scale
		doc.LogicalHeight *= scale
		return doc, at, nil
	}
	for i, sample := range plan {
		if sample.leg.From == nil {
			tiles[i].bounds = tiles[i].bounds.Add(offset(sample.group))
			continue
		}
		from, fromOffset, err := cameraBoard(sample.leg.From, sample.diagram)
		if err != nil {
			return nil, nil, fmt.Errorf("storyboard %s at %dms: %w", sample.Board, sample.TimeMS, err)
		}
		to, toOffset, err := cameraBoard(sample.leg.To, sample.arrival)
		if err != nil {
			return nil, nil, fmt.Errorf("storyboard %s at %dms: %w", sample.Board, sample.TimeMS, err)
		}
		camera := newGIFLayerCamera(zoomCanvas, sample.leg.Zoom, from, fromOffset, to, toOffset)
		renderOpts := gifFrameOptions(time.Duration(sample.TimeMS)*time.Millisecond, paintCount)
		pixels, err := camera.render(ctx, session, renderOpts, sample.progress, renderOpts.Time)
		if err != nil {
			return nil, nil, fmt.Errorf("storyboard %s at %dms: %w", sample.Board, sample.TimeMS, err)
		}
		if err := validateRenderSession("storyboard", session); err != nil {
			return nil, nil, err
		}
		tiles[i] = storyboardTile{pixels, canvas}
	}
	faceData := d2fonts.FontFaces.Get(d2fonts.SourceSansPro.Font(0, d2fonts.FONT_STYLE_REGULAR))
	captionFont, err := opentype.Parse(faceData)
	if err != nil {
		return nil, nil, err
	}
	face, err := opentype.NewFace(captionFont, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, nil, err
	}
	defer face.Close()

	cols := int(math.Ceil(math.Sqrt(float64(len(plan)) * float64(storyboardHeight+storyboardCaption) / storyboardWidth)))
	rows := (len(plan) + cols - 1) / cols
	sheet := image.NewNRGBA(image.Rect(0, 0, storyboardGap+cols*(storyboardWidth+storyboardGap), storyboardGap+rows*(storyboardHeight+storyboardCaption+storyboardGap)))
	background := image.NewUniform(color.NRGBA{R: 235, G: 237, B: 240, A: 255})
	draw.Draw(sheet, sheet.Bounds(), background, image.Point{}, draw.Src)
	info := make([]storyboardFrameInfo, len(plan))
	for i, sample := range plan {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		tile := tiles[i]
		origin := image.Pt(storyboardGap+(i%cols)*(storyboardWidth+storyboardGap), storyboardGap+(i/cols)*(storyboardHeight+storyboardCaption+storyboardGap))
		draw.Draw(sheet, image.Rect(0, 0, storyboardWidth, storyboardHeight+storyboardCaption).Add(origin), image.White, image.Point{}, draw.Src)
		// Place the tile in its playback viewport, letterboxed inside the panel.
		left := (storyboardWidth-scale*float64(canvas.Dx()))/2 + scale*float64(tile.bounds.Min.X)
		top := (storyboardHeight-scale*float64(canvas.Dy()))/2 + scale*float64(tile.bounds.Min.Y)
		x, y := int(math.Round(left)), int(math.Round(top))
		right := int(math.Round(left + scale*float64(tile.bounds.Dx())))
		bottom := int(math.Round(top + scale*float64(tile.bounds.Dy())))
		dest := image.Rect(x, y, max(x+1, right), max(y+1, bottom)).Add(origin)
		xdraw.ApproxBiLinear.Scale(sheet, dest, tile.pixels, tile.pixels.Bounds(), draw.Src, nil)
		drawStoryboardCaption(sheet, face, origin.Add(image.Pt(8, storyboardHeight+21)), sample.storyboardFrameInfo)
		info[i] = sample.storyboardFrameInfo
	}
	var encoder rasterPNGEncoder
	defer encoder.close()
	data, err := encoder.encode(ctx, sheet)
	return data, info, err
}

func storyboardScale(width, height float64) float64 {
	scale := min(2, storyboardWidth/width, storyboardHeight/height)
	if scale*width > storyboardWidth || scale*height > storyboardHeight {
		scale = math.Nextafter(scale, 0)
	}
	return scale
}

func drawStoryboardCaption(dst draw.Image, face font.Face, at image.Point, info storyboardFrameInfo) {
	t := info.TimeMS
	timestamp := fmt.Sprintf("%d:%02d.%03d", t/60000, t/1000%60, t%1000)
	d := font.Drawer{Dst: dst, Src: image.NewUniform(color.NRGBA{R: 43, G: 50, B: 63, A: 255}), Face: face, Dot: fixed.P(at.X, at.Y)}
	width := fixed.I(storyboardWidth-16) - d.MeasureString(timestamp)
	width -= fixed.I(16)
	// MCP always receives the exact path. Keep the printed caption readable
	// too, even for a script outside the bundled caption font's coverage.
	var name strings.Builder
	var advance fixed.Int26_6
	ellipsis := d.MeasureString("…")
	for _, r := range info.Board {
		label := string(r)
		dx, ok := face.GlyphAdvance(r)
		if !ok {
			quoted := strconv.QuoteRuneToASCII(r)
			label = quoted[1 : len(quoted)-1]
			dx = d.MeasureString(label)
		}
		if advance+dx > width-ellipsis {
			name.WriteString("…")
			break
		}
		name.WriteString(label)
		advance += dx
	}
	d.DrawString(name.String())
	d.Dot.X = fixed.I(at.X+storyboardWidth-16) - d.MeasureString(timestamp)
	d.DrawString(timestamp)
}
