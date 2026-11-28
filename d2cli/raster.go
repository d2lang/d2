package d2cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"image"
	"image/color"
	"math"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/d2svgimport"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/imageasset"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/xgif"
)

func renderRasterSVG(diagram *d2target.Diagram, opts d2svg.RenderOpts, returnSVG bool) ([]byte, error) {
	if !returnSVG {
		return nil, nil
	}
	return d2svg.Render(diagram, &opts)
}

const (
	// These production safety ceilings bound raster work independently
	// of the caller's input source.
	// Admit long, narrow diagrams while rasterMaxPixels bounds complete-frame
	// allocations. Streaming PNG uses these dimensions with a bounded strip.
	rasterMaxDimension                = 32_768
	rasterMaxPixels             int64 = 64 * 1024 * 1024
	rasterMaxNodes                    = 1_000_000
	rasterMaxDepth                    = 1_024
	rasterMaxPathCommands             = 10_000_000
	rasterMaxTextRunesPerRun          = 100_000
	rasterMaxFontFacesPerText         = fontAssetReserve
	rasterMaxTextCoverageChecks int64 = 10_000_000
	rasterMaxTextShapingRuns          = 100_000
	rasterMaxAnimationTracks          = 1_000_000
	rasterMaxAnimationKeyframes       = 10_000_000
	rasterMaxOffscreenBytes     int64 = 512 * 1024 * 1024
	rasterMaxEvenOddClipWork    int64 = 250_000_000
	// Scanline work counts conservative edge-row visits, horizontal crossings,
	// and painted/cleared spans across the complete export.
	rasterMaxScanlineWork int64 = 4_000_000_000

	// GIF rendering keeps only indexed-color frames between samples. Bound its
	// schedule, retained pixels, transient quantizer work, and encoded output
	// independently of the one-frame renderer ceilings.
	gifMaxFrames            = 1_800
	gifMaxFramePixels int64 = 2 * 1024 * 1024
	// Median-cut quantization has substantial non-cancellable scratch storage.
	// Render and quantize one full-color frame at a time, while retaining only
	// its smaller indexed-color result.
	gifMaxFrameOffscreenBytes int64 = 128 * 1024 * 1024
	// Animated SVG clips repeat their work for every sample. This GIF-specific
	// aggregate admits complex animation while retaining a total-work ceiling;
	// the smaller static export allowance remains unchanged.
	gifMaxEvenOddClipWork int64 = 768 * 1024 * 1024
	gifMaxScanlineWork    int64 = 8_000_000_000
	// GIF normalizes every frame to max(width) x max(height). This ceiling is
	// charged against the retained indexed-color canvas, not the smaller source
	// rectangles. The encoder output has a separate bound because compressed
	// bytes and buffer capacity are not represented by this pixel accounting.
	// A board is held for as long as its caption takes to read, and a hold is
	// sampled like any other stretch of the clock. The ceiling admits a
	// narrated explainer of about twenty seconds at 700 by 600.
	gifMaxNormalizedPixels int64 = 256 * 1024 * 1024
	gifMaxEncodedBytes     int64 = 64 * 1024 * 1024
	// The watch preview is generated from trusted d2svg output, but it can still
	// multiply one asset across many image elements. Bound both the input scan
	// and the fully expanded data-URI output before allocating the result.
	rasterPreviewMaxSourceBytes           = 32 * 1024 * 1024
	rasterPreviewMaxImageReferences       = 4_096
	rasterPreviewMaxOutputBytes     int64 = 192 * 1024 * 1024

	rasterMaxAssets             = 4_096
	rasterMaxAssetBytes   int64 = 512 * 1024 * 1024
	rasterMaxDecodedBytes int64 = 512 * 1024 * 1024
	// Document.Assets also retains bundled font faces. Keep image resolution
	// below the renderer's all-asset ceiling so a boundary-valid image set does
	// not fail only after fonts have been added.
	fontAssetReserve                 = 64
	fontAssetByteReserve       int64 = 64 * 1024 * 1024
	fontMaxRunesPerText              = rasterMaxTextRunesPerRun
	fontMaxTotalRunes                = rasterMaxPathCommands
	fontMaxCoverageChecks      int64 = 50_000_000
	fontSearchDirectoryEntries       = 20_000
	fontSearchFiles                  = 4_096
	fontSearchFaces                  = 8_192
	fontSearchRequestedRunes         = fontMaxRunesPerText
	fontSearchCoverageChecks         = 10_000_000
	fontSearchFileBytes        int64 = 32 * 1024 * 1024
	fontSearchScannedBytes     int64 = 256 * 1024 * 1024
	// Resolvers return owned bytes for each style bucket, including content and
	// face pairs already retained by an earlier bucket. Bound cumulative
	// resolver output separately from retained scene bytes.
	fontSearchResolvedBytes int64 = 2 * fontAssetByteReserve
	// The export-scoped resolver retains one immutable copy of the bundled
	// color face and shares it across every board scene. Keep that copy under
	// an independent bound in addition to the downstream operation ceiling.
	fontBundledCopyBytes     int64 = 8 * 1024 * 1024
	fontBundledResolvedBytes       = fontBundledCopyBytes + fontSearchResolvedBytes
	gifMaxAssets                   = 1_024
	gifMaxAssetBytes         int64 = 128 * 1024 * 1024
	gifMaxDecodedAssetBytes  int64 = 128 * 1024 * 1024
	// A GIF render session retains the full immutable asset-key memo (up to
	// 128 MiB), decoded raster pixels (up to 128 MiB), parsed font sources (up
	// to the 64 MiB reserve), and bounded entry metadata. Keeping this above
	// that arithmetic ceiling makes every repeated frame a cache hit rather
	// than silently hashing, parsing, or decoding the same assets again.
	gifRenderCacheBytes   int64 = 321 * 1024 * 1024
	gifImageAssetMaxCount       = gifMaxAssets - fontAssetReserve
	gifImageEncodedBytes  int64 = gifMaxAssetBytes - fontAssetByteReserve
	gifImageDecodedBytes  int64 = gifMaxDecodedAssetBytes

	imageAssetMaxBytes                  int64 = 32 * 1024 * 1024
	imageAssetMaxCount                        = rasterMaxAssets - fontAssetReserve
	imageAssetMaxCumulativeEncodedBytes int64 = rasterMaxAssetBytes - fontAssetByteReserve
	imageAssetMaxCumulativeDecodedBytes int64 = rasterMaxDecodedBytes
	// SVG embedding uses the same strict PNG, JPEG, GIF, WebP, and SVG resolver
	// as raster export. It has no retained-font reserve, so the complete image
	// count and byte budget remain available to one output document.
	svgBundleMaxAssets             = rasterMaxAssets
	svgBundleMaxEncodedBytes int64 = rasterMaxAssetBytes
	svgBundleMaxDecodedBytes int64 = rasterMaxDecodedBytes
	// Paged exports render each board once, so earlier per-board key memos may be
	// evicted. This cap still retains the complete 512 MiB decoded-raster
	// budget, 64 MiB of parsed font sources, one maximum-size 32 MiB key memo,
	// and charged entry metadata without an admission skip.
	pagedRenderCacheBytes int64 = 612 * 1024 * 1024
	assetCacheMaxBytes    int64 = 512 * 1024 * 1024
	assetCacheNamespace         = "d2cli/imageasset/v1/default-http"

	// Static raster exports paint at most two appendix items (tooltip and
	// link) per typed shape or connection region; each Markdown link adds at
	// most one title row. Keep the metadata strings and resulting scene nodes
	// bounded even for PNG/GIF callers that do not consume typed PDF/PPTX
	// annotations.
	rasterMaxLinkRegions     = 4_096
	rasterMaxLinkStringBytes = 1 * 1024 * 1024

	// Per-icon ceilings cover the hosted SVG catalog, including detailed paths
	// and embedded raster thumbnails. Diagrams also share the totals below.
	svgMaxBytes              = 256 * 1024
	svgMaxDecodedRasterBytes = 1 << 20
	svgMaxDepth              = 32
	svgMaxElements           = 2_048
	svgMaxAttributes         = 4_096
	svgMaxAttributeBytes     = 256 * 1024
	svgMaxPathCommands       = 16_384
	svgMaxTransformFunctions = 512
	svgMaxUseDepth           = 8
	svgMaxResources          = 512

	svgDocumentMaxSourceBytes          = 1 * 1024 * 1024
	svgDocumentMaxElements             = 4_096
	svgDocumentMaxAttributes           = 8_192
	svgDocumentMaxAttributeBytes       = 1 * 1024 * 1024
	svgDocumentMaxPathCommands         = 65_536
	svgDocumentMaxTransformFunctions   = 2_048
	svgDocumentMaxDeclaredResources    = 1_024
	svgDocumentMaxExpandedUseInstances = 1_024

	assetHTTPMaxIdleConns          = 32
	assetHTTPMaxIdleConnsPerHost   = 16
	assetHTTPMaxConnsPerHost       = 32
	assetHTTPMaxResponseHeaderSize = 64 << 10
)

