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
	count    int
	viewport *gifViewport
	bounds   image.Rectangle
	// Owning board for an icon stream that can span several step or tween pictures.
	iconBoard *d2target.Diagram
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

// Related boards share a world origin. Center their entire group as a unit.
func (s gifSegment) offset(canvas image.Rectangle) image.Point {
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
	output := image.NewRGBA(c.canvas)
	draw.Draw(output, c.canvas, image.White, image.Point{}, draw.Src)
	parent, child := c.zoom.Poses(c.canvas, progress)
	for _, item := range []struct {
		layer    gifLayer
		pose     d2animate.LayerPose
		arriving bool
	}{{c.parent, parent, !c.zoom.Inward}, {c.child, child, c.zoom.Inward}} {
		layer, pose := item.layer, item.pose
		if pose.Opacity <= 0 {
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
		frame, err := session.Render(ctx, &document, frameOptions)
		if err != nil {
			return nil, fmt.Errorf("layer zoom: %w", err)
		}
		mask := image.NewUniform(color.Alpha{A: uint8(math.Round(pose.Opacity * 255))})
		draw.DrawMask(output, c.canvas, frame, image.Point{}, mask, image.Point{}, draw.Over)
	}
	return output, nil
}
