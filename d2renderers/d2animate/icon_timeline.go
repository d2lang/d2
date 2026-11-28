package d2animate

import (
	"fmt"
	"math"
	"slices"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
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
	period, first float64   // seconds between launches, and first launch in this board
	carried       []float64 // distance from source at the start of this board
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
	var timeline IconTimeline
	var last *d2target.Diagram
	var visit func(board, predecessor *d2target.Diagram)
	visit = func(board, predecessor *d2target.Diagram) {
		if !board.IsFolderOnly {
			// Unrelated layers and scenarios can reuse the same object and edge IDs.
			// Carry a stream only into its adjacent inheriting step.
			var prev *iconBoard
			continuing := predecessor != nil && predecessor == last
			if continuing {
				prev = timeline.boards[predecessor]
			}
			b := makeIconBoard(board, intervalMS, prev, continuing)
			if b != nil {
				if timeline.boards == nil {
					timeline.boards = make(map[*d2target.Diagram]*iconBoard)
				}
				timeline.boards[board] = b
			}
			last = board
		}
		for _, group := range [][]*d2target.Diagram{board.Layers, board.Scenarios} {
			for _, child := range group {
				visit(child, nil)
			}
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

func makeIconBoard(board *d2target.Diagram, intervalMS int, prev *iconBoard, continuing bool) *iconBoard {
	var b *iconBoard
	for i, path := range board.ConnectionIconRoutes() {
		e := &board.Connections[i]
		if b == nil {
			b = &iconBoard{streams: make(map[string]*iconStream), duration: float64(max(intervalMS, board.ReadingMS(), board.TweenDurationMS)) / 1000}
		}
		s := &iconStream{path: path, period: path.LaunchPeriod(e.IconAnimation)}
		prevID := e.ID
		if e.IconAnimation.PreviousID != nil {
			prevID = *e.IconAnimation.PreviousID
		}
		if prev != nil && prevID != "" && prev.streams[prevID] != nil {
			old := prev.streams[prevID]
			// At a boundary the next board owns the departure at that exact instant.
			s.carried = old.distances(prev.duration, false)
			if oldLength := old.path.Length(); oldLength > 0 {
				for i := range s.carried {
					s.carried[i] *= path.Length() / oldLength
				}
			}
			if !math.IsInf(s.period, 1) && !math.IsInf(old.period, 1) {
				s.first = old.nextLaunch(prev.duration) / old.period * s.period
			}
		} else if !continuing {
			// The opening board shows the stream already running.
			for n := 1; n < d2target.MaxMovingIconsPerConnection; n++ {
				age := float64(n) * s.period
				if age >= path.Length()/d2target.IconMotionSpeed {
					break
				}
				s.carried = append(s.carried, age*d2target.IconMotionSpeed)
			}
		}
		b.streams[e.ID] = s
		b.active = b.active || len(s.carried) > 0 || !math.IsInf(s.period, 1)
	}
	return b
}

// distances returns the live copies without iterating departures outside the
// visible travel window. A closed right boundary includes a launch at t itself.
func (s *iconStream) distances(t float64, includeBoundary bool) []float64 {
	length := s.path.Length()
	var out []float64
	for _, start := range s.carried {
		d := start + t*d2target.IconMotionSpeed
		if d < length {
			out = append(out, d)
		}
	}
	if math.IsInf(s.period, 1) || t < s.first {
		return out
	}
	since := t - s.first
	lastAge := math.Mod(since, s.period)
	// Round grid-aligned departures consistently at decimal millisecond boundaries.
	if s.period-lastAge < 1e-10 {
		lastAge = 0
	}
	if !includeBoundary && lastAge < 1e-10 {
		lastAge = s.period
	}
	for n := 0; n < d2target.MaxMovingIconsPerConnection; n++ {
		age := lastAge + float64(n)*s.period
		d := age * d2target.IconMotionSpeed
		if age > since+1e-10 || d >= length {
			break
		}
		out = append(out, d)
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
			if len(s.distances(float64(at)/1000, true)) > 0 {
				return 0, false
			}
		}
		// A carried copy may already be traveling when the hold begins.
		// Otherwise the next launch starts the first visible interval.
		travel := s.path.Length() / d2target.IconMotionSpeed
		begin, remaining := start, 0.
		for _, distance := range s.distances(start, true) {
			remaining = max(remaining, travel-distance/d2target.IconMotionSpeed)
		}
		if remaining == 0 {
			begin += s.nextLaunch(start)
			remaining = travel
		}
		finish := min(begin+remaining, end)
		if begin >= finish {
			continue
		}
		at := min(endMS-1, int(math.Round((begin+finish)*500)))
		if at < best && len(s.distances(float64(at)/1000, true)) > 0 {
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
		for _, dist := range s.distances(float64(elapsedMS)/1000, true) {
			progress := dist / s.path.Length()
			e.IconPositions = append(e.IconPositions, path.PointAt(progress*path.Length()))
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
	phase := (at - s.first) / s.period
	return math.Max(0, (math.Ceil(phase-1e-10)-phase)*s.period)
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
		d := s.distances(at, false)
		for i := range d {
			d[i] /= s.path.Length()
		}
		first := s.nextLaunch(at)
		e.IconSchedule = &d2target.ConnectionIconSchedule{
			FixedLayer: fixedLayer, StartMS: startMS, DurationMS: frame.DurationMS, FilmMS: totalMS,
			Transit: s.path.Length() / d2target.IconMotionSpeed, First: first, Period: s.period, Carried: d,
		}
	}
	return &picture
}
