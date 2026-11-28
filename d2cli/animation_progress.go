package d2cli

import (
	"context"
	"image"
	"image/color"
	"image/draw"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	d2color "github.com/d2lang/d2/lib/color"
)

// Choose the ink from the opaque footer of each pose, including camera blends.
type rasterProgressInk func(footer color.RGBA) color.RGBA

func animationProgressInk(opts d2svg.RenderOpts) (rasterProgressInk, error) {
	if opts.AnimateProgress == nil || !*opts.AnimateProgress {
		return nil, nil
	}
	if opts.AnimateProgressColor == "" {
		return d2color.InverseContrast, nil
	}
	if err := d2target.ValidateAnimateProgressColor(opts.AnimateProgressColor); err != nil {
		return nil, err
	}
	paint, _ := d2color.ParseRGBA(opts.AnimateProgressColor)
	return func(color.RGBA) color.RGBA { return color.RGBA(paint) }, nil
}

func animationProgressWidth(width, elapsed, total int, last bool) int {
	if last {
		return width
	}
	return int(int64(width) * int64(elapsed) / int64(total))
}

// Static poses need a new picture only when the line reaches another
// output pixel (or a coarser step if the clip is near its pixel budget).
func videoProgressRun(tick, total, steps int) int {
	if tick == total-1 {
		return 1
	}
	step := int64(steps) * int64(tick) / int64(total)
	next := int(((step+1)*int64(total) + int64(steps) - 1) / int64(steps))
	return min(next, total-1) - tick
}

// The renderer lends this surface until emit returns. Only the bottom strip
// changes over a static pose. Restore it before releasing the raster workspace.
type videoProgressOverlay struct {
	ink    rasterProgressInk
	strip  *image.RGBA
	steps  int
	height int
}

func (p *videoProgressOverlay) emit(ctx context.Context, frame draw.Image, footer color.RGBA, start, repeats, total int, emit func(image.Image, int) error) error {
	if p.ink == nil {
		return emitVideoHold(frame, repeats, start+repeats == total, emit)
	}
	bounds := frame.Bounds()
	strip := bounds
	strip.Min.Y = strip.Max.Y - p.height
	if p.strip == nil {
		p.strip = image.NewRGBA(strip)
	}
	draw.Draw(p.strip, strip, frame, strip.Min, draw.Src)
	defer draw.Draw(frame, strip, p.strip, strip.Min, draw.Src)
	paint := image.NewUniform(p.ink(footer))
	left := strip.Min.X
	for tick, stop := start, start+repeats; tick < stop; {
		if err := ctx.Err(); err != nil {
			return err
		}
		extent := animationProgressWidth(bounds.Dx(), tick, total, tick == total-1)
		run := videoProgressRun(tick, total, p.steps)
		right := bounds.Min.X + extent
		draw.Draw(frame, image.Rect(left, strip.Min.Y, right, strip.Max.Y), paint, image.Point{}, draw.Src)
		left = right
		run = min(run, stop-tick)
		if err := emit(frame, run); err != nil {
			return err
		}
		tick += run
	}
	return nil
}
