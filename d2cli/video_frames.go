package d2cli

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"
	"time"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
)

const (
	videoFPS                  = 30
	videoMaxDuration          = time.Hour
	videoMaxFramePixels int64 = 4096 * 2160
	// Bound total coded pixels separately from peak raster and scene work.
	videoMaxTotalPixels int64 = 4096 * 2160 * videoFPS * 5 * 60
)

type videoSegment struct {
	d2animate.Frame
	rasterPlacement
	start, end  time.Duration
	route       []d2animate.LayerLeg
	movingUntil time.Duration // local board clock
	matte       rasterMattes
}

type videoTimeline struct {
	segments               []videoSegment
	exact                  map[*d2target.Diagram]int
	period, duration, zoom time.Duration
	frames                 int
	finishOnLast           bool
}

// Round the complete output clock up once. Rounding every board separately
// would accumulate drift, especially on diagrams with short tween samples.
func videoFrameCount(duration time.Duration) int {
	return int((duration*time.Duration(videoFPS) + time.Second - 1) / time.Second)
}

func planVideo(root *d2target.Diagram, intervalMS int, layers gifLayerOptions, duration time.Duration) (*videoTimeline, error) {
	if duration < 0 || duration > videoMaxDuration {
		return nil, fmt.Errorf("video duration must be between zero and %s", videoMaxDuration)
	}
	frames, err := d2animate.SceneTimeline(root, intervalMS, layers.duration)
	if err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("video requires at least one renderable board")
	}
	p := &videoTimeline{exact: make(map[*d2target.Diagram]int), zoom: time.Duration(layers.duration) * time.Millisecond}
	var boards []*d2target.Diagram
	for i, f := range frames {
		if !f.TweenSample {
			boards = append(boards, f.Diagram)
			p.exact[f.Diagram] = i
		}
	}
	viewports := gifViewports(root, boards)
	var nav *d2animate.LayerNavigation
	if p.zoom > 0 {
		nav = d2animate.NewLayerNavigation(root, layers.boardPath)
	}
	// The finite tour finishes on its last board. A longer explicit clip
	// repeats the tour and can follow the return portal into its next lap.
	var boardIndex int
	for i, f := range frames {
		s := videoSegment{Frame: f, start: p.period, movingUntil: rasterMovingUntil(f.Picture())}
		if v := viewports[f.Diagram]; v != nil {
			s.viewport = v
			tl, br := f.Diagram.BoundingBox()
			v.bounds = v.bounds.Union(image.Rect(tl.X, tl.Y, br.X, br.Y))
		}
		if !f.TweenSample {
			if nav != nil && boardIndex+1 < len(boards) {
				next := boards[boardIndex+1]
				if len(next.TweenFrames) == 0 {
					s.route = nav.Route(f.Diagram, next)
				}
			}
			boardIndex++
		}
		if len(s.route) > 0 {
			s.DurationMS = max(s.DurationMS, layers.duration)
		}
		if int64(s.DurationMS) > (math.MaxInt64-int64(s.start))/int64(time.Millisecond) {
			return nil, fmt.Errorf("video timeline duration overflows at segment %d", i)
		}
		s.end = s.start + time.Duration(s.DurationMS)*time.Millisecond
		p.period = s.end
		p.segments = append(p.segments, s)
	}
	last := &p.segments[len(p.segments)-1]
	if duration > p.period && len(p.segments) == 1 {
		// Single-board dash and pulse tracks own their phase for the whole clip.
		last.end, p.period = duration, duration
	} else if duration > p.period && nav != nil && len(boards[0].TweenFrames) == 0 {
		last.route = nav.Route(last.Diagram, boards[0])
		if len(last.route) > 0 {
			last.end = max(last.end, last.start+p.zoom)
			p.period = last.end
		}
	}
	p.duration = duration
	if duration == 0 {
		p.duration = p.period
	}
	if p.duration <= 0 || p.duration > videoMaxDuration {
		return nil, fmt.Errorf("video exceeds the %s duration limit", videoMaxDuration)
	}
	p.frames = videoFrameCount(p.duration)
	// A final endpoint can be shorter than one frame. Reserve the last sample
	// for it when a one-pass export would otherwise end on an incoming tween.
	p.finishOnLast = (duration == 0 || duration == p.period) && time.Duration(p.frames-1)*time.Second/videoFPS < last.start
	return p, nil
}

