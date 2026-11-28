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
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
)

const (
	videoFPS                  = 30
	videoMaxDuration          = time.Hour
	videoMaxFramePixels int64 = 4096 * 2160
	// Encoded samples include repeats, even when a static board paints only
	// once. Bound pixel throughput separately from the peak size of a frame.
	videoMaxTotalPixels int64 = 4096 * 2160 * videoFPS * 5 * 60
)

type videoSegment struct {
	d2animate.Frame
	rasterPlacement
	start, end time.Duration
	route      []d2animate.LayerLeg
	moving     bool
	matte      color.RGBA
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
		return nil, fmt.Errorf("MP4 duration must be between zero and %s", videoMaxDuration)
	}
	frames, err := d2animate.Timeline(root, intervalMS)
	if err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("MP4 requires at least one renderable board")
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
		s := videoSegment{Frame: f, start: p.period, moving: d2scenebuild.ShortestAnimationPeriod(f.Diagram) > 0}
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
			return nil, fmt.Errorf("MP4 timeline duration overflows at segment %d", i)
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
		return nil, fmt.Errorf("MP4 exceeds the %s duration limit", videoMaxDuration)
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
	if s.moving {
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

// Count actual scene builds (including misses in the two-scene cache) and
// paints before assigning operation-wide budgets. A minute-long static hold
// costs one paint; a moving camera may paint both sides of every transition.
func (p *videoTimeline) work(ctx context.Context) (builds, paints int, err error) {
	var cache gifSceneCache
	visit := func(index int) {
		if !cache.touch(index) {
			builds++
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
			return 0, 0, err
		}
		index, _, repeat, leg, _ := p.sample(i)
		if leg == nil {
			visit(index)
			paints++
		} else {
			if leg != active {
				visit(p.exact[leg.From])
				visit(p.exact[leg.To])
			}
			paints += 2
		}
		active = leg
		i += repeat
	}
	return builds, paints, nil
}

// A video may perform up to 64 single-scene budgets of structural work over
// the export. Individual paints and builds retain the existing scene limits.
// Pixel throughput and the number of output samples have independent bounds.
func videoWorkDivisor(count int) int {
	return max(1, (count+63)/64)
}

// emit consumes opaque, fixed-size images synchronously. The backing pixels
// are reused after it returns. No encoded or full-frame history is retained.
func renderVideoFrames(ctx context.Context, inputPath string, cacheImages bool, diagram *d2target.Diagram, opts d2svg.RenderOpts, intervalMS int, layers gifLayerOptions, duration time.Duration, forceAppendix bool, emit func(image.Image, int) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := planVideo(diagram, intervalMS, layers, duration)
	if err != nil {
		return err
	}
	// The measurement and playback passes share the asset resolver and a
	// two-entry cache of complete scenes, including intermediate camera legs.
	builds, paints, err := p.work(ctx)
	if err != nil {
		return err
	}
	buildDivisor := videoWorkDivisor(builds)
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
	options := gifFrameOptions(0, videoWorkDivisor(paints))
	options.MaxPixels = videoMaxFramePixels
	buildLimits := sceneOptionsForFrame(gifFrameOptions(0, buildDivisor))
	buildLimits.omitAppendix = !forceAppendix
	opts = rasterRenderOptions(opts)
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
		doc, err := buildSceneWithResourcesAndLinks(ctx, s.Diagram, opts, assets, fonts, links, limits)
		if err != nil {
			return nil, fmt.Errorf("MP4 segment %d: %w", index, err)
		}
		cache.docs[0] = doc
		return doc, nil
	}
	theme := d2themescatalog.Find(themeID(opts))
	theme.ApplyOverrides(opts.ThemeOverrides)
	var width, height int
	for i := range p.segments {
		doc, err := load(i)
		if err != nil {
			return err
		}
		bounds, err := animationFrameBounds(doc, "MP4", videoMaxFramePixels)
		if err != nil {
			return err
		}
		p.segments[i].matte = videoBoardMatte(theme, p.segments[i].Diagram.Root.Fill)
		group := p.segments[i].measure(bounds, doc, *opts.Scale)
		width, height = max(width, group.Dx()), max(height, group.Dy())
	}
	// H.264 4:2:0 uses even dimensions. Padding the final row/column keeps
	// labels at the authored scale.
	width, height = width+width%2, height+height%2
	pixels := int64(width) * int64(height)
	if width > rasterMaxDimension || height > rasterMaxDimension || pixels <= 0 || pixels > videoMaxFramePixels {
		return fmt.Errorf("MP4 canvas %dx%d exceeds the %d-pixel frame limit", width, height, videoMaxFramePixels)
	}
	if pixels > videoMaxTotalPixels/int64(p.frames) {
		return fmt.Errorf("MP4 canvas %dx%d over %d frames exceeds the %d cumulative-pixel limit", width, height, p.frames, videoMaxTotalPixels)
	}
	canvas := image.Rect(0, 0, width, height)
	var padded *image.RGBA
	var workspace d2raster.RenderWorkspace
	defer workspace.Reset()
	var active *d2animate.LayerLeg
	var camera *gifLayerCamera
	var fromMatte, toMatte color.RGBA
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
					return fmt.Errorf("MP4 layer route leaves the selected boards")
				}
				a, err := load(from)
				if err != nil {
					return err
				}
				b, err := load(to)
				if err != nil {
					return err
				}
				camera = newGIFLayerCamera(canvas, leg.Zoom, a, p.segments[from].offset(canvas), b, p.segments[to].offset(canvas))
				fromMatte, toMatte = p.segments[from].matte, p.segments[to].matte
				active = leg
			}
			cameraOptions := options
			cameraOptions.Background = blendVideoMatte(fromMatte, toMatte, progress)
			pixels, err := camera.render(ctx, session, cameraOptions, progress, 0)
			if err != nil {
				return err
			}
			if err := emit(pixels, repeat); err != nil {
				return err
			}
		} else {
			active, camera = nil, nil
			doc, err := load(index)
			if err != nil {
				return err
			}
			err = workspace.Render(ctx, session, doc, options, func(frame *image.NRGBA) error {
				origin := p.segments[index].offset(canvas)
				if origin == (image.Point{}) && frame.Bounds() == canvas {
					return emit(frame, repeat)
				}
				if padded == nil {
					padded = image.NewRGBA(canvas)
				}
				draw.Draw(padded, canvas, image.NewUniform(p.segments[index].matte), image.Point{}, draw.Src)
				draw.Draw(padded, frame.Bounds().Add(origin), frame, frame.Bounds().Min, draw.Src)
				return emit(padded, repeat)
			})
			if err != nil {
				return err
			}
		}
		if err := validateRenderSession("MP4", session); err != nil {
			return err
		}
		i += repeat
	}
	return ctx.Err()
}
