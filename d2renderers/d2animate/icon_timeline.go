package d2animate

import (
	"fmt"
	"math"
	"slices"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"
)

// IconTimeline can seek to any time within an authored board. Entry states
// retain only the copies still in flight, independently of the length of a hold.
type IconTimeline struct {
	boards map[*d2target.Diagram]*iconBoard
}

type iconBoard struct {
	streams  map[string]*iconStream
	duration float64 // seconds
	active   bool
}

type iconStream struct {
	path          d2target.IconMotionPath
	animation     *d2target.ConnectionIconAnimation
	period, first float64 // seconds between launches, and first launch in this board
	carried       []iconFlight
}

// A live copy keeps its progress and the timing that was active at departure.
// Untimed copies retain their diagram speed when a step changes the route.
type iconFlight struct {
	progress float64
	launched *d2target.ConnectionIconAnimation
}

func (f iconFlight) remaining(path d2target.IconMotionPath) float64 {
	return (1 - f.progress) * path.TransitDuration(f.launched)
}

func NewIconTimeline(root *d2target.Diagram, intervalMS int) (*IconTimeline, error) {
	if intervalMS <= 0 {
		return nil, fmt.Errorf("animation interval must be positive")
	}
	if err := d2target.ValidateRenderTarget(root); err != nil {
		return nil, err
	}
	return newIconTimeline(root, intervalMS), nil
}

func newIconTimeline(root *d2target.Diagram, intervalMS int) *IconTimeline {
	var scenarioBases map[*d2target.Diagram]*d2target.Diagram
	var timeline IconTimeline
	var last *d2target.Diagram
	var visit func(board, predecessor *d2target.Diagram)
	visit = func(board, predecessor *d2target.Diagram) {
		if !board.IsFolderOnly {
			// A nested board can interrupt the sequence before an inheriting step.
			var prev *iconBoard
			continuing := predecessor != nil && predecessor == last
			if continuing {
				prev = timeline.boards[predecessor]
			}
			b := makeIconBoard(board, intervalMS, prev, continuing, scenarioBases[board] != nil)
			if b != nil {
				if timeline.boards == nil {
					timeline.boards = make(map[*d2target.Diagram]*iconBoard)
				}
				timeline.boards[board] = b
			}
			last = board
		}
		for _, layer := range board.Layers {
			visit(layer, nil)
		}
		if len(board.Scenarios) > 0 && scenarioBases == nil {
			scenarioBases = root.ScenarioPredecessors()
		}
		for _, scenario := range board.Scenarios {
			visit(scenario, scenarioBases[scenario])
		}
		prev := board
		for _, step := range board.Steps {
			visit(step, prev)
			prev = step
		}
	}
	visit(root, nil)
	if len(timeline.boards) == 0 {
		return nil
	}
	return &timeline
}

func iconStrokeWidths(board *d2target.Diagram) map[string]float64 {
	widths := make(map[string]float64, len(board.Shapes))
	for i := range board.Shapes {
		s := &board.Shapes[i]
		widths[s.ID] = s.RenderedStrokeWidth()
	}
	return widths
}

func makeIconBoard(board *d2target.Diagram, intervalMS int, prev *iconBoard, continuing, scenario bool) *iconBoard {
	var b *iconBoard
	for i, path := range board.ConnectionIconRoutes() {
		e := &board.Connections[i]
		if b == nil {
			b = &iconBoard{streams: make(map[string]*iconStream), duration: float64(max(intervalMS, board.ReadingMS(), board.TweenDurationMS)) / 1000}
		}
		s := &iconStream{path: path, animation: e.IconAnimation, period: path.LaunchPeriod(e.IconAnimation)}
		prevID := e.ID
		if e.IconAnimation.PreviousID != nil {
			prevID = *e.IconAnimation.PreviousID
		}
		// The compiler records a scenario connection's origin only when its
		// board has an eligible tween. Other streams start independently.
		linked := continuing && (!scenario || e.IconAnimation.PreviousID != nil)
		if linked && prev != nil && prevID != "" && prev.streams[prevID] != nil {
			old := prev.streams[prevID]
			// At a boundary the next board owns the departure at that exact instant.
			s.carried = old.flights(prev.duration, false)
			if !math.IsInf(s.period, 1) && !math.IsInf(old.period, 1) {
				s.first = old.nextLaunch(prev.duration) / old.period * s.period
			}
			s.admitDepartures()
		} else if !linked {
			// The opening board shows the stream already running.
			transit := path.TransitDuration(s.animation)
			for n := 1; n < d2target.MaxMovingIconsPerConnection; n++ {
				age := float64(n) * s.period
				if age >= transit {
					break
				}
				s.carried = append(s.carried, iconFlight{progress: age / transit, launched: s.animation})
			}
		}
		b.streams[e.ID] = s
		b.active = b.active || len(s.carried) > 0 || !math.IsInf(s.period, 1)
	}
	return b
}