// Return a scene, its local animation time, and the number of identical
// output frames. Only static poses can run through to the next boundary.
func (p *videoTimeline) sample(index int) (segment int, local time.Duration, repeats int, leg *d2animate.LayerLeg, progress float64) {
	at := time.Duration(index) * time.Second / videoFPS
	inCycle := at % p.period
	if p.finishOnLast && index == p.frames-1 {
		inCycle = p.segments[len(p.segments)-1].start
	}
	segment = sort.Search(len(p.segments), func(i int) bool { return p.segments[i].end > inCycle })
	s := &p.segments[segment]
	local = time.Duration(s.ElapsedMS)*time.Millisecond + inCycle - s.start
	stop := s.end
	if len(s.route) > 0 {
		stop -= p.zoom
		if inCycle >= stop {
			phase := float64(inCycle-stop) / float64(p.zoom) * float64(len(s.route))
			n := min(int(phase), len(s.route)-1)
			if n > 0 {
				local = 0
			} // An intermediate layer is only passing through.
			return segment, local, 1, &s.route[n], phase - float64(n)
		}
	}
	if local < s.movingUntil {
		return segment, local, 1, nil, 0
	}
	// Clamp to the requested duration before converting the boundary to
	// frames: an explicitly truncated board can have an arbitrarily long hold.
	boundary := at + min(stop-inCycle, p.duration-at)
	end := min(p.frames, videoFrameCount(boundary))
	if p.finishOnLast && index < p.frames-1 {
		end = min(end, p.frames-1)
	}
	return segment, local, max(1, end-index), nil, 0
}

type videoWork struct{ builds, paints, samples int }

func (w videoWork) checkPixels(output image.Rectangle) error {
	if int64(output.Dx())*int64(output.Dy()) > videoMaxTotalPixels/int64(w.samples) {
		return fmt.Errorf("video canvas %dx%d over %d coded samples exceeds the %d cumulative-pixel limit; reduce --video-size, --scale, or --video-duration", output.Dx(), output.Dy(), w.samples, videoMaxTotalPixels)
	}
	return nil
}

// Every native sample still receives the exact elapsed width. During static
// holds, each extra step can cost one full coded picture. Preserve the native
// work budget by gradually reducing only these optional bar updates.
func (w videoWork) progressSteps(output image.Rectangle) int {
	maxSamples := videoMaxTotalPixels / (int64(output.Dx()) * int64(output.Dy()))
	return int(min(int64(output.Dx()), maxSamples-int64(w.samples)+1))
}

// Count actual scene builds (including misses in the two-scene cache),
// paints, and coded samples before assigning operation-wide budgets.
func (p *videoTimeline) work(ctx context.Context) (videoWork, error) {
	var w videoWork
	var cache gifSceneCache
	visit := func(index int) {
		if !cache.touch(index) {
			w.builds++
		}
	}
	// Even a truncated export measures the full tour, so it retains the same
	// framing as its corresponding prefix in a longer export.
	for i := range p.segments {
		visit(i)
	}
	var active *d2animate.LayerLeg
	for i := 0; i < p.frames; {
		if err := ctx.Err(); err != nil {
			return videoWork{}, err
		}
		index, _, repeat, leg, _ := p.sample(i)
		w.samples++
		if i+repeat == p.frames && repeat > 1 {
			w.samples++ // The final hold has an explicit last tick.
		}
		if leg == nil {
			visit(index)
			w.paints++
		} else {
			if leg != active {
				visit(p.exact[leg.From])
				visit(p.exact[leg.To])
			}
			w.paints += 2
		}
		active = leg
		i += repeat
	}
	return w, nil
}

// A video may perform up to 64 single-scene budgets of structural work over
// the export. Individual paints and builds retain the existing scene limits.
// Pixel throughput and the number of output samples have independent bounds.
func videoWorkDivisor(count int) int {
	return max(1, (count+63)/64)
}

