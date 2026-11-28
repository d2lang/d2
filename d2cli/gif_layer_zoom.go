package d2cli

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
)

type gifLayerOptions struct {
	boardPath []string
	duration  int
}

type gifSegment struct {
	d2animate.Frame
	rasterPlacement
	// Owning board for a stream that continues across tween samples.
	iconBoard *d2target.Diagram
	count     int
	route     []d2animate.LayerLeg
	zoom      []gifZoomSample // Trailing samples rendered on the full canvas.
}

type gifZoomSample struct {
	leg      int
	progress float64
}

// Zooms can revisit exact boards on either side of a branch. Keep two
// neighboring scenes; moving to the next leg usually reuses its source.
type gifSceneCache struct {
	keys [2]int // One-based timeline indices; zero means empty.
	docs [2]*d2scene.Document
}

func (c *gifSceneCache) touch(index int) bool {
	key := index + 1
	if c.keys[0] == key {
		return true
	}
	hit := c.keys[1] == key
	c.keys[0], c.keys[1] = key, c.keys[0]
	c.docs[0], c.docs[1] = c.docs[1], c.docs[0]
	if !hit {
		c.docs[0] = nil
	}
	return hit
}

// Measure scene construction separately from frame painting. The bounds
// prepass and the later visits to intermediate boards share the same cache.
func gifZoomSceneBuildCount(timeline []gifSegment, indices map[*d2target.Diagram]int) int {
	var cache gifSceneCache
	count := 0
	visit := func(index int) {
		if !cache.touch(index) {
			count++
		}
	}
	for i := range timeline {
		visit(i)
	}
	for i, segment := range timeline {
		visit(i)
		previous := -1
		for _, sample := range segment.zoom {
			if sample.leg == previous {
				continue
			}
			leg := segment.route[sample.leg]
			visit(indices[leg.From])
			visit(indices[leg.To])
			previous = sample.leg
		}
	}
	return count
}

type rasterPlacement struct {
	viewport *gifViewport
	bounds   image.Rectangle
}

// Related boards share a world origin. Include decorations outside the layout
// bounds before centering each group on the final animation canvas.
func (s *rasterPlacement) measure(bounds image.Rectangle, document *d2scene.Document, scale float64) image.Rectangle {
	s.bounds = bounds
	if v := s.viewport; v != nil {
		offset := image.Pt(
			int(math.Round((document.ViewBox.X-float64(v.bounds.Min.X))*scale)),
			int(math.Round((document.ViewBox.Y-float64(v.bounds.Min.Y))*scale)),
		)
		s.bounds = bounds.Add(offset)
		v.canvas = v.canvas.Union(s.bounds)
		return v.canvas
	}
	return bounds
}

func (s rasterPlacement) offset(canvas image.Rectangle) image.Point {
	group := s.bounds
	if s.viewport != nil {
		group = s.viewport.canvas
	}
	center := image.Pt((canvas.Dx()-group.Dx())/2, (canvas.Dy()-group.Dy())/2)
	return center.Add(s.bounds.Min.Sub(group.Min))
}

type gifLayer struct {
	document  *d2scene.Document
	placement d2scene.Matrix // Board coordinates to pixels on the final GIF canvas.
}

func newGIFLayer(document *d2scene.Document, offset image.Point) gifLayer {
	box := document.ViewBox
	scale := math.Min(document.LogicalWidth/box.Width, document.LogicalHeight/box.Height)
	dx, dy := float64(offset.X), float64(offset.Y)
	if document.ViewportAlign == d2scene.ViewportAlignXMidYMid {
		dx += (document.LogicalWidth - box.Width*scale) / 2
		dy += (document.LogicalHeight - box.Height*scale) / 2
	}
	return gifLayer{document, d2scene.Matrix{A: scale, D: scale, E: dx - box.X*scale, F: dy - box.Y*scale}}
}

type gifLayerCamera struct {
	canvas        image.Rectangle
	zoom          d2animate.LayerZoom
	parent, child gifLayer
}

func newGIFLayerCamera(canvas image.Rectangle, zoom d2animate.LayerZoom, from *d2scene.Document, fromOffset image.Point, to *d2scene.Document, toOffset image.Point) *gifLayerCamera {
	parent, child := newGIFLayer(from, fromOffset), newGIFLayer(to, toOffset)
	if !zoom.Inward {
		parent, child = child, parent
	}
	// GIF independently centers boards that do not share layout coordinates.
	// Measure the portal where the enclosing board's shape actually appears.
	focus := *zoom.Focus
	p := parent.placement
	focus.Pos.X = int(math.Round(float64(focus.Pos.X)*p.A + p.E))
	focus.Pos.Y = int(math.Round(float64(focus.Pos.Y)*p.D + p.F))
	focus.Width = max(1, int(math.Round(float64(focus.Width)*p.A)))
	focus.Height = max(1, int(math.Round(float64(focus.Height)*p.D)))
	zoom.Focus = &focus
	return &gifLayerCamera{canvas, zoom, parent, child}
}

