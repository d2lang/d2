package d2cli

import (
	"context"
	"image"
	"image/draw"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2svg"
	d2color "github.com/d2lang/d2/lib/color"
)

// Raster formats draw in delivery pixels, after camera and canvas placement.
func animationProgressStrip(bounds image.Rectangle) image.Rectangle {
	bounds.Min.Y = max(bounds.Min.Y, bounds.Max.Y-d2animate.ProgressHeight)
	return bounds
}

func animationProgressInk(opts d2svg.RenderOpts) (*image.Uniform, error) {
	if opts.AnimateProgress == nil || !*opts.AnimateProgress {
		return nil, nil
	}
	paint, err := d2animate.ResolveProgressColor(opts.ThemeID, opts.ThemeOverrides, opts.AnimateProgressColor)
	if err != nil {
		return nil, err
	}
	ink, _ := d2color.ParseRGBA(paint)
	return image.NewUniform(ink), nil
}

func animationProgressWidth(width, elapsed, total int, last bool) int {
	if last {
		return width
	}
	return int(int64(width) * int64(elapsed) / int64(total))
}

// Successive ticks may land on the same delivery pixel. Preserve those holds
// so that a long static clip does not send an identical canvas at every tick.
func videoProgressRun(tick, total, width int) (extent, repeats int) {
	extent = animationProgressWidth(width, tick, total, tick == total-1)
	if tick == total-1 {
		return extent, 1
	}
	next := int((int64(extent+1)*int64(total) + int64(width) - 1) / int64(width))
	return extent, min(next, total-1) - tick
}

// The renderer lends this surface until emit returns. Only the bottom strip
// changes over a static pose. Restore it before releasing the raster workspace.
type videoProgressOverlay struct {
	ink   *image.Uniform
	strip *image.RGBA
}

func (p *videoProgressOverlay) emit(ctx context.Context, frame draw.Image, start, repeats, total int, emit func(image.Image, int) error) error {
	if p.ink == nil {
		return emit(frame, repeats)
	}
	bounds := frame.Bounds()
	strip := animationProgressStrip(bounds)
	if p.strip == nil {
		p.strip = image.NewRGBA(strip)
	}
	draw.Draw(p.strip, strip, frame, strip.Min, draw.Src)
	defer draw.Draw(frame, strip, p.strip, strip.Min, draw.Src)
	left := strip.Min.X
	for tick, stop := start, start+repeats; tick < stop; {
		if err := ctx.Err(); err != nil {
			return err
		}
		extent, run := videoProgressRun(tick, total, bounds.Dx())
		right := bounds.Min.X + extent
		draw.Draw(frame, image.Rect(left, strip.Min.Y, right, strip.Max.Y), p.ink, image.Point{}, draw.Src)
		left = right
		run = min(run, stop-tick)
		if err := emit(frame, run); err != nil {
			return err
		}
		tick += run
	}
	return nil
}