// Some FFmpeg versions discard the final packet duration. Separate the last
// tick of the final hold while the renderer still has its pixels. Every other
// hold gets its exact duration from the next pose's presentation timestamp.
func emitVideoHold(frame image.Image, repeats int, final bool, emit func(image.Image, int) error) error {
	if final && repeats > 1 {
		if err := emit(frame, repeats-1); err != nil {
			return err
		}
		repeats = 1
	}
	return emit(frame, repeats)
}

// The rasterizer composites settled scenes onto an opaque background, and
// camera frames arrive already composited onto their matte. At alpha 255,
// NRGBA and RGBA share the same bytes, so placing either requires only a copy.
func placeVideoFrame(dst *image.RGBA, frame draw.Image, origin image.Point, matte color.Color) {
	bounds := dst.Bounds()
	target := frame.Bounds().Add(origin)
	// Clear every uncovered row or column on every pose: the previous
	// board may have occupied a different rectangle or used another matte.
	fill := image.NewUniform(matte)
	draw.Draw(dst, image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, target.Min.Y), fill, image.Point{}, draw.Src)
	draw.Draw(dst, image.Rect(bounds.Min.X, target.Min.Y, target.Min.X, target.Max.Y), fill, image.Point{}, draw.Src)
	draw.Draw(dst, image.Rect(target.Max.X, target.Min.Y, bounds.Max.X, target.Max.Y), fill, image.Point{}, draw.Src)
	draw.Draw(dst, image.Rect(bounds.Min.X, target.Max.Y, bounds.Max.X, bounds.Max.Y), fill, image.Point{}, draw.Src)

	if opaque, ok := frame.(*image.NRGBA); ok {
		frame = &image.RGBA{Pix: opaque.Pix, Stride: opaque.Stride, Rect: opaque.Rect}
	}
	draw.Draw(dst, target, frame, frame.Bounds().Min, draw.Src)
}