var (
	assetCacheOnce sync.Once
	assetCache     *imageasset.MemoryCache
	assetCacheErr  error

	// A process-owned client lets watch mode and repeated uncached renders reuse
	// connections instead of leaving one idle pool behind per Resolver.
	assetHTTPTransport = newAssetHTTPTransport()
	assetHTTPClient    = &http.Client{
		Transport: assetHTTPTransport,
		Timeout:   time.Minute,
	}
)

func newAssetHTTPTransport() *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport := base.Clone()
		transport.MaxIdleConns = assetHTTPMaxIdleConns
		transport.MaxIdleConnsPerHost = assetHTTPMaxIdleConnsPerHost
		transport.MaxConnsPerHost = assetHTTPMaxConnsPerHost
		transport.MaxResponseHeaderBytes = assetHTTPMaxResponseHeaderSize
		return transport
	}
	return &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		DialContext:            (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           assetHTTPMaxIdleConns,
		MaxIdleConnsPerHost:    assetHTTPMaxIdleConnsPerHost,
		MaxConnsPerHost:        assetHTTPMaxConnsPerHost,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		MaxResponseHeaderBytes: assetHTTPMaxResponseHeaderSize,
	}
}

func renderPNGWithEncoder(ctx context.Context, inputPath string, cacheImages bool, diagram *d2target.Diagram, opts d2svg.RenderOpts, encoder *rasterPNGEncoder) ([]byte, error) {
	document, err := buildScene(ctx, inputPath, cacheImages, diagram, opts)
	if err != nil {
		return nil, err
	}
	frame, err := d2raster.Render(ctx, document, rasterFrameOptions(2, 0))
	if err != nil {
		return nil, err
	}
	var localEncoder rasterPNGEncoder
	if encoder == nil {
		encoder = &localEncoder
		defer localEncoder.close()
	}
	return encoder.encode(ctx, frame)
}

func buildScene(ctx context.Context, inputPath string, cacheImages bool, diagram *d2target.Diagram, opts d2svg.RenderOpts) (*d2scene.Document, error) {
	if diagram == nil {
		return nil, fmt.Errorf("raster export: nil diagram")
	}
	assetOptions, err := sceneAssetOptions(ctx, inputPath, cacheImages)
	if err != nil {
		return nil, err
	}
	defer assetOptions.Resolver.CloseIdleConnections()
	return buildSceneWithAssets(ctx, diagram, opts, assetOptions)
}

func buildSceneWithAssets(ctx context.Context, diagram *d2target.Diagram, opts d2svg.RenderOpts, assetOptions *d2scenebuild.AssetOptions) (*d2scene.Document, error) {
	fontOptions, err := newFontFallbackOptions(1)
	if err != nil {
		return nil, err
	}
	return buildSceneWithAssetsAndFonts(ctx, diagram, opts, assetOptions, fontOptions)
}

func buildSceneWithAssetsAndFonts(ctx context.Context, diagram *d2target.Diagram, opts d2svg.RenderOpts, assetOptions *d2scenebuild.AssetOptions, fontOptions *d2scenebuild.FontFallbackOptions) (*d2scene.Document, error) {
	return buildSceneWithResourcesAndLinks(ctx, diagram, opts, assetOptions, fontOptions, d2scenebuild.LinkBudget{
		MaxRegions: rasterMaxLinkRegions, MaxStringBytes: rasterMaxLinkStringBytes,
	}, sceneFrameOptions{
		maxNodes: rasterMaxNodes, maxPathCommands: rasterMaxPathCommands,
	})
}

