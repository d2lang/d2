package d2cli

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2scene"
)

// Windows Media Foundation's H.264 decoder accepts 48x48 through 4096x2304
// coded pixels and levels up to 5.1. At 30 fps the Level 5.1 macroblock rate
// also limits large frames (in particular, 3840x2160 fits).
const (
	videoMinSide                 = 48
	videoMaxWidth                = 4096
	videoMaxHeight               = 2304
	videoMaxMacroblocksPerSecond = 983040
)

type videoSize struct{ width, height int }

func parseVideoSize(raw string) (videoSize, error) {
	w, h, found := strings.Cut(strings.ToLower(raw), "x")
	width, ew := strconv.ParseUint(w, 10, 31)
	height, eh := strconv.ParseUint(h, 10, 31)
	if !found || ew != nil || eh != nil {
		return videoSize{}, fmt.Errorf("expected WIDTHxHEIGHT, for example 1920x1080")
	}
	size := videoSize{int(width), int(height)}
	if err := size.validate(); err != nil {
		return videoSize{}, err
	}
	return size, nil
}

func (s videoSize) validate() error {
	if s.width < videoMinSide || s.height < videoMinSide || s.width%2 != 0 || s.height%2 != 0 {
		return fmt.Errorf("%dx%d is invalid: dimensions must be even and at least %d pixels", s.width, s.height, videoMinSide)
	}
	if s.width > videoMaxWidth || s.height > videoMaxHeight {
		return fmt.Errorf("%dx%d exceeds the H.264 decoder limits of %d pixels wide and %d high", s.width, s.height, videoMaxWidth, videoMaxHeight)
	}
	blocks := ((s.width + 15) / 16) * ((s.height + 15) / 16)
	if blocks*videoFPS > videoMaxMacroblocksPerSecond {
		return fmt.Errorf("%dx%d at %d fps exceeds H.264 Level 5.1; try 3840x2160", s.width, s.height, videoFPS)
	}
	return nil
}

func (s videoSize) rect() image.Rectangle { return image.Rect(0, 0, s.width, s.height) }

// Measure the complete tour in D2 coordinates before choosing pixel sizes.
// This permits fitting a large source without allocating a source-size image,
// and gives trimmed clips the same scale and camera as an untrimmed export.
func (p *videoTimeline) frameSizes(boxes []d2scene.Box, scale float64, requested videoSize) (content, output image.Rectangle, err error) {
	groups := make(map[*gifViewport]d2scene.Bounds)
	var width, height float64
	for i, box := range boxes {
		w, h := box.Width, box.Height
		if v := p.segments[i].viewport; v != nil {
			// Subtract a shared origin before computing the union. Absolute
			// diagram coordinates can be far from the visible area.
			box.X -= float64(v.bounds.Min.X)
			box.Y -= float64(v.bounds.Min.Y)
			boxes[i] = box
			union := groups[v].Union(box.Bounds())
			groups[v] = union
			w, h = union.Width(), union.Height()
		}
		width, height = max(width, w), max(height, h)
	}
	if requested != (videoSize{}) {
		if err := requested.validate(); err != nil {
			return content, output, err
		}
		scale = min(float64(requested.width)/width, float64(requested.height)/height)
	}
	if !(scale > 0) || math.IsInf(scale, 0) {
		return content, output, fmt.Errorf("invalid MP4 scale %g", scale)
	}

	// Only the final canvas must be even. Independent width/height rounding
	// of the output cannot stretch the vectors inside it.
	w, h := math.Ceil(width*scale), math.Ceil(height*scale)
	if requested != (videoSize{}) {
		// Division and multiplication can overshoot the requested integer by
		// a fraction of an ulp when that side exactly fills the output.
		w, h = min(w, float64(requested.width)), min(h, float64(requested.height))
	}
	if !(w > 0 && h > 0) || w > videoMaxWidth || h > videoMaxHeight {
		return content, output, fmt.Errorf("MP4 canvas %.0fx%.0f exceeds the H.264 decoder limits of %dx%d; use --video-size=1920x1080 or reduce --scale", w, h, videoMaxWidth, videoMaxHeight)
	}
	content = image.Rect(0, 0, int(w), int(h))
	if requested == (videoSize{}) {
		// Automatic video has always framed the camera on the even canvas.
		// Rounding after the zoom shifts its center and changes every pose.
		content.Max.X += content.Dx() % 2
		content.Max.Y += content.Dy() % 2
		requested = videoSize{max(videoMinSide, content.Dx()), max(videoMinSide, content.Dy())}
		if err := requested.validate(); err != nil {
			return content, output, fmt.Errorf("MP4 canvas: %w; use --video-size=1920x1080 or reduce --scale", err)
		}
	}
	output = requested.rect()

	for i, box := range boxes {
		s := &p.segments[i]
		pw := min(content.Dx(), max(1, int(math.Ceil(box.Width*scale))))
		ph := min(content.Dy(), max(1, int(math.Ceil(box.Height*scale))))
		s.bounds = image.Rect(0, 0, pw, ph)
		if v := s.viewport; v != nil {
			union := groups[v]
			gw := min(content.Dx(), max(pw, int(math.Ceil(union.Width()*scale))))
			gh := min(content.Dy(), max(ph, int(math.Ceil(union.Height()*scale))))
			v.canvas = image.Rect(0, 0, gw, gh)
			x := min(gw-pw, max(0, int(math.Round((box.X-union.Min.X)*scale))))
			y := min(gh-ph, max(0, int(math.Round((box.Y-union.Min.Y)*scale))))
			s.bounds = s.bounds.Add(image.Pt(x, y))
		}
	}
	return content, output, nil
}
