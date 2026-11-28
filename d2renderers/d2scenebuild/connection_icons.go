package d2scenebuild

import (
	"fmt"
	"math"
	"time"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func (b *builder) addMovingConnectionIcons(group *d2scene.Node, c d2target.Connection, fill d2scene.Paint) error {
	add := func(i int, pt geo.Point, tracks []d2scene.Track, showAt time.Duration) error {
		icon, err := b.connectionIcon(c, fmt.Sprintf("%s:icon:%d", c.ID, i), pt, fill)
		if err != nil {
			return err
		}
		icon.Animations = tracks
		if showAt > 0 {
			// Before the first launch, delayed motion tracks would otherwise
			// hold their initial frame visibly at the source.
			gate := d2scene.NewNode(nil)
			gate.Animations = []d2scene.Track{{Property: d2scene.AnimateOpacity, Duration: showAt, Keyframes: []d2scene.Keyframe{
				{Offset: 0, Value: d2scene.NumberValue(0), Easing: d2scene.Easing{Kind: d2scene.EaseStepEnd}},
				{Offset: 1, Value: d2scene.NumberValue(1)},
			}}}
			gate.Children = []*d2scene.Node{icon}
			icon = gate
		}
		group.Children = append(group.Children, icon)
		return nil
	}
	if c.IconPositions != nil {
		for i, pt := range c.IconPositions {
			if err := add(i, pt, nil, 0); err != nil {
				return err
			}
		}
		return nil
	}
	path := c.IconMotionPath(b.idToShape[c.Src].RenderedStrokeWidth(), b.idToShape[c.Dst].RenderedStrokeWidth())
	if path.Length() <= 0 {
		return nil
	}
	travel := path.TransitDuration(c.IconAnimation)
	period := path.LaunchPeriod(c.IconAnimation)
	first, window, start := 0., math.Inf(1), time.Duration(0)
	carried := 0
	if s := c.IconSchedule; s != nil {
		travel, period, first, window = s.Transit, s.Period, s.First, float64(s.DurationMS)/1000
		start = time.Duration(s.StartMS) * time.Millisecond
		var finish []d2scene.Track
		var transit float64
		for i, trip := range s.Carried {
			if trip.Transit != transit {
				transit = trip.Transit
				finish = iconTracks(path, transit, transit, false)
			}
			phase := min(finish[0].Duration-1, time.Duration(math.Round(trip.Progress*float64(finish[0].Duration))))
			if err := add(i, geo.Point{}, startIconTracks(finish, start, phase), 0); err != nil {
				return err
			}
		}
		carried = len(s.Carried)
	}
	if math.IsInf(period, 1) || !(period > 0) || first >= window {
		return nil
	}
	slots := min(d2target.MaxMovingIconsPerConnection, max(1, int(math.Ceil(travel/period))))
	loop := period * float64(slots)
	tracks := iconTracks(path, travel, loop, loop < time.Duration(math.MaxInt64).Seconds())
	for i := 0; i < slots; i++ {
		offset := float64(i) * period
		if c.IconSchedule != nil {
			depart := first + offset
			if depart >= window {
				break
			}
			delay := start + time.Duration(math.Round(depart*float64(time.Second)))
			if err := add(carried+i, geo.Point{}, startIconTracks(tracks, delay, 0), delay); err != nil {
				return err
			}
		} else {
			// A standalone stream starts in steady state with evenly spaced copies.
			phase := min(tracks[0].Duration-1, time.Duration(math.Round(offset*float64(time.Second))))
			if err := add(i, geo.Point{}, startIconTracks(tracks, 0, phase), 0); err != nil {
				return err
			}
		}
	}
	return nil
}

// A train shares its route keyframes across all copies. Its period controls
// departures; its travel time determines where a copy is on this picture.
func iconTracks(path d2target.IconMotionPath, travel, loop float64, repeat bool) []d2scene.Track {
	cycle := iconDuration(loop)
	active := min(1., travel/cycle.Seconds())
	positions := make([]d2scene.Keyframe, len(path.Points))
	for i, p := range path.Points {
		positions[i] = d2scene.Keyframe{Offset: path.Distances[i] / path.Length() * active, Value: d2scene.TransformValue(d2scene.Translate(p.X, p.Y))}
	}
	tracks := []d2scene.Track{{Property: d2scene.AnimateTransform, Duration: cycle, Repeat: repeat, Keyframes: positions}}
	if active < 1 || !repeat {
		tracks = append(tracks, d2scene.Track{Property: d2scene.AnimateOpacity, Duration: cycle, Repeat: repeat, Keyframes: []d2scene.Keyframe{
			{Offset: 0, Value: d2scene.NumberValue(1), Easing: d2scene.Easing{Kind: d2scene.EaseStepEnd}},
			{Offset: active, Value: d2scene.NumberValue(0)},
		}})
	}
	return tracks
}

func startIconTracks(shared []d2scene.Track, delay, phase time.Duration) []d2scene.Track {
	tracks := make([]d2scene.Track, len(shared))
	copy(tracks, shared)
	for i := range tracks {
		tracks[i].Delay, tracks[i].Phase = delay, phase
	}
	return tracks
}

// time.Duration represents every timestamp the rasterizer can sample. A
// longer authored period has only its initial departure in that window.
func iconDuration(seconds float64) time.Duration {
	if seconds >= time.Duration(math.MaxInt64).Seconds() {
		return time.Duration(math.MaxInt64)
	}
	return max(time.Nanosecond, time.Duration(math.Round(seconds*float64(time.Second))))
}
