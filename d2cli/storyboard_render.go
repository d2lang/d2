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
	diagram    *d2target.Diagram
	filmBounds image.Rectangle // all authored boards and tween frames in world coordinates
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
	if err := d2target.ValidateRenderTarget(root); err != nil {
		return nil, err
	}
	var boards []*d2target.Diagram
	var names []string
	var visit func(*d2target.Diagram, []string) error
	visit = func(b *d2target.Diagram, path []string) error {
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
			boards = append(boards, b)
			names = append(names, name)
		}
		for _, group := range []struct {
			kind     string
			children []*d2target.Diagram
		}{{"layers", b.Layers}, {"scenarios", b.Scenarios}, {"steps", b.Steps}} {
			for _, child := range group.children {
				if err := visit(child, append(path, group.kind, child.Name)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(root, nil); err != nil {
		return nil, err
	}
	if len(boards) == 0 {
		return nil, fmt.Errorf("storyboard requires at least one renderable board")
	}
	// The animated SVG places every board on the same world-space canvas,
	// including geometry in transitions that this sheet does not sample.
	tl, br := root.NestedBoundingBox()
	filmBounds := image.Rect(tl.X, tl.Y, br.X, br.Y)
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
		duration := endpoint.ElapsedMS + endpoint.DurationMS
		if int64(duration) > math.MaxInt64/int64(time.Millisecond)-startMS {
			return nil, fmt.Errorf("storyboard duration exceeds the supported time range")
		}
		add := func(diagram *d2target.Diagram, at int) {
			samples = append(samples, storyboardSample{
				storyboardFrameInfo: storyboardFrameInfo{Board: names[i], TimeMS: startMS + int64(at)},
				diagram:             diagram, filmBounds: filmBounds,
			})
		}
		hold := endpoint.ElapsedMS + endpoint.DurationMS/2
		if extra > 0 && len(frames) > 1 {
			at := min(b.TweenDurationMS/2, endpoint.ElapsedMS-1)
			for _, frame := range frames {
				if at < frame.ElapsedMS+frame.DurationMS {
					add(frame.Diagram, at)
					extra--
					break
				}
			}
		} else if extra > 0 && hold > 0 {
			if period := d2scenebuild.ShortestAnimationPeriod(b); period > 0 {
				first, second := storyboardEffectSamples(startMS, hold, period)
				add(b, first)
				hold = second
				extra--
			}
		}
		add(b, hold)
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

type storyboardTile struct {
	pixels *image.NRGBA
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
	links, err := gifLinkBudget(len(plan))
	if err != nil {
		return nil, nil, err
	}
	opts.Scale = new(1.0) // Rasterize directly at thumbnail resolution.
	tiles := make([]storyboardTile, len(plan))
	pad := d2svg.DEFAULT_PADDING
	if opts.Pad != nil {
		pad = int(*opts.Pad)
	}
	canvas := plan[0].filmBounds.Inset(-pad)
	for i, sample := range plan {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		renderOpts := gifFrameOptions(time.Duration(sample.TimeMS)*time.Millisecond, len(plan))
		sceneOpts := sceneOptionsForFrame(renderOpts)
		sceneOpts.omitAppendix = forceAppendix == nil || !*forceAppendix
		doc, err := buildSceneWithResourcesAndLinks(ctx, sample.diagram, opts, assets, fonts, links, sceneOpts)
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
		canvas = canvas.Union(bounds)
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
	scale := storyboardScale(float64(canvas.Dx()), float64(canvas.Dy()))
	info := make([]storyboardFrameInfo, len(plan))
	for i, sample := range plan {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		tile := tiles[i]
		origin := image.Pt(storyboardGap+(i%cols)*(storyboardWidth+storyboardGap), storyboardGap+(i/cols)*(storyboardHeight+storyboardCaption+storyboardGap))
		draw.Draw(sheet, image.Rect(0, 0, storyboardWidth, storyboardHeight+storyboardCaption).Add(origin), image.White, image.Point{}, draw.Src)
		// Preserve world positions as well as size. A pin in one layer must
		// appear at the same pixel coordinate as that pin in another layer.
		left := (storyboardWidth-scale*float64(canvas.Dx()))/2 + scale*float64(tile.bounds.Min.X-canvas.Min.X)
		top := (storyboardHeight-scale*float64(canvas.Dy()))/2 + scale*float64(tile.bounds.Min.Y-canvas.Min.Y)
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
