package d2cli

import (
	"context"
	"image"
	"image/color"
)

// Delays are GIF centiseconds. Their sum, including caption and camera holds,
// is the playback clock, even when adjacent frames have very different delays.
type gifProgressOverlay struct {
	ink                   *image.Uniform
	delays                []int
	index, elapsed, total int
}

func (p *gifProgressOverlay) start(delays []int) {
	if p.ink == nil {
		return
	}
	p.delays = delays
	for _, delay := range delays {
		p.total += delay
	}
}

func (p *gifProgressOverlay) paint(ctx context.Context, frame *image.Paletted) error {
	if p.ink == nil {
		return nil
	}
	bounds := frame.Bounds()
	extent := animationProgressWidth(bounds.Dx(), p.elapsed, p.total, p.index == len(p.delays)-1)
	p.elapsed += p.delays[p.index]
	p.index++
	if extent == 0 {
		return nil
	}
	index, err := gifProgressColorIndex(ctx, frame, p.ink.C)
	if err != nil {
		return err
	}
	strip := animationProgressStrip(bounds)
	for y := strip.Min.Y; y < strip.Max.Y; y++ {
		start := frame.PixOffset(bounds.Min.X, y)
		for i := range extent {
			frame.Pix[start+i] = uint8(index)
		}
	}
	return nil
}

// Reserve an exact overlay color after quantization so a narrow accent cannot
// disappear into a grayscale or photographic scene's palette.
func gifProgressColorIndex(ctx context.Context, frame *image.Paletted, ink color.Color) (int, error) {
	palette := frame.Palette
	index := palette.Index(ink)
	if color.RGBAModel.Convert(palette[index]) == color.RGBAModel.Convert(ink) {
		return index, nil
	}
	if len(palette) < 256 {
		frame.Palette = append(palette, ink)
		return len(palette), nil
	}
	// Keep the opaque white used by the GIF canvas. Reassign the least-used
	// other slot and send its old pixels to their nearest remaining color.
	var counts [256]int
	for y := frame.Rect.Min.Y; y < frame.Rect.Max.Y; y++ {
		if y%256 == 0 {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		row := frame.PixOffset(frame.Rect.Min.X, y)
		for _, pixel := range frame.Pix[row : row+frame.Rect.Dx()] {
			counts[pixel]++
		}
	}
	white := palette.Index(color.White)
	index = (white + 1) % 256
	for i, count := range counts {
		if i != white && count < counts[index] {
			index = i
		}
	}
	old := palette[index]
	palette[index] = palette[white]
	if counts[index] != 0 {
		replacement := palette.Index(old)
		if replacement == index {
			replacement = white
		}
		for y := frame.Rect.Min.Y; y < frame.Rect.Max.Y; y++ {
			if y%256 == 0 {
				if err := ctx.Err(); err != nil {
					return 0, err
				}
			}
			row := frame.PixOffset(frame.Rect.Min.X, y)
			for i, pixel := range frame.Pix[row : row+frame.Rect.Dx()] {
				if pixel == uint8(index) {
					frame.Pix[row+i] = uint8(replacement)
				}
			}
		}
	}
	palette[index] = ink
	return index, nil
}
