package d2animate

import (
	"math"
	"reflect"
	"time"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
)

// TranslationTracks returns rigid translations of unchanged artwork in a
// validated board. A nil result keeps the original render-and-hold sequence.
// Matrices translate the first sample's integer geometry; compiler-provided
// motion offsets distinguish continuous trajectories from authored holds.
func TranslationTracks(board *d2target.Diagram) map[string]d2scene.Track {
	samples := board.TweenFrames
	if len(samples) < 2 || len(board.TweenFrameEndsMS) != 0 {
		return nil
	}
	base := samples[0]
	if len(samples[1].Shapes) != len(base.Shapes) {
		return nil
	}
	moving := make(map[string]d2scene.Track)
	for i := range base.Shapes {
		s := &base.Shapes[i]
		if s.MotionOffset != nil || samples[1].Shapes[i].MotionOffset != nil {
			if s.MotionLabelBox != nil || s.MotionLabelTopLeft != nil {
				return nil // These labels have independent world-space anchors.
			}
			if _, duplicate := moving[s.ID]; duplicate {
				return nil
			}
			moving[s.ID] = d2scene.Track{Property: d2scene.AnimateTransform, Duration: time.Duration(board.TweenDurationMS) * time.Millisecond}
		}
		// Annotation geometry can depend on the positions of other shapes.
		if s.Annotation != nil || s.LeavingAnnotation != nil {
			return nil
		}
	}
	if len(moving) == 0 || base.Root.Annotation != nil || base.Root.LeavingAnnotation != nil {
		return nil
	}
	for i := range base.Connections {
		c := &base.Connections[i]
		if c.IconAnimation != nil || c.Annotation != nil || c.LeavingAnnotation != nil {
			return nil
		}
	}
	for i := 0; i <= len(samples); i++ {
		pose := board
		if i < len(samples) {
			pose = samples[i]
		}
		root := pose.Root
		root.Label = base.Root.Label // The authored board name is absent from scene playback.
		if len(pose.Shapes) != len(base.Shapes) || !reflect.DeepEqual(root, base.Root) ||
			!reflect.DeepEqual(pose.Connections, base.Connections) || !reflect.DeepEqual(pose.Legend, base.Legend) ||
			!reflect.DeepEqual(pose.FontFamily, base.FontFamily) || !reflect.DeepEqual(pose.MonoFontFamily, base.MonoFontFamily) ||
			!reflect.DeepEqual(pose.Canvas, base.Canvas) {
			return nil
		}
		for j, shape := range pose.Shapes {
			ref := base.Shapes[j]
			track, translates := moving[ref.ID]
			if translates {
				if i > 0 && i < len(samples) && shape.MotionOffset == nil {
					return nil
				}
				pos := shape.RenderedPosition()
				dx, dy := pos.X-float64(ref.Pos.X), pos.Y-float64(ref.Pos.Y)
				track.Keyframes = appendTranslationKeyframe(track.Keyframes, float64(i)/float64(len(samples)), dx, dy)
				moving[ref.ID] = track
				shape.Pos, shape.MotionOffset = ref.Pos, ref.MotionOffset
			}
			if !reflect.DeepEqual(shape, ref) {
				return nil
			}
		}
	}
	for id, track := range moving {
		// Stationary pins need no track and can still have attached wires.
		if len(track.Keyframes) == 2 && track.Keyframes[0].Value.Transform == track.Keyframes[1].Value.Transform {
			delete(moving, id)
		}
	}
	if len(moving) == 0 {
		return nil
	}
	for _, c := range base.Connections {
		_, srcMoves := moving[c.Src]
		_, dstMoves := moving[c.Dst]
		if srcMoves || dstMoves {
			return nil
		}
	}
	return moving
}

// Remove intermediate samples on a straight, uniformly paced track. Eased
// motion, direction changes and stationary portions retain their breakpoints.
func appendTranslationKeyframe(keys []d2scene.Keyframe, t, x, y float64) []d2scene.Keyframe {
	if n := len(keys); n >= 2 {
		a, b := keys[n-2], keys[n-1]
		m := a.Value.Transform
		f := (b.Offset - a.Offset) / (t - a.Offset)
		if math.Abs(m.E+f*(x-m.E)-b.Value.Transform.E) < 1e-9 && math.Abs(m.F+f*(y-m.F)-b.Value.Transform.F) < 1e-9 {
			keys = keys[:n-1]
		}
	}
	return append(keys, d2scene.Keyframe{Offset: t, Value: d2scene.TransformValue(d2scene.Translate(x, y))})
}
