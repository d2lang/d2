package d2cli

import (
	"context"
	"image"
	"image/color"
	"slices"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/lib/xgif"
)

// Sparse tween poses can hold for seconds. Add intermediate progress
// pictures where there is room, sharing available slots proportionally
// across holds. Authored pictures, their boundaries and the GIF limits
// are preserved even when no optional updates fit.
func (s *gifRenderSummary) splitProgressHolds(ctx context.Context, poses []*image.Paletted) ([]*image.Paletted, error) {
	extraFor := func(delay int) int64 { return int64(max(0, delay/3-1)) }
	var wanted int64
	for _, delay := range s.delays {
		wanted += extraFor(delay)
	}
	pixels := int64(s.maxWidth) * int64(s.maxHeight+s.footerHeight)
	limit := min(int64(gifMaxFrames), gifMaxNormalizedPixels/pixels)
	// A progress strip can advance by at most one new position per pixel.
	extra := min(wanted, int64(s.maxWidth), limit-int64(len(poses)))
	if extra <= 0 {
		return poses, nil
	}

	capacity := len(poses) + int(extra)
	images := make([]*image.Paletted, 0, capacity)
	delays := make([]int, 0, capacity)
	var mattes []rasterMattes
	if s.mattes != nil {
		mattes = make([]rasterMattes, 0, capacity)
	}
	var offsets []image.Point
	if s.offsets != nil {
		offsets = make([]image.Point, 0, capacity)
	}
	var requested, assigned int64
	for i, pose := range poses {
		requested += extraFor(s.delays[i])
		next := requested * extra / wanted
		count := 1 + int(next-assigned)
		assigned = next
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for range count {
			images = append(images, pose)
			if s.mattes != nil {
				mattes = append(mattes, s.mattes[i])
			}
		}
		delays = xgif.AppendFrameDelays(delays, s.delays[i], count)
		if s.offsets != nil {
			for range count {
				offsets = append(offsets, s.offsets[i])
			}
		}
	}
	s.delays, s.offsets, s.mattes, s.totalFrames = delays, offsets, mattes, len(images)
	return images, nil
}

// Delays are GIF centiseconds. Their sum, including caption and camera holds,
// is the playback clock, even when adjacent frames have very different delays.
type gifProgressOverlay struct {
	ink                                *image.Uniform
	plan                               gifRenderSummary
	poses                              []*image.Paletted
	streamCanvas                       *image.Paletted
	boardIndex, footerIndex, lastWidth int
	index, elapsed, total              int
}

func (p *gifProgressOverlay) start(plan gifRenderSummary, poses []*image.Paletted) {
	if p.ink == nil {
		return
	}
	p.plan, p.poses = plan, poses
	p.lastWidth = -1
	for _, delay := range plan.delays {
		p.total += delay
	}
	if poses == nil {
		p.streamCanvas = image.NewPaletted(image.Rect(0, 0, plan.maxWidth, plan.maxHeight+plan.footerHeight), nil)
	}
}

// The single-board encoder consumes pixels synchronously. Append the footer
// using one reusable indexed buffer, without re-quantizing the board.
func (p *gifProgressOverlay) extendSingleBoard(src *image.Paletted) *image.Paletted {
	dst := p.streamCanvas
	if dst == nil {
		return src
	}
	p.lastWidth = -1
	dst.Palette = src.Palette
	if src.Stride == dst.Stride {
		copy(dst.Pix, src.Pix[:src.Rect.Dy()*src.Stride])
	} else {
		for y := range src.Rect.Dy() {
			copy(dst.Pix[y*dst.Stride:(y+1)*dst.Stride], src.Pix[y*src.Stride:])
		}
	}
	return dst
}

func (p *gifProgressOverlay) paint(ctx context.Context, frame *image.Paletted) error {
	if p.ink == nil {
		return nil
	}
	bounds := frame.Bounds()
	// The encoder lends its normalized canvas for this call only. A repeated
	// source pose at the same offset carries the previous overlay forward.
	newPose := p.lastWidth < 0
	if p.poses != nil && p.index > 0 {
		newPose = p.poses[p.index] != p.poses[p.index-1] || p.plan.offsets != nil && p.plan.offsets[p.index] != p.plan.offsets[p.index-1]
	}
	if newPose {
		if err := p.prepareCanvas(ctx, frame); err != nil {
			return err
		}
		p.lastWidth = 0
	}
	extent := animationProgressWidth(bounds.Dx(), p.elapsed, p.total, p.index == len(p.plan.delays)-1)
	p.elapsed += p.plan.delays[p.index]
	p.index++
	if extent == p.lastWidth {
		return nil
	}
	index, err := gifProgressColorIndex(ctx, frame, p.ink.C, p.boardIndex, p.footerIndex)
	if err != nil {
		return err
	}
	line := image.Rect(p.lastWidth, p.plan.maxHeight+d2animate.ProgressGap, extent, bounds.Max.Y)
	p.lastWidth = extent
	return fillGIFMargin(ctx, frame, line, uint8(index))
}

// Extend the board or camera matte across its footer and any margins. Keep
// the old board placement, and prepare only once across repeated static poses.
func (p *gifProgressOverlay) prepareCanvas(ctx context.Context, frame *image.Paletted) error {
	canvas := image.Rect(0, 0, p.plan.maxWidth, p.plan.maxHeight)
	matte := p.plan.mattes[p.index]
	boardIndex, err := gifProgressColorIndex(ctx, frame, matte.board)
	if err != nil {
		return err
	}
	p.boardIndex, p.footerIndex = boardIndex, boardIndex
	if p.poses != nil && matte.board != (color.RGBA{255, 255, 255, 255}) {
		pose := p.poses[p.index].Rect
		board := pose.Sub(pose.Min).Add(p.plan.offsets[p.index])
		if board != canvas {
			for _, margin := range [...]image.Rectangle{
				image.Rect(0, 0, canvas.Max.X, board.Min.Y),
				image.Rect(0, board.Min.Y, board.Min.X, board.Max.Y),
				image.Rect(board.Max.X, board.Min.Y, canvas.Max.X, board.Max.Y),
				image.Rect(0, board.Max.Y, canvas.Max.X, canvas.Max.Y),
			} {
				if err := fillGIFMargin(ctx, frame, margin, uint8(boardIndex)); err != nil {
					return err
				}
			}
		}
	}
	if matte.footer != matte.board {
		p.footerIndex, err = gifProgressColorIndex(ctx, frame, matte.footer, boardIndex)
		if err != nil {
			return err
		}
	}
	return fillGIFMargin(ctx, frame, image.Rect(0, canvas.Max.Y, canvas.Max.X, frame.Rect.Max.Y), uint8(p.footerIndex))
}

func fillGIFMargin(ctx context.Context, frame *image.Paletted, rect image.Rectangle, index uint8) error {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		if y%256 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		at := frame.PixOffset(rect.Min.X, y)
		row := frame.Pix[at : at+rect.Dx()]
		for i := range row {
			row[i] = index
		}
	}
	return nil
}

// Reserve an exact overlay color after quantization so a narrow accent cannot
// disappear into a grayscale or photographic scene's palette.
func gifProgressColorIndex(ctx context.Context, frame *image.Paletted, ink color.Color, protected ...int) (int, error) {
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
	for slices.Contains(protected, index) || index == white {
		index = (index + 1) % 256
	}
	for i, count := range counts {
		if i != white && !slices.Contains(protected, i) && count < counts[index] {
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