// emit consumes opaque, fixed-size images synchronously. The backing pixels
// are reused after it returns. No encoded or full-frame history is retained.
func renderVideoFrames(ctx context.Context, inputPath string, cacheImages bool, diagram *d2target.Diagram, opts d2svg.RenderOpts, intervalMS int, layers gifLayerOptions, duration time.Duration, forceAppendix bool, size videoSize, emit func(image.Image, int) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ink, err := animationProgressInk(opts)
	if err != nil {
		return err
	}
	overlay := videoProgressOverlay{ink: ink}
	p, err := planVideo(diagram, intervalMS, layers, duration)
	if err != nil {
		return err
	}
	// The measurement and playback passes share the asset resolver and a
	// two-entry cache of complete scenes, including intermediate camera legs.
	work, err := p.work(ctx)
	if err != nil {
		return err
	}
	buildDivisor := videoWorkDivisor(work.builds)
	// Leave at least one declared resource and one expanded use available to
	// each SVG importer. Even the longest clip can visit at most two scenes
	// per sampled timestamp, plus the bounded one-pass measurement.
	importDivisor := min(buildDivisor, svgDocumentMaxDeclaredResources, svgDocumentMaxExpandedUseInstances)
	assets, err := animationSceneAssetOptions(ctx, inputPath, cacheImages, importDivisor, videoMaxFramePixels)
	if err != nil {
		return err
	}
	defer assets.Resolver.CloseIdleConnections()
	fonts, err := newFontFallbackOptions(buildDivisor)
	if err != nil {
		return err
	}
	links, err := gifLinkBudget(buildDivisor)
	if err != nil {
		return err
	}
	session, err := newGIFRenderSession()
	if err != nil {
		return err
	}
	options := gifFrameOptions(0, videoWorkDivisor(work.paints))
	options.MaxPixels = videoMaxFramePixels
	buildLimits := sceneOptionsForFrame(gifFrameOptions(0, buildDivisor))
	buildLimits.omitAppendix = !forceAppendix
	opts = rasterRenderOptions(opts)
	authoredScale := *opts.Scale
	opts.Scale = nil // First measure vector scenes in their authored coordinate system.
	var cache gifSceneCache
	load := func(index int) (*d2scene.Document, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cache.touch(index) {
			return cache.docs[0], nil
		}
		s := &p.segments[index]
		limits := buildLimits
		if s.viewport != nil {
			limits.minimumBounds = &s.viewport.bounds
		}
		doc, err := buildSceneWithResourcesAndLinks(ctx, s.Picture(), opts, assets, fonts, links, limits)
		if err != nil {
			return nil, fmt.Errorf("video segment %d: %w", index, err)
		}
		cache.docs[0] = doc
		return doc, nil
	}
	theme := d2themescatalog.Find(themeID(opts))
	theme.ApplyOverrides(opts.ThemeOverrides)
	boxes := make([]d2scene.Box, len(p.segments))
	for i := range p.segments {
		doc, err := load(i)
		if err != nil {
			return err
		}
		boxes[i] = doc.ViewBox
		p.segments[i].matte = boardRasterMattes(theme, p.segments[i].Diagram.Root.Fill)
	}
	framing, err := p.frameSizes(boxes, authoredScale, size, ink != nil)
	if err != nil {
		return err
	}
	canvas, output := framing.camera, framing.output
	if err := work.checkPixels(output); err != nil {
		return err
	}
	overlay.steps = work.progressSteps(output)
	overlay.height = framing.lineHeight
	loadForPlayback := func(index int) (*d2scene.Document, error) {
		doc, err := load(index)
		if err != nil {
			return nil, err
		}
		scaled := *doc
		scaled.LogicalWidth = float64(p.segments[index].bounds.Dx())
		scaled.LogicalHeight = float64(p.segments[index].bounds.Dy())
		return &scaled, nil
	}
	// Rasterize each pose at delivery resolution. Only integer translation
	// remains when placing it on the shared canvas or inside the letterbox.
	var letterboxed *image.RGBA
	present := func(frame draw.Image, origin image.Point, matte rasterMattes, start, repeat int) error {
		origin = origin.Add(framing.margin)
		if origin == (image.Point{}) && frame.Bounds() == output {
			return overlay.emit(ctx, frame, start, repeat, p.frames, emit)
		}
		if letterboxed == nil {
			letterboxed = image.NewRGBA(output)
		}
		placeVideoFrame(letterboxed, frame, origin, matte.board)
		if ink != nil && matte.footer != matte.board {
			footer := output
			footer.Min.Y = framing.artHeight
			draw.Draw(letterboxed, footer, image.NewUniform(matte.footer), image.Point{}, draw.Src)
		}
		return overlay.emit(ctx, letterboxed, start, repeat, p.frames, emit)
	}
	var padded *image.RGBA
	var workspace d2raster.RenderWorkspace
	defer workspace.Reset()
	var active *d2animate.LayerLeg
	var camera *gifLayerCamera
	var fromMatte, toMatte rasterMattes
	for i := 0; i < p.frames; {
		if err := ctx.Err(); err != nil {
			return err
		}
		index, local, repeat, leg, progress := p.sample(i)
		options.Time = local
		if leg != nil {
			if leg != active {
				from, haveFrom := p.exact[leg.From]
				to, haveTo := p.exact[leg.To]
				if !haveFrom || !haveTo {
					return fmt.Errorf("video layer route leaves the selected boards")
				}
				a, err := loadForPlayback(from)
				if err != nil {
					return err
				}
				b, err := loadForPlayback(to)
				if err != nil {
					return err
				}
				camera = newGIFLayerCamera(canvas, leg.Zoom, a, p.segments[from].offset(canvas), b, p.segments[to].offset(canvas))
				fromMatte, toMatte = p.segments[from].matte, p.segments[to].matte
				active = leg
			}
			cameraOptions := options
			matte := fromMatte.blend(toMatte, progress)
			cameraOptions.Background = matte.board
			if padded == nil {
				padded = image.NewRGBA(canvas)
			}
			if err := camera.renderInto(ctx, session, &workspace, cameraOptions, progress, 0, padded); err != nil {
				return err
			}
			if err := present(padded, image.Point{}, matte, i, repeat); err != nil {
				return err
			}
		} else {
			active, camera = nil, nil
			doc, err := loadForPlayback(index)
			if err != nil {
				return err
			}
			err = workspace.Render(ctx, session, doc, options, func(frame *image.NRGBA) error {
				return present(frame, p.segments[index].offset(canvas), p.segments[index].matte, i, repeat)
			})
			if err != nil {
				return err
			}
		}
		if err := validateRenderSession("video", session); err != nil {
			return err
		}
		i += repeat
	}
	return ctx.Err()
}
