package d2cli

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/d2renderers/d2scene"
)

type gifLayerOptions struct {
	boardPath []string
	duration  int
}

type gifSegment struct {
	d2animate.Frame
	count      int
	viewport   *gifViewport
	bounds     image.Rectangle
	route      []d2animate.LayerLeg
	zoomFrames int // Trailing samples rendered on the full canvas.
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

func (c *gifLayerCamera) render(ctx context.Context, session *d2raster.RenderSession, options d2raster.FrameOptions, progress float64) (*image.RGBA, error) {
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
			frameOptions.Time = 0 // The arriving board's clock starts on its slot.
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
