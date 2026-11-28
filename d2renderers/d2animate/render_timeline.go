package d2animate

import (
	"math"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

// RenderSVGFrames renders the artwork for WrapTimeline. A straight translation
// of otherwise identical artwork needs only the ends of the line. Check the
// emitted SVG too, since bundling, appendices and custom renderers can change it.
func RenderSVGFrames(root *d2target.Diagram, boardPath []string, frames []Frame, opts d2svg.RenderOpts, zoomMS int, render func(*d2target.Diagram) ([]byte, error)) ([]Frame, [][]byte, error) {
	art := make([][]byte, len(frames))
	draw := func(i int) ([]byte, error) {
		if art[i] == nil {
			b, err := render(frames[i].Picture())
			if err != nil {
				return nil, err
			}
			art[i] = b
		}
		return art[i], nil
	}
	if opts.MasterID != "" && d2svg.UsesDefaultStylesheet() && (opts.Sketch == nil || !*opts.Sketch) {
		indices, total := straightMotionFrames(frames)
		if indices != nil {
			nav := NewLayerNavigation(root, boardPath)
			if !nav.HasLayerPortal() && len(layerLinks(nav, frames, zoomMS)) == 0 {
				projected := make([]Frame, len(indices))
				pictures := make([][]byte, len(indices))
				for j, i := range indices {
					var err error
					pictures[j], err = draw(i)
					if err != nil {
						return nil, nil, err
					}
					projected[j] = frames[i]
					if j+1 < len(indices) {
						for k := i + 1; k < indices[j+1]; k++ {
							projected[j].DurationMS += frames[k].DurationMS
						}
					}
				}
				// Only discard intermediate artwork when every changing part has a
				// native track. Unknown SVG, changing backgrounds or discrete gaps
				// require the original frame schedule.
				if native, _, discrete := nativeMotion(projected, pictures, opts.MasterID, total); native != nil && !discrete {
					return projected, pictures, nil
				}
			}
		}
	}
	for i := range frames {
		if _, err := draw(i); err != nil {
			return nil, nil, err
		}
	}
	return frames, art, nil
}

// Retain all authored holds and both ends of each sampled run. The last
// sample matters when the timeline divides its final period with the endpoint.
func straightMotionFrames(frames []Frame) (indices []int, total int) {
	start := 0
	for i, f := range frames {
		if f.DurationMS <= 0 || f.DurationMS > math.MaxInt-total || f.picture != nil {
			return nil, 0
		}
		total += f.DurationMS
		if f.TweenSample {
			continue
		}
		n := i - start
		straight := n > 2 && n == len(f.Diagram.TweenFrames)
		if straight {
			tracks := TranslationTracks(f.Diagram)
			straight = len(tracks) > 0
			for _, track := range tracks {
				if len(track.Keyframes) != 2 {
					straight = false
					break
				}
			}
		}
		if straight {
			left, right := f.Diagram.BoundingBox()
			for j, sampled := range f.Diagram.TweenFrames {
				// Each frame paints its own background. Two objects crossing
				// can temporarily shrink that rectangle even on straight paths.
				tl, br := sampled.BoundingBox()
				if frames[start+j].Diagram != sampled || tl != left || br != right {
					straight = false
					break
				}
			}
		}
		if straight {
			indices = append(indices, start, i-1, i)
		} else {
			for j := start; j <= i; j++ {
				indices = append(indices, j)
			}
		}
		start = i + 1
	}
	if start != len(frames) || len(indices) >= len(frames) {
		return nil, 0
	}
	return indices, total
}