// Only the initial departures can crowd the wire: thereafter new copies
// replace their own arrivals and the number of carried copies only decreases.
func (s *iconStream) admitDepartures() {
	if math.IsInf(s.period, 1) {
		return
	}
	if s.animation.Rate == nil {
		// A single icon starts immediately on an empty wire, or as
		// soon as the last outstanding icon completes its trip.
		s.first = 0
		for _, f := range s.carried {
			s.first = max(s.first, f.remaining(s.path))
		}
		return
	}
	free := d2target.MaxMovingIconsPerConnection - len(s.carried)
	slots := min(d2target.MaxMovingIconsPerConnection, int(math.Ceil(s.path.TransitDuration(s.animation)/s.period)))
	if slots <= free {
		return
	}
	arrivals := make([]float64, len(s.carried))
	for i, f := range s.carried {
		arrivals[i] = f.remaining(s.path)
	}
	xsort.Ordered(arrivals)
	for slot := free; slot < slots; slot++ {
		// This departure needs a slot vacated by an incoming flight.
		s.first = max(s.first, arrivals[slot-free]-float64(slot)*s.period)
	}
}

// Board boundaries can accumulate a fraction of a nanosecond of rounding.
const iconTimeEpsilon = 1e-10 // seconds

// flights returns the live copies without iterating departures outside the
// visible travel window. A closed right boundary includes a launch at t itself.
func (s *iconStream) flights(t float64, includeBoundary bool) []iconFlight {
	if s.path.Length() <= 0 {
		return nil
	}
	var out []iconFlight
	for _, f := range s.carried {
		if t+iconTimeEpsilon < f.remaining(s.path) {
			f.progress += t / s.path.TransitDuration(f.launched)
			out = append(out, f)
		}
	}
	if math.IsInf(s.period, 1) || t+iconTimeEpsilon < s.first {
		return out
	}
	since := max(0, t-s.first)
	lastAge := math.Mod(since, s.period)
	// Round grid-aligned departures consistently at decimal millisecond boundaries.
	if s.period-lastAge < iconTimeEpsilon {
		lastAge = 0
	}
	if !includeBoundary && lastAge < iconTimeEpsilon {
		lastAge = s.period
	}
	transit := s.path.TransitDuration(s.animation)
	for n := 0; n < d2target.MaxMovingIconsPerConnection; n++ {
		age := lastAge + float64(n)*s.period
		if age > since+iconTimeEpsilon || age >= transit {
			break
		}
		out = append(out, iconFlight{progress: age / transit, launched: s.animation})
	}
	if len(out) > d2target.MaxMovingIconsPerConnection {
		// Rapid stop/start edits can temporarily overlap two bounded streams.
		out = out[len(out)-d2target.MaxMovingIconsPerConnection:]
	}
	return out
}

func (tl *IconTimeline) Active(board *d2target.Diagram) bool {
	return tl != nil && tl.boards[board] != nil && tl.boards[board].active
}