func newFontFallbackOptions(totalBoards int) (*d2scenebuild.FontFallbackOptions, error) {
	if totalBoards <= 0 {
		return nil, fmt.Errorf("raster export: font fallback budget requires a positive board count")
	}
	systemFontResolver, err := d2fonts.NewSystemFallbackResolver(d2fonts.SystemFallbackLimits{
		MaxDirectoryEntries: fontSearchDirectoryEntries,
		MaxFiles:            fontSearchFiles,
		MaxFaces:            fontSearchFaces,
		MaxRequestedRunes:   fontSearchRequestedRunes,
		MaxCoverageChecks:   fontSearchCoverageChecks,
		MaxFileBytes:        fontSearchFileBytes,
		MaxScannedBytes:     fontSearchScannedBytes,
		MaxResolvedBytes:    fontSearchResolvedBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("raster export: initialize font fallback resolver: %w", err)
	}
	return bundledFontFallbackOptions(totalBoards, systemFontResolver)
}

func bundledFontFallbackOptions(totalBoards int, next d2fonts.FallbackResolver) (*d2scenebuild.FontFallbackOptions, error) {
	fontResolver, err := d2fonts.NewBundledFallbackResolver(next, d2fonts.BundledFallbackLimits{
		MaxRequestedRunes: fontSearchRequestedRunes,
		MaxBundledBytes:   fontBundledCopyBytes,
		MaxResolvedBytes:  fontBundledResolvedBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("raster export: initialize bundled font fallback resolver: %w", err)
	}
	return &d2scenebuild.FontFallbackOptions{
		Resolver: fontResolver, MaxAssets: fontAssetReserve, MaxBytes: fontAssetByteReserve,
		MaxRunesPerText:     fontMaxRunesPerText,
		MaxTotalRunes:       max(1, fontMaxTotalRunes/totalBoards),
		MaxCoverageChecks:   max(int64(1), fontMaxCoverageChecks/int64(totalBoards)),
		MaxFontFacesPerText: rasterMaxFontFacesPerText,
		MaxShapingRuns:      max(1, rasterMaxTextShapingRuns/totalBoards),
		MaxShapedGlyphs:     max(1, rasterMaxPathCommands/totalBoards),
	}, nil
}

type sceneFrameOptions struct {
	omitAppendix    bool // storyboard follows SVG; other native exports include appendix entries by default.
	minimumBounds   *image.Rectangle
	maxNodes        int
	maxPathCommands int
}

func sceneOptionsForFrame(options d2raster.FrameOptions) sceneFrameOptions {
	return sceneFrameOptions{maxNodes: options.MaxNodes, maxPathCommands: options.MaxPathCommands}
}

func buildSceneWithResourcesAndLinks(ctx context.Context, diagram *d2target.Diagram, opts d2svg.RenderOpts, assetOptions *d2scenebuild.AssetOptions, fontOptions *d2scenebuild.FontFallbackOptions, linkBudget d2scenebuild.LinkBudget, frameOpts sceneFrameOptions) (*d2scene.Document, error) {
	if diagram == nil {
		return nil, fmt.Errorf("raster export: nil diagram")
	}
	// render() walks boards independently. Keep the scene for this board free
	// of child-board metadata so d2scenebuild does not mistake it for a request
	// to compose multiple boards into one frame.
	board := *diagram
	board.Layers = nil
	board.Scenarios = nil
	board.Steps = nil
	sketch := opts.Sketch != nil && *opts.Sketch
	document, err := d2scenebuild.Build(ctx, &board, d2scenebuild.Options{
		Pad:             opts.Pad,
		MinimumBounds:   frameOpts.minimumBounds,
		Scale:           opts.Scale,
		Center:          opts.Center,
		ThemeID:         opts.ThemeID,
		ThemeOverrides:  opts.ThemeOverrides,
		MaxNodes:        frameOpts.maxNodes,
		MaxPathCommands: frameOpts.maxPathCommands,
		Sketch:          sketch,
		SketchBudget: d2scenebuild.SketchBudget{
			MaxOperationSets: rasterMaxNodes,
			MaxOperations:    rasterMaxPathCommands,
			MaxPathCommands:  rasterMaxPathCommands,
		},
		Assets:     assetOptions,
		Fonts:      fontOptions,
		LinkBudget: linkBudget,
		Appendix:   !frameOpts.omitAppendix,
	})
	if err != nil {
		return nil, err
	}
	return document, nil
}

func rasterFrameOptions(scale float64, timestamp time.Duration) d2raster.FrameOptions {
	return d2raster.FrameOptions{
		Scale:                 scale,
		Time:                  timestamp,
		Background:            color.White,
		MaxWidth:              rasterMaxDimension,
		MaxHeight:             rasterMaxDimension,
		MaxPixels:             rasterMaxPixels,
		MaxNodes:              rasterMaxNodes,
		MaxDepth:              rasterMaxDepth,
		MaxPathCommands:       rasterMaxPathCommands,
		MaxTextRunesPerRun:    rasterMaxTextRunesPerRun,
		MaxFontFacesPerText:   rasterMaxFontFacesPerText,
		MaxTextCoverageChecks: rasterMaxTextCoverageChecks,
		MaxTextShapingRuns:    rasterMaxTextShapingRuns,
		MaxAnimationTracks:    rasterMaxAnimationTracks,
		MaxAnimationKeyframes: rasterMaxAnimationKeyframes,
		MaxAssets:             rasterMaxAssets,
		MaxAssetBytes:         rasterMaxAssetBytes,
		MaxDecodedAssetBytes:  rasterMaxDecodedBytes,
		MaxImportDepth:        rasterMaxDepth,
		MaxOffscreenBytes:     rasterMaxOffscreenBytes,
		MaxEvenOddClipWork:    rasterMaxEvenOddClipWork,
		MaxScanlineWork:       rasterMaxScanlineWork,
	}
}

func gifFrameOptions(timestamp time.Duration, totalFrames int) d2raster.FrameOptions {
	options := rasterFrameOptions(1, timestamp)
	options.MaxPixels = gifMaxFramePixels
	// Render preflight repeats for every frame. Divide aggregate work ceilings
	// across the known schedule so many tiny frames cannot multiply a full
	// one-document allowance.
	divisor := max(1, totalFrames)
	options.MaxNodes = max(1, rasterMaxNodes/divisor)
	options.MaxPathCommands = max(1, rasterMaxPathCommands/divisor)
	options.MaxTextCoverageChecks = max(int64(1), rasterMaxTextCoverageChecks/int64(divisor))
	options.MaxTextShapingRuns = max(1, rasterMaxTextShapingRuns/divisor)
	options.MaxAnimationTracks = max(1, rasterMaxAnimationTracks/divisor)
	options.MaxAnimationKeyframes = max(1, rasterMaxAnimationKeyframes/divisor)
	options.MaxEvenOddClipWork = max(int64(1), gifMaxEvenOddClipWork/int64(divisor))
	options.MaxScanlineWork = max(int64(1), gifMaxScanlineWork/int64(divisor))
	options.MaxAssets = gifMaxAssets
	options.MaxAssetBytes = gifMaxAssetBytes
	options.MaxDecodedAssetBytes = gifMaxDecodedAssetBytes
	// GIF frames render serially, so each receives this fixed offscreen ceiling.
	options.MaxOffscreenBytes = gifMaxFrameOffscreenBytes
	return options
}

type gifFrameRenderer func(context.Context, int, time.Duration, d2raster.FrameOptions, func(image.Image) error) error

type gifFrameConsumer func(frameIndex int, frame image.Image) error

type gifBoardPlanConsumer func(totalFrames int, bounds image.Rectangle) error

// renderGIFBoardFrames paints and consumes one frame at a time. The consumer
// can quantize and release each full-color image before the next render begins.
func renderGIFBoardFrames(
	ctx context.Context,
	firstFrame, frameCount, totalFrames int,
	render gifFrameRenderer,
	consume gifFrameConsumer,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if firstFrame < 0 || frameCount <= 0 || totalFrames <= 0 || firstFrame > totalFrames || frameCount > totalFrames-firstFrame {
		return fmt.Errorf("GIF frame schedule must contain a positive board count within the total frame count")
	}
	if render == nil {
		return fmt.Errorf("GIF frame renderer is nil")
	}
	if consume == nil {
		return fmt.Errorf("GIF frame consumer is nil")
	}
	for offset := 0; offset < frameCount; offset++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		frameIndex := firstFrame + offset
		timestamp, err := xgif.AnimationFrameTime(frameIndex)
		if err != nil {
			return err
		}
		emitted := false
		var emitErr error
		err = render(ctx, frameIndex, timestamp, gifFrameOptions(timestamp, totalFrames), func(frame image.Image) error {
			if emitted {
				emitErr = fmt.Errorf("GIF frame %d was rendered more than once", frameIndex)
				return emitErr
			}
			emitted = true
			if frame == nil {
				emitErr = fmt.Errorf("GIF frame %d was not rendered", frameIndex)
				return emitErr
			}
			emitErr = consume(frameIndex, frame)
			return emitErr
		})
		if emitErr != nil {
			return emitErr
		}
		if err != nil {
			return fmt.Errorf("GIF frame %d: %w", frameIndex, err)
		}
		if !emitted {
			return fmt.Errorf("GIF frame %d was not rendered", frameIndex)
		}
	}
	return nil
}

func renderGIF(ctx context.Context, inputPath string, cacheImages bool, diagram *d2target.Diagram, opts d2svg.RenderOpts, intervalMs int, wantPreview bool, layers gifLayerOptions) (encoded, previewSVG []byte, err error) {
	session, err := newGIFRenderSession()
	if err != nil {
		return nil, nil, err
	}
	var workspace d2raster.RenderWorkspace
	defer workspace.Reset()
	var quantizationWorkspace xgif.OpaqueQuantizationWorkspace
	palettedFrames := make([]*image.Paletted, 0)
	var incrementalEncoder *xgif.OpaquePalettedAnimationEncoder
	var incrementalBounds image.Rectangle
	summary, err := renderGIFWithSession(
		ctx, inputPath, cacheImages, diagram, opts, intervalMs, layers, session, &workspace, wantPreview,
		func(totalFrames int, bounds image.Rectangle) error {
			encoder, encoderErr := xgif.NewOpaquePalettedAnimationEncoder(
				ctx, bounds.Dx(), bounds.Dy(), totalFrames, intervalMs, gifMaxEncodedBytes,
			)
			if encoderErr != nil {
				return encoderErr
			}
			incrementalEncoder = encoder
			incrementalBounds = bounds
			return nil
		},
		func(frameIndex int, frame image.Image) error {
			if incrementalEncoder != nil {
				var consumeErr error
				quantizeErr := quantizationWorkspace.Quantize(ctx, frame, func(paletted *image.Paletted) error {
					normalized, normalizeErr := xgif.NormalizePalettedImage(ctx, paletted, incrementalBounds.Dx(), incrementalBounds.Dy())
					if normalizeErr != nil {
						consumeErr = fmt.Errorf("GIF frame %d normalization: %w", frameIndex, normalizeErr)
						return consumeErr
					}
					if encodeErr := incrementalEncoder.WriteFrame(normalized); encodeErr != nil {
						consumeErr = fmt.Errorf("GIF frame %d encoding: %w", frameIndex, encodeErr)
						return consumeErr
					}
					return nil
				})
				if consumeErr != nil {
					return consumeErr
				}
				if quantizeErr != nil {
					return fmt.Errorf("GIF frame %d quantization: %w", frameIndex, quantizeErr)
				}
				return nil
			}
			paletted, quantizeErr := xgif.QuantizeImage(ctx, frame)
			if quantizeErr != nil {
				return fmt.Errorf("GIF frame %d quantization: %w", frameIndex, quantizeErr)
			}
			palettedFrames = append(palettedFrames, paletted)
			return nil
		},
	)
	if err != nil {
		return nil, nil, err
	}
	if incrementalEncoder != nil {
		encoded, err = incrementalEncoder.Finish()
		if err != nil {
			return nil, nil, err
		}
		return encoded, summary.previewSVG, nil
	}
	if len(palettedFrames) != summary.totalFrames {
		return nil, nil, fmt.Errorf("GIF encoded frame count %d differs from render schedule %d", len(palettedFrames), summary.totalFrames)
	}
	encoded, err = xgif.AnimateTimedOpaquePalettedImages(
		ctx, palettedFrames, summary.maxWidth, summary.maxHeight, summary.offsets, summary.delays, gifMaxEncodedBytes,
	)
	if err != nil {
		return nil, nil, err
	}
	return encoded, summary.previewSVG, nil
}

func newGIFRenderSession() (*d2raster.RenderSession, error) {
	session, err := d2raster.NewRenderSession(d2raster.RenderSessionOptions{
		MaxCacheEntries:    gifMaxAssets,
		MaxCacheBytes:      gifRenderCacheBytes,
		MaxConcurrentLoads: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("GIF render session: %w", err)
	}
	return session, nil
}

type gifRenderSummary struct {
	previewSVG          []byte
	totalFrames         int
	delays              []int
	offsets             []image.Point
	maxWidth, maxHeight int
}

// Browsers commonly display a 0cs or 1cs GIF frame for 100ms. Motion can
// sample at 60fps and paint boundaries can be closer still. Pick the nearest
// native sample for each 20ms tick and reserve the last two centiseconds
// for the exact authored endpoint.
func motionGIFFrames(frames []d2animate.Frame) []d2animate.Frame {
	end := frames[len(frames)-1]
	slot := end.ElapsedMS + end.DurationMS
	ticks := (slot + 9) / 10
	endStart := min(end.ElapsedMS/10, ticks-2) * 10
	if endStart < 20 {
		end.ElapsedMS, end.DurationMS = 0, slot
		return append(frames[:0], end)
	}
	tick := func(ms int) int { return (ms + 10) / 20 * 20 }
	samples := frames[:0]
	for _, frame := range frames[:len(frames)-1] {
		start := tick(frame.ElapsedMS)
		if start > endStart-20 {
			break
		}
		if n := len(samples); n > 0 && tick(samples[n-1].ElapsedMS) == start {
			previous := samples[n-1]
			if math.Abs(float64(frame.ElapsedMS-start)) < math.Abs(float64(previous.ElapsedMS-start)) {
				samples[n-1] = frame
			}
			continue
		}
		samples = append(samples, frame)
	}
	// A sparse or stationary part of the transition retains its whole hold.
	for i := range samples {
		start := tick(samples[i].ElapsedMS)
		next := endStart
		if i+1 < len(samples) {
			next = tick(samples[i+1].ElapsedMS)
		}
		samples[i].ElapsedMS, samples[i].DurationMS = start, next-start
	}
	end.ElapsedMS, end.DurationMS = endStart, slot-endStart
	return append(samples, end)
}

// gifBoardFrames schedules an authored step for GIF playback. Supply its
// predecessor only when the two pictures use the same camera.
func gifBoardFrames(scratch []d2animate.Frame, board, previous *d2target.Diagram, intervalMS int) []d2animate.Frame {
	frames := d2animate.AppendBoardFrames(scratch[:0], board, intervalMS)
	// The last sample can split its hold with the endpoint even at 25fps.
	denseMotion := board.LayoutContinuity && len(frames) > 2 &&
		slices.ContainsFunc(frames[:len(frames)-2], func(f d2animate.Frame) bool { return f.DurationMS < 20 })
	if board.GridMotion || denseMotion {
		return motionGIFFrames(frames)
	}
	if len(board.TweenFrames) > 0 {
		return paintGIFFrames(frames, previous)
	}
	return frames
}

func gifStepDelay(frame d2animate.Frame) int {
	return xgif.StepDelay(time.Duration(frame.ElapsedMS)*time.Millisecond, time.Duration(frame.DurationMS)*time.Millisecond, frame.TweenSample)
}

// Paint samples normally last around 40ms, but a short authored endpoint
// hold can quantize to a single centisecond. Keep the original boundaries
// wherever two centiseconds fit on both sides.
func paintGIFFrames(frames []d2animate.Frame, previous *d2target.Diagram) []d2animate.Frame {
	if !slices.ContainsFunc(frames, func(f d2animate.Frame) bool { return gifStepDelay(f) == 1 }) {
		return frames
	}
	end := frames[len(frames)-1]
	slot := end.ElapsedMS + end.DurationMS
	ticks := (slot + 9) / 10
	capacity := max(1, ticks/2)
	frames = slices.DeleteFunc(frames, func(f d2animate.Frame) bool { return gifStepDelay(f) == 0 })
	if capacity == 1 {
		end.ElapsedMS, end.DurationMS = 0, slot
		return append(frames[:0], end)
	}
	if len(frames) > capacity && previous != nil && sameGIFAppearance(previous, frames[0].Diagram) {
		// Its starting picture already had a full board slot. Spend the scarce
		// time on the intermediate paint, including on a two-sample tween.
		frames = frames[1:]
	}
	if len(frames) > capacity {
		// A selected step may have no preceding picture. Keep both ends and
		// the intermediate samples nearest evenly spaced indices.
		for i := range capacity {
			index := (i*(len(frames)-1) + (capacity-1)/2) / (capacity - 1)
			frames[i] = frames[index]
		}
		frames = frames[:capacity]
	}
	start := 0
	for i := range frames {
		next := slot
		if remaining := len(frames) - i - 1; remaining > 0 {
			native := frames[i+1].ElapsedMS / 10
			next = 10 * max(start/10+2, min(native, ticks-2*remaining))
		}
		frames[i].ElapsedMS, frames[i].DurationMS = start, next-start
		start = next
	}
	return frames
}

func sameGIFAppearance(a, b *d2target.Diagram) bool {
	// Root.Label names the board in paged exports and is absent from GIF.
	root := a.Root
	root.Label = b.Root.Label
	return reflect.DeepEqual(root, b.Root) && reflect.DeepEqual(a.Shapes, b.Shapes) &&
		reflect.DeepEqual(a.Connections, b.Connections) && reflect.DeepEqual(a.Legend, b.Legend) &&
		reflect.DeepEqual(a.FontFamily, b.FontFamily) && reflect.DeepEqual(a.MonoFontFamily, b.MonoFontFamily)
}

func renderGIFWithSession(
	ctx context.Context,
	inputPath string,
	cacheImages bool,
	diagram *d2target.Diagram,
	opts d2svg.RenderOpts,
	intervalMs int,
	layers gifLayerOptions,
	session *d2raster.RenderSession,
	workspace *d2raster.RenderWorkspace,
	wantPreview bool,
	plan gifBoardPlanConsumer,
	consume gifFrameConsumer,
) (gifRenderSummary, error) {
	var summary gifRenderSummary
	if ctx == nil {
		ctx = context.Background()
	}
	if session == nil {
		return summary, fmt.Errorf("GIF requires a render session")
	}
	if consume == nil {
		return summary, fmt.Errorf("GIF frame consumer is nil")
	}
	framesPerBoard, err := xgif.AnimationFrameCount(intervalMs)
	if err != nil {
		return summary, err
	}
	if framesPerBoard > gifMaxFrames {
		return summary, fmt.Errorf("GIF requires %d frames per board, exceeding the %d-frame limit", framesPerBoard, gifMaxFrames)
	}
	boards, err := collectGIFBoards(ctx, diagram, gifMaxFrames)
	if err != nil {
		return summary, err
	}
	if len(boards) == 0 {
		return summary, fmt.Errorf("GIF animation requires at least one renderable board")
	}
	if err := d2target.ValidateRenderTarget(diagram); err != nil {
		return summary, err
	}
	var navigation *d2animate.LayerNavigation
	if layers.duration > 0 {
		navigation = d2animate.NewLayerNavigation(diagram, layers.boardPath)
	}
	viewports := gifViewports(diagram, boards)
	var timeline []gifSegment
	totalFrames, extraPaints := 0, 0
	var boardFrames []d2animate.Frame
	for i, board := range boards {
		var previous *d2target.Diagram
		if i > 0 && viewports[boards[i-1]] == viewports[board] {
			previous = boards[i-1]
		}
		boardFrames = gifBoardFrames(boardFrames, board, previous, intervalMs)
		for _, segment := range boardFrames {
			var route []d2animate.LayerLeg
			if navigation != nil && !segment.TweenSample {
				next := boards[(i+1)%len(boards)]
				if len(next.TweenFrames) == 0 {
					route = navigation.Route(board, next)
				}
				if len(route) > 0 {
					segment.DurationMS = max(segment.DurationMS, layers.duration)
				}
			}
			delay := gifStepDelay(segment)
			if delay == 0 {
				continue
			}
			count := 1
			if !segment.TweenSample {
				count, err = xgif.AnimationFrameCount(segment.DurationMS)
				if err != nil {
					return summary, err
				}
			}
			if len(board.TweenFrames) > 0 || board.GridMotion {
				// A short endpoint hold can otherwise be split into 1cs frames.
				count = min(count, max(1, delay/2))
			}
			if count > gifMaxFrames-totalFrames {
				return summary, fmt.Errorf("GIF animation exceeds the %d-frame limit", gifMaxFrames)
			}
			summary.delays = xgif.AppendFrameDelays(summary.delays, delay, count)
			zoomFrames := 0
			if len(route) > 0 {
				// A zoom paints two separate scenes on the same frame. Charge
				// that extra raster work against the shared animation budget.
				elapsed := 0
				for _, cs := range summary.delays[totalFrames:] {
					if elapsed > segment.DurationMS-layers.duration {
						zoomFrames++
					}
					elapsed += cs * 10
				}
			}
			viewport := viewports[segment.Diagram]
			if viewport != nil {
				tl, br := segment.Diagram.BoundingBox()
				viewport.bounds = viewport.bounds.Union(image.Rect(tl.X, tl.Y, br.X, br.Y))
			}
			timeline = append(timeline, gifSegment{Frame: segment, count: count, viewport: viewport, route: route, zoomFrames: zoomFrames})
			extraPaints += zoomFrames
			totalFrames += count
		}
	}
	hasZoom := extraPaints > 0
	assetOptions, err := gifSceneAssetOptions(ctx, inputPath, cacheImages, len(timeline))
	if err != nil {
		return summary, err
	}
	defer assetOptions.Resolver.CloseIdleConnections()
	fontOptions, err := newFontFallbackOptions(len(timeline))
	if err != nil {
		return summary, err
	}
	linkBudget, err := gifLinkBudget(len(timeline))
	if err != nil {
		return summary, err
	}
	summary.totalFrames = totalFrames
	// Legends, appendix entries and root borders can expand individual scenes
	// beyond the shared object bounds. Preserve their final world-space origins
	// when padding frames for the encoder.
	scale := 1.0
	if opts.Scale != nil {
		scale = *opts.Scale
	}
	boardOpts := rasterRenderOptions(opts)
	// Neighboring zooms use the preceding destination again as their source.
	// Keep only the two endpoint scenes, regardless of how many boards play.
	type cachedScene struct {
		index int
		doc   *d2scene.Document
	}
	var recent, previousScene cachedScene
	loadDocument := func(boardIndex int) (*d2scene.Document, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if hasZoom {
			if previousScene.doc != nil && previousScene.index == boardIndex {
				recent, previousScene = previousScene, recent
			}
			if recent.doc != nil && recent.index == boardIndex {
				return recent.doc, nil
			}
		}
		segment := timeline[boardIndex]
		board := segment.Diagram
		frameOptions := gifFrameOptions(0, totalFrames+extraPaints)
		sceneOpts := sceneOptionsForFrame(frameOptions)
		if segment.viewport != nil {
			sceneOpts.minimumBounds = &segment.viewport.bounds
		}
		document, err := buildSceneWithResourcesAndLinks(ctx, board, boardOpts, assetOptions, fontOptions, linkBudget, sceneOpts)
		if err != nil {
			return nil, fmt.Errorf("GIF board %d scene: %w", boardIndex, err)
		}
		if hasZoom {
			recent, previousScene = cachedScene{boardIndex, document}, recent
		}
		return document, nil
	}
	measure := func(boardIndex int, document *d2scene.Document) error {
		segment := &timeline[boardIndex]
		plannedBounds, err := gifDocumentFrameBounds(document)
		if err != nil {
			return fmt.Errorf("GIF board %d: %w", boardIndex, err)
		}
		canvas := plannedBounds
		frameBounds := plannedBounds
		if v := segment.viewport; v != nil {
			offset := image.Pt(
				int(math.Round((document.ViewBox.X-float64(v.bounds.Min.X))*scale)),
				int(math.Round((document.ViewBox.Y-float64(v.bounds.Min.Y))*scale)),
			)
			frameBounds = plannedBounds.Add(offset)
			v.canvas = v.canvas.Union(frameBounds)
			canvas = v.canvas
		}
		segment.bounds = frameBounds
		summary.maxWidth = max(summary.maxWidth, canvas.Dx())
		summary.maxHeight = max(summary.maxHeight, canvas.Dy())
		if err := validateGIFNormalizedFootprint(summary.maxWidth, summary.maxHeight, totalFrames); err != nil {
			return fmt.Errorf("GIF board %d: %w", boardIndex, err)
		}
		return nil
	}
	// Camera poses use the final GIF canvas, including independent boards and
	// the actual legend and appendix bounds. Measure these before the first zoom.
	var boardIndexByDiagram map[*d2target.Diagram]int
	if hasZoom {
		boardIndexByDiagram = make(map[*d2target.Diagram]int, len(boards))
		for i := range timeline {
			if !timeline[i].TweenSample {
				boardIndexByDiagram[timeline[i].Diagram] = i
			}
			document, err := loadDocument(i)
			if err != nil {
				return summary, err
			}
			if err := measure(i, document); err != nil {
				return summary, err
			}
		}
		if int64(summary.maxWidth)*int64(summary.maxHeight) > gifMaxFramePixels {
			return summary, fmt.Errorf("GIF layer zoom canvas exceeds the %d-pixel frame limit", gifMaxFramePixels)
		}
	}
	zoomCanvas := image.Rect(0, 0, summary.maxWidth, summary.maxHeight)
	loadCamera := func(leg d2animate.LayerLeg) (*gifLayerCamera, error) {
		fromIndex, toIndex := boardIndexByDiagram[leg.From], boardIndexByDiagram[leg.To]
		from, err := loadDocument(fromIndex)
		if err != nil {
			return nil, err
		}
		to, err := loadDocument(toIndex)
		if err != nil {
			return nil, err
		}
		return newGIFLayerCamera(zoomCanvas, leg.Zoom,
			from, timeline[fromIndex].offset(zoomCanvas), to, timeline[toIndex].offset(zoomCanvas)), nil
	}
	renderedFrameCount := 0
	for boardIndex := range timeline {
		document, err := loadDocument(boardIndex)
		if err != nil {
			return summary, err
		}
		if !hasZoom {
			if err := measure(boardIndex, document); err != nil {
				return summary, err
			}
		}
		segment := timeline[boardIndex]
		board := segment.Diagram
		plannedBounds := segment.bounds.Sub(segment.bounds.Min)
		needsPreview := wantPreview && board == diagram && !diagram.IsFolderOnly
		sourceSVG, err := renderRasterSVG(board, boardOpts, needsPreview)
		if err != nil {
			return summary, fmt.Errorf("GIF board %d: %w", boardIndex, err)
		}
		if needsPreview {
			summary.previewSVG = sourceSVG
		}
		if plan != nil && len(boards) == 1 && len(boards[0].TweenFrames) == 0 {
			if err := plan(totalFrames, plannedBounds); err != nil {
				return summary, fmt.Errorf("GIF board %d: %w", boardIndex, err)
			}
		}
		var camera *gifLayerCamera
		activeLeg := -1
		clockMS := 0
		firstFrame := renderedFrameCount
		expectedBounds := plannedBounds
		err = renderGIFBoardFrames(
			ctx, 0, segment.count, totalFrames,
			func(renderCtx context.Context, frameIndex int, _ time.Duration, frameOptions d2raster.FrameOptions, emit func(image.Image) error) error {
				if extraPaints > 0 {
					frameOptions = gifFrameOptions(frameOptions.Time, totalFrames+extraPaints)
				}
				elapsed := clockMS
				clockMS += summary.delays[firstFrame+frameIndex] * 10
				frameOptions.Time += time.Duration(segment.ElapsedMS) * time.Millisecond
				if segment.zoomFrames > 0 && elapsed > segment.DurationMS-layers.duration {
					expectedBounds = zoomCanvas
					// Divide the same transition slot among the path's camera moves.
					// Load an intermediate parent only when its leg first appears.
					legTime := float64(elapsed-(segment.DurationMS-layers.duration)) * float64(len(segment.route)) / float64(layers.duration)
					legIndex := min(int(legTime), len(segment.route)-1)
					if activeLeg != legIndex {
						var err error
						camera, err = loadCamera(segment.route[legIndex])
						if err != nil {
							return err
						}
						activeLeg = legIndex
					}
					if legIndex > 0 {
						// The intermediate board is only passing through its resting
						// pose, so its own independent animation clock stays at zero.
						frameOptions.Time = 0
					}
					frame, err := camera.render(renderCtx, session, frameOptions, legTime-float64(legIndex))
					if err != nil {
						return err
					}
					return emit(frame)
				}
				if workspace == nil {
					frame, renderErr := session.Render(renderCtx, document, frameOptions)
					if renderErr != nil {
						return renderErr
					}
					return emit(frame)
				}
				return workspace.Render(renderCtx, session, document, frameOptions, func(frame *image.NRGBA) error {
					return emit(frame)
				})
			},
			func(frameIndex int, frame image.Image) error {
				if frame.Bounds() != expectedBounds {
					return fmt.Errorf("GIF frame %d bounds %v differ from planned bounds %v", frameIndex, frame.Bounds(), expectedBounds)
				}
				if err := validateRenderSession("GIF", session); err != nil {
					return err
				}
				if err := consume(frameIndex, frame); err != nil {
					return err
				}
				renderedFrameCount++
				return nil
			},
		)
		if err != nil {
			return summary, fmt.Errorf("GIF board %d: %w", boardIndex, err)
		}
	}
	if len(viewports) != 0 || hasZoom {
		canvas := image.Rect(0, 0, summary.maxWidth, summary.maxHeight)
		for _, segment := range timeline {
			offset := segment.offset(canvas)
			for range segment.count - segment.zoomFrames {
				summary.offsets = append(summary.offsets, offset)
			}
			for range segment.zoomFrames {
				summary.offsets = append(summary.offsets, image.Point{})
			}
		}
	}
	if renderedFrameCount != totalFrames {
		return summary, fmt.Errorf("GIF rendered frame count %d differs from schedule %d", renderedFrameCount, totalFrames)
	}
	if len(summary.previewSVG) != 0 {
		summary.previewSVG, err = bundleRasterPreview(ctx, assetOptions, summary.previewSVG)
		if err != nil {
			return summary, fmt.Errorf("GIF watch preview: %w", err)
		}
	}
	return summary, nil
}

func gifDocumentFrameBounds(document *d2scene.Document) (image.Rectangle, error) {
	if document == nil {
		return image.Rectangle{}, fmt.Errorf("GIF requires a scene document")
	}
	widthFloat, heightFloat := document.LogicalWidth, document.LogicalHeight
	if math.IsNaN(widthFloat) || math.IsInf(widthFloat, 0) || widthFloat <= 0 ||
		math.IsNaN(heightFloat) || math.IsInf(heightFloat, 0) || heightFloat <= 0 {
		return image.Rectangle{}, fmt.Errorf("GIF scene has invalid logical dimensions")
	}
	widthCeil, heightCeil := math.Ceil(widthFloat), math.Ceil(heightFloat)
	if widthCeil > rasterMaxDimension || heightCeil > rasterMaxDimension {
		return image.Rectangle{}, fmt.Errorf("GIF scene dimensions %.0fx%.0f exceed the %d-pixel dimension limit", widthCeil, heightCeil, rasterMaxDimension)
	}
	width, height := int(widthCeil), int(heightCeil)
	if int64(width) > gifMaxFramePixels/int64(height) {
		return image.Rectangle{}, fmt.Errorf("GIF frame pixels exceed the %d-pixel limit", gifMaxFramePixels)
	}
	return image.Rect(0, 0, width, height), nil
}

func validateGIFNormalizedFootprint(width, height, totalFrames int) error {
	if width <= 0 || height <= 0 || totalFrames <= 0 {
		return fmt.Errorf("GIF normalized frame footprint requires positive dimensions and frame count")
	}
	maxPixelsPerFrame := gifMaxNormalizedPixels / int64(totalFrames)
	if int64(width) > maxPixelsPerFrame/int64(height) {
		return fmt.Errorf("GIF normalized frame footprint exceeds the %d cumulative-pixel limit", gifMaxNormalizedPixels)
	}
	return nil
}

func gifLinkBudget(totalBoards int) (d2scenebuild.LinkBudget, error) {
	if totalBoards <= 0 {
		return d2scenebuild.LinkBudget{}, fmt.Errorf("GIF requires a positive board count for link budgets")
	}
	return d2scenebuild.LinkBudget{
		MaxRegions:     max(1, rasterMaxLinkRegions/totalBoards),
		MaxStringBytes: max(1, rasterMaxLinkStringBytes/totalBoards),
	}, nil
}

func validateRenderSession(export string, session *d2raster.RenderSession) error {
	if session == nil {
		return fmt.Errorf("%s requires a render session", export)
	}
	stats := session.Stats()
	if stats.SkippedOversize != 0 || stats.MemoSkipped != 0 {
		return fmt.Errorf(
			"%s render cache rejected bounded state (asset skips %d, memo skips %d, retained bytes %d); reduce asset size or count",
			export, stats.SkippedOversize, stats.MemoSkipped, stats.RetainedBytes,
		)
	}
	return nil
}

var rasterImageHrefPrefix = []byte(`<image href="`)
var unavailableImageURI = []byte("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(d2scenebuild.UnavailableImageSVG)))

type rasterPreviewReference struct {
	start  int
	end    int
	source string
}

type rasterPreviewResource struct {
	resource *imageasset.Resource
	uriBytes int
	uri      []byte
}

// bundleRasterPreview embeds the scene's resolved images and any placeholders
// recorded for the previewed board.
func bundleRasterPreview(ctx context.Context, assets *d2scenebuild.AssetOptions, sourceSVG []byte) ([]byte, error) {
	return bundleRasterPreviewWithLimit(ctx, assets, sourceSVG, rasterPreviewMaxOutputBytes)
}

type rasterPreviewSizeError struct{ limit int64 }

func (err *rasterPreviewSizeError) Error() string {
	return fmt.Sprintf("expanded output bytes exceed limit %d", err.limit)
}

func bundleRasterPreviewWithLimit(ctx context.Context, assets *d2scenebuild.AssetOptions, sourceSVG []byte, maxOutput int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(sourceSVG)) > rasterPreviewMaxSourceBytes {
		return nil, fmt.Errorf("source bytes %d exceed limit %d", len(sourceSVG), rasterPreviewMaxSourceBytes)
	}
	if assets == nil || assets.Resolver == nil {
		return nil, fmt.Errorf("image resolver is required")
	}

	references := make([]rasterPreviewReference, 0)
	resources := make(map[string]rasterPreviewResource)
	searchAt := 0
	imageCount := 0
	for {
		relative := bytes.Index(sourceSVG[searchAt:], rasterImageHrefPrefix)
		if relative < 0 {
			break
		}
		start := searchAt + relative + len(rasterImageHrefPrefix)
		relativeEnd := bytes.IndexByte(sourceSVG[start:], '"')
		if relativeEnd < 0 {
			return nil, fmt.Errorf("unterminated image href in generated SVG")
		}
		end := start + relativeEnd
		imageCount++
		if imageCount > rasterPreviewMaxImageReferences {
			return nil, fmt.Errorf("image references %d exceed limit %d", imageCount, rasterPreviewMaxImageReferences)
		}
		source := string(sourceSVG[start:end])
		if !strings.HasPrefix(strings.ToLower(source), "data:") {
			source = html.UnescapeString(source)
			references = append(references, rasterPreviewReference{start: start, end: end, source: source})
			if _, missing := assets.UnavailableImages[source]; missing {
				resources[source] = rasterPreviewResource{uri: unavailableImageURI, uriBytes: len(unavailableImageURI)}
			}
		}
		searchAt = end + 1
	}
	if len(references) == 0 {
		return sourceSVG, nil
	}

	outputBytes := int64(len(sourceSVG))
	for _, reference := range references {
		previewResource, ok := resources[reference.source]
		if !ok {
			resolved, err := assets.Resolver.Resolve(ctx, reference.source)
			if err != nil {
				return nil, err
			}
			if resolved == nil {
				return nil, fmt.Errorf("image resolver returned nil for preview source")
			}
			encodedBytes := resolved.EncodedBytes()
			if encodedBytes < 0 || encodedBytes > int64(^uint(0)>>1) {
				return nil, fmt.Errorf("image encoded byte count %d is not representable", encodedBytes)
			}
			uriBytes := len("data:") + len(resolved.MIMEType()) + len(";base64,") + base64.StdEncoding.EncodedLen(int(encodedBytes))
			previewResource = rasterPreviewResource{resource: resolved, uriBytes: uriBytes}
			resources[reference.source] = previewResource
		}
		delta := int64(previewResource.uriBytes) - int64(reference.end-reference.start)
		if delta > maxOutput-outputBytes {
			return nil, &rasterPreviewSizeError{maxOutput}
		}
		outputBytes += delta
	}
	if outputBytes < 0 || outputBytes > maxOutput {
		return nil, &rasterPreviewSizeError{maxOutput}
	}

	for source, previewResource := range resources {
		if previewResource.uri != nil {
			continue
		}
		uri, err := previewResource.resource.DataURIContext(ctx)
		if err != nil {
			return nil, err
		}
		previewResource.uri = uri
		resources[source] = previewResource
	}

	result := make([]byte, 0, int(outputBytes))
	copyAt := 0
	for _, reference := range references {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result = append(result, sourceSVG[copyAt:reference.start]...)
		result = append(result, resources[reference.source].uri...)
		copyAt = reference.end
	}
	result = append(result, sourceSVG[copyAt:]...)
	if int64(len(result)) != outputBytes {
		return nil, fmt.Errorf("expanded output size %d differs from preflight size %d", len(result), outputBytes)
	}
	return result, nil
}

func rasterRenderOptions(opts d2svg.RenderOpts) d2svg.RenderOpts {
	scale := opts.Scale
	if scale == nil {
		one := 1.0
		scale = &one
	}
	return d2svg.RenderOpts{
		Pad:                opts.Pad,
		Sketch:             opts.Sketch,
		Center:             opts.Center,
		Scale:              scale,
		ThemeID:            opts.ThemeID,
		DarkThemeID:        opts.DarkThemeID,
		ThemeOverrides:     opts.ThemeOverrides,
		DarkThemeOverrides: opts.DarkThemeOverrides,
		OmitVersion:        opts.OmitVersion,
	}
}

func collectGIFBoards(ctx context.Context, root *d2target.Diagram, maxBoards int) ([]*d2target.Diagram, error) {
	if root == nil {
		return nil, fmt.Errorf("GIF: nil diagram")
	}
	if maxBoards <= 0 {
		return nil, fmt.Errorf("GIF board count exceeds the %d-frame limit", gifMaxFrames)
	}
	boards := make([]*d2target.Diagram, 0, min(maxBoards, 16))
	active := make(map[*d2target.Diagram]struct{})
	visited := 0
	var walk func(*d2target.Diagram, int) error
	walk = func(diagram *d2target.Diagram, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if diagram == nil {
			return fmt.Errorf("GIF board tree contains a nil diagram")
		}
		if depth > rasterMaxDepth {
			return fmt.Errorf("GIF board tree exceeds depth %d", rasterMaxDepth)
		}
		visited++
		if visited > d2target.MaxRenderTargetBoards {
			return fmt.Errorf("GIF board tree exceeds %d total nodes", d2target.MaxRenderTargetBoards)
		}
		if _, ok := active[diagram]; ok {
			return fmt.Errorf("GIF board tree contains a cycle")
		}
		active[diagram] = struct{}{}
		defer delete(active, diagram)
		if !diagram.IsFolderOnly {
			if len(boards) == maxBoards {
				return fmt.Errorf("GIF board count exceeds the %d-frame limit", gifMaxFrames)
			}
			boards = append(boards, diagram)
		}
		for _, children := range [][]*d2target.Diagram{diagram.Layers, diagram.Scenarios, diagram.Steps} {
			for _, child := range children {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, 1); err != nil {
		return nil, err
	}
	return boards, nil
}

type assetSessionLimits struct {
	maxDecodedPixels          int64
	maxAssets                 int
	maxCumulativeEncodedBytes int64
	maxCumulativeDecodedBytes int64
	svgImportBudget           d2scenebuild.SVGImportBudget
}

func sceneAssetOptions(ctx context.Context, inputPath string, cacheImages bool) (*d2scenebuild.AssetOptions, error) {
	return newSceneAssetOptions(ctx, inputPath, cacheImages, assetSessionLimits{
		maxDecodedPixels:          rasterMaxPixels,
		maxAssets:                 imageAssetMaxCount,
		maxCumulativeEncodedBytes: imageAssetMaxCumulativeEncodedBytes,
		maxCumulativeDecodedBytes: imageAssetMaxCumulativeDecodedBytes,
		svgImportBudget:           svgImportBudget(),
	})
}

func gifSceneAssetOptions(ctx context.Context, inputPath string, cacheImages bool, boardCount int) (*d2scenebuild.AssetOptions, error) {
	budget, err := divideSVGImportBudget(svgImportBudget(), boardCount)
	if err != nil {
		return nil, err
	}
	return newSceneAssetOptions(ctx, inputPath, cacheImages, assetSessionLimits{
		maxDecodedPixels:          gifMaxFramePixels,
		maxAssets:                 gifImageAssetMaxCount,
		maxCumulativeEncodedBytes: gifImageEncodedBytes,
		maxCumulativeDecodedBytes: gifImageDecodedBytes,
		svgImportBudget:           budget,
	})
}

func newSceneAssetOptions(ctx context.Context, inputPath string, cacheImages bool, limits assetSessionLimits) (*d2scenebuild.AssetOptions, error) {
	resolver, err := newImageAssetResolver(ctx, inputPath, cacheImages, imageasset.Limits{
		MaxFetchedBytes:           min(imageAssetMaxBytes, limits.maxCumulativeEncodedBytes),
		MaxEncodedBytes:           min(imageAssetMaxBytes, limits.maxCumulativeEncodedBytes),
		MaxDecompressedBytes:      min(imageAssetMaxBytes, limits.maxCumulativeEncodedBytes),
		MaxSVGBytes:               svgMaxBytes,
		MaxDecodedWidth:           rasterMaxDimension,
		MaxDecodedHeight:          rasterMaxDimension,
		MaxDecodedPixels:          limits.maxDecodedPixels,
		MaxAssets:                 limits.maxAssets,
		MaxCumulativeEncodedBytes: limits.maxCumulativeEncodedBytes,
		MaxCumulativeDecodedBytes: limits.maxCumulativeDecodedBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("raster export: initialize image resolver: %w", err)
	}
	return &d2scenebuild.AssetOptions{
		Resolver:        resolver,
		SVGImportLimits: svgImportLimits(),
		SVGImportBudget: limits.svgImportBudget,
	}, nil
}

func svgBundleResolver(ctx context.Context, inputPath string, cacheImages bool) (*imageasset.Resolver, error) {
	return newImageAssetResolver(ctx, inputPath, cacheImages, imageasset.Limits{
		MaxFetchedBytes:           imageAssetMaxBytes,
		MaxEncodedBytes:           imageAssetMaxBytes,
		MaxDecompressedBytes:      imageAssetMaxBytes,
		MaxSVGBytes:               imageAssetMaxBytes,
		MaxDecodedWidth:           rasterMaxDimension,
		MaxDecodedHeight:          rasterMaxDimension,
		MaxDecodedPixels:          rasterMaxPixels,
		MaxAssets:                 svgBundleMaxAssets,
		MaxCumulativeEncodedBytes: svgBundleMaxEncodedBytes,
		MaxCumulativeDecodedBytes: svgBundleMaxDecodedBytes,
	})
}

func newImageAssetResolver(ctx context.Context, inputPath string, cacheImages bool, limits imageasset.Limits) (*imageasset.Resolver, error) {
	baseDir := ""
	if inputPath != "" && inputPath != "-" {
		baseDir = filepath.Dir(inputPath)
	}
	var cache imageasset.Cache
	if cacheImages {
		assetCacheOnce.Do(func() {
			assetCache, assetCacheErr = imageasset.NewMemoryCache(svgBundleMaxAssets, assetCacheMaxBytes)
		})
		if assetCacheErr != nil {
			return nil, fmt.Errorf("initialize image cache: %w", assetCacheErr)
		}
		cache = assetCache
	}
	cacheNamespace := ""
	if cache != nil {
		cacheNamespace = assetCacheNamespace
	}
	return imageasset.New(imageasset.Options{
		BaseDir:        baseDir,
		LocalFiles:     localFilesFromContext(ctx),
		HTTPClient:     assetHTTPClient,
		NetworkPolicy:  netpolicy.FromContext(ctx),
		Cache:          cache,
		CacheNamespace: cacheNamespace,
		Limits:         limits,
	})
}

func svgImportLimits() d2svgimport.Limits {
	return d2svgimport.Limits{
		MaxBytes:              svgMaxBytes,
		MaxDecodedRasterBytes: svgMaxDecodedRasterBytes,
		MaxDepth:              svgMaxDepth,
		MaxElements:           svgMaxElements,
		MaxAttributes:         svgMaxAttributes,
		MaxAttributeBytes:     svgMaxAttributeBytes,
		MaxPathCommands:       svgMaxPathCommands,
		MaxTransformFunctions: svgMaxTransformFunctions,
		MaxUseDepth:           svgMaxUseDepth,
		MaxResources:          svgMaxResources,
	}
}

func svgImportBudget() d2scenebuild.SVGImportBudget {
	return d2scenebuild.SVGImportBudget{
		MaxSourceBytes:          svgDocumentMaxSourceBytes,
		MaxElements:             svgDocumentMaxElements,
		MaxAttributes:           svgDocumentMaxAttributes,
		MaxAttributeBytes:       svgDocumentMaxAttributeBytes,
		MaxPathCommands:         svgDocumentMaxPathCommands,
		MaxTransformFunctions:   svgDocumentMaxTransformFunctions,
		MaxDeclaredResources:    svgDocumentMaxDeclaredResources,
		MaxExpandedUseInstances: svgDocumentMaxExpandedUseInstances,
	}
}

func divideSVGImportBudget(total d2scenebuild.SVGImportBudget, boardCount int) (d2scenebuild.SVGImportBudget, error) {
	if boardCount <= 0 {
		return d2scenebuild.SVGImportBudget{}, fmt.Errorf("GIF requires a positive board count")
	}
	divided := d2scenebuild.SVGImportBudget{
		MaxSourceBytes:          total.MaxSourceBytes / boardCount,
		MaxElements:             total.MaxElements / boardCount,
		MaxAttributes:           total.MaxAttributes / boardCount,
		MaxAttributeBytes:       total.MaxAttributeBytes / boardCount,
		MaxPathCommands:         total.MaxPathCommands / boardCount,
		MaxTransformFunctions:   total.MaxTransformFunctions / boardCount,
		MaxDeclaredResources:    total.MaxDeclaredResources / boardCount,
		MaxExpandedUseInstances: total.MaxExpandedUseInstances / boardCount,
	}
	if divided.MaxSourceBytes <= 0 || divided.MaxElements <= 0 || divided.MaxAttributes <= 0 || divided.MaxAttributeBytes <= 0 ||
		divided.MaxPathCommands <= 0 || divided.MaxTransformFunctions <= 0 || divided.MaxDeclaredResources <= 0 || divided.MaxExpandedUseInstances <= 0 {
		return d2scenebuild.SVGImportBudget{}, fmt.Errorf("GIF has too many boards for the operation-wide SVG import budget")
	}
	return divided, nil
}