func (c *gifLayerCamera) render(ctx context.Context, session *d2raster.RenderSession, options d2raster.FrameOptions, progress float64, arrivalTime time.Duration) (*image.RGBA, error) {
	// GIF and storyboard consumers may retain this image after the next sample.
	output := image.NewRGBA(c.canvas)
	var workspace d2raster.RenderWorkspace
	if err := c.renderInto(ctx, session, &workspace, options, progress, arrivalTime, output); err != nil {
		return nil, err
	}
	return output, nil
}

// Video consumes each image synchronously and can reuse both the composition
// canvas and the raster workspace across samples, boards and camera legs.
func (c *gifLayerCamera) renderInto(ctx context.Context, session *d2raster.RenderSession, workspace *d2raster.RenderWorkspace, options d2raster.FrameOptions, progress float64, arrivalTime time.Duration, output *image.RGBA) error {
	matte := options.Background
	if matte == nil {
		matte = color.White
	}
	draw.Draw(output, c.canvas, image.NewUniform(matte), image.Point{}, draw.Src)
	parent, child := c.zoom.Poses(c.canvas, progress)
	for _, item := range []struct {
		layer    gifLayer
		pose     d2animate.LayerPose
		arriving bool
	}{{c.parent, parent, !c.zoom.Inward}, {c.child, child, c.zoom.Inward}} {
		layer, pose := item.layer, item.pose
		opacity := uint8(math.Round(pose.Opacity * 255))
		if opacity == 0 {
			continue
		}
		// Rasterize the transformed vectors at the GIF resolution so labels
		// and outlines stay sharp as an off-center shape fills the frame.
		p := layer.placement
		scale := pose.Scale * p.A
		width, height := float64(c.canvas.Dx()), float64(c.canvas.Dy())
		document := *layer.document
		document.LogicalWidth, document.LogicalHeight = width, height
		document.ViewBox = d2scene.Box{
			X: -(pose.Scale*p.E + pose.DX) / scale, Y: -(pose.Scale*p.F + pose.DY) / scale,
			Width: width / scale, Height: height / scale,
		}
		document.ViewportFit = d2scene.ViewportStretch
		frameOptions := options
		frameOptions.Background = nil
		if item.arriving {
			frameOptions.Time = arrivalTime
		}
		err := workspace.Render(ctx, session, &document, frameOptions, func(frame *image.NRGBA) error {
			return compositeLayer(ctx, output, frame, opacity)
		})
		if err != nil {
			return fmt.Errorf("layer zoom: %w", err)
		}
	}
	return nil
}

// compositeLayer is draw.Over with a constant alpha mask, for the native
// rasterizer's straight-alpha source and a premultiplied composition canvas.
// Match image/draw's 16-bit arithmetic, including both integer divisions.
func compositeLayer(ctx context.Context, dst *image.RGBA, src *image.NRGBA, opacity uint8) error {
	if opacity == 0 {
		return nil
	}
	const full = 0xffff
	mask := uint32(opacity) * 0x101
	width := min(dst.Rect.Dx(), src.Rect.Dx()) * 4
	height := min(dst.Rect.Dy(), src.Rect.Dy())
	for y := range height {
		if err := ctx.Err(); err != nil {
			return err
		}
		dest := dst.Pix[y*dst.Stride:][:width]
		source := src.Pix[y*src.Stride:][:width]
		for x := 0; x < width; x += 4 {
			s, d := source[x:x+4:x+4], dest[x:x+4:x+4]
			if s[3] == 0 {
				continue
			}
			if s[3] == 255 && opacity == 255 {
				copy(d, s)
				continue
			}
			alpha := uint32(s[3]) * 0x101
			inverse := (full - alpha*mask/full) * 0x101
			for channel := range 3 {
				premultiplied := uint32(s[channel]) * alpha / 0xff
				d[channel] = uint8((uint32(d[channel])*inverse + premultiplied*mask) / full >> 8)
			}
			d[3] = uint8((uint32(d[3])*inverse + alpha*mask) / full >> 8)
		}
	}
	return nil
}