// MissingIconSample chooses a time inside the board's hold if the proposed
// storyboard samples all miss the moving artwork. Work depends on the number
// of streams, regardless of how long it takes for the next departure.
func (tl *IconTimeline) MissingIconSample(board *d2target.Diagram, startMS, endMS int, samples []int) (int, bool) {
	if !tl.Active(board) || startMS >= endMS {
		return 0, false
	}
	start, end := float64(startMS)/1000, float64(endMS)/1000
	best := endMS
	for i := range board.Connections {
		c := &board.Connections[i]
		if !c.HasVisibleIconStream() {
			continue
		}
		s := tl.boards[board].streams[c.ID]
		for _, at := range samples {
			if len(s.flights(float64(at)/1000, true)) > 0 {
				return 0, false
			}
		}
		// A carried copy may already be traveling when the hold begins.
		// Otherwise the next launch starts the first visible interval.
		begin, remaining := start, 0.
		for _, f := range s.flights(start, true) {
			remaining = max(remaining, f.remaining(s.path))
		}
		if remaining == 0 {
			begin += s.nextLaunch(start)
			remaining = s.path.TransitDuration(s.animation)
		}
		finish := min(begin+remaining, end)
		if begin >= finish {
			continue
		}
		at := min(endMS-1, int(math.Round((begin+finish)*500)))
		if at < best && len(s.flights(float64(at)/1000, true)) > 0 {
			best = at
		}
	}
	return best, best < endMS
}

// Sample keeps the route of picture (which may be a tween), and evaluates the
// icon clock at elapsedMS since the beginning of the authored board.
func (tl *IconTimeline) Sample(board, picture *d2target.Diagram, elapsedMS int) *d2target.Diagram {
	if tl == nil || tl.boards[board] == nil {
		return picture
	}
	streams := tl.boards[board].streams
	frame := *picture
	var widths map[string]float64
	frame.Connections = slices.Clone(picture.Connections)
	for i := range frame.Connections {
		e := &frame.Connections[i]
		if e.IconAnimation == nil {
			continue
		}
		e.IconPositions = []geo.Point{}
		s := streams[e.ID]
		if s == nil || s.path.Length() == 0 {
			continue
		}
		path := s.path
		if picture != board {
			if widths == nil {
				widths = iconStrokeWidths(picture)
			}
			path = e.IconMotionPath(widths[e.Src], widths[e.Dst])
		}
		for _, f := range s.flights(float64(elapsedMS)/1000, true) {
			e.IconPositions = append(e.IconPositions, path.PointAt(f.progress*path.Length()))
		}
	}
	return &frame
}

func (s *iconStream) nextLaunch(at float64) float64 {
	if math.IsInf(s.period, 1) {
		return math.Inf(1)
	}
	if at <= s.first {
		return s.first - at
	}
	age := math.Mod(at-s.first, s.period)
	if age < iconTimeEpsilon || s.period-age < iconTimeEpsilon {
		return 0
	}
	return s.period - age
}

// ScenePicture gives a fixed native scene its carried icons and launch clock.
// Render that scene with timestamps measured from the beginning of its board.
func (tl *IconTimeline) ScenePicture(board *d2target.Diagram, frame Frame) *d2target.Diagram {
	if tl == nil || tl.boards[board] == nil {
		return frame.Diagram
	}
	return tl.scheduledPicture(board, frame, frame.ElapsedMS, frame.ElapsedMS+frame.DurationMS, false)
}

func (tl *IconTimeline) scheduledPicture(board *d2target.Diagram, frame Frame, startMS, totalMS int, fixedLayer bool) *d2target.Diagram {
	picture := *frame.Diagram
	picture.Connections = slices.Clone(picture.Connections)
	at := float64(frame.ElapsedMS) / 1000
	for i := range picture.Connections {
		e := &picture.Connections[i]
		if e.IconAnimation == nil {
			continue
		}
		s := tl.boards[board].streams[e.ID]
		if s == nil || s.path.Length() <= 0 {
			e.IconPositions = []geo.Point{}
			continue
		}
		schedule := &d2target.ConnectionIconSchedule{
			FixedLayer: fixedLayer, StartMS: startMS, DurationMS: frame.DurationMS, FilmMS: totalMS,
			Transit: s.path.TransitDuration(s.animation), First: s.nextLaunch(at), Period: s.period,
		}
		for _, f := range s.flights(at, false) {
			schedule.Carried = append(schedule.Carried, d2target.ConnectionIconTrip{
				Progress: f.progress,
				Transit:  s.path.TransitDuration(f.launched),
			})
		}
		e.IconSchedule = schedule
	}
	return &picture
}
