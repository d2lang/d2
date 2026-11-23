package d2animate

import (
	"fmt"
	"math"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// Bound the complete set of resident frames before copying any render targets.
// Include route points in the work estimate since every renderer walks them.
const (
	maxSequenceFrames       = 1800
	maxSequenceFrameObjects = 250_000
)

// A multiboard export retains sampled diagrams until rendering. Share this
// admission budget across boards in one export, including GIF rasterization.
type sequenceBudget struct {
	frames  int
	objects int
}

type messagePosition struct {
	row     int
	reverse bool
}

type spanPosition struct {
	d2target.SequenceSpan
	timeline int
}

type sequencePlan struct {
	board    *d2target.Diagram
	template d2target.Diagram
	messages map[string]messagePosition
	spans    map[string]spanPosition
	rows     [][]*d2target.Connection
	// An activation continuing into the next row grows during the inter-message pause.
	growing []bool
}

func newSequencePlan(board *d2target.Diagram, length int) sequencePlan {
	plan := sequencePlan{board: board, template: board.SequenceFrame(),
		messages: make(map[string]messagePosition), spans: make(map[string]spanPosition),
		growing: make([]bool, length)}
	edges := make(map[string]*d2target.Connection, len(board.Connections))
	for i := range board.Connections {
		edge := &board.Connections[i]
		edges[edge.ID] = edge
	}
	// Sweep intervals once, including overlapping activations on different actors.
	openUntil := make([]int, length)
	for i, timeline := range board.SequenceTimelines {
		var rows []*d2target.Connection
		for j, message := range timeline.Messages {
			plan.messages[message.ConnectionID] = messagePosition{j, message.Reverse}
			rows = append(rows, edges[message.ConnectionID])
		}
		plan.rows = append(plan.rows, rows)
		for _, span := range timeline.Spans {
			plan.spans[span.ShapeID] = spanPosition{span, i}
			openUntil[span.Start.Message] = max(openUntil[span.Start.Message], span.End.Message)
		}
	}
	end := 0
	for row := range plan.growing {
		end = max(end, openUntil[row])
		plan.growing[row] = row < end
	}
	return plan
}

// Count retained geometry and render work before allocating partial boards.
func sequenceBoardCost(board *d2target.Diagram) int {
	objects := 1 + len(board.Shapes) + len(board.Connections)
	for _, c := range board.Connections {
		objects += len(c.Route)
	}
	if board.Legend != nil {
		// Legend connections render on small synthesized two-point routes.
		objects += len(board.Legend.Shapes) + 3*len(board.Legend.Connections)
	}
	return objects
}

// SequenceFrames schedules a single drawable board. Its initial state holds for
// intervalMS. Messages then start every max(intervalMS, durationMS), whether
// their arrows are traced or revealed. Independent sequences run concurrently.
func SequenceFrames(board *d2target.Diagram, mode string, intervalMS, durationMS int) ([]Frame, error) {
	return sequenceFrames(board, mode, intervalMS, durationMS, new(sequenceBudget), nil)
}

func sequenceFrames(board *d2target.Diagram, mode string, intervalMS, durationMS int, budget *sequenceBudget, incoming []Frame) ([]Frame, error) {
	if board == nil {
		return nil, fmt.Errorf("sequence playback requires a diagram")
	}
	if len(board.SequenceTimelines) == 0 {
		return nil, nil
	}
	if mode != "trace" && mode != "reveal" {
		return nil, fmt.Errorf("unknown sequence playback mode %q", mode)
	}
	if intervalMS <= 0 || durationMS < 0 || durationMS > d2target.MaxTweenDurationMS {
		return nil, fmt.Errorf("invalid sequence playback interval %dms or duration %dms", intervalMS, durationMS)
	}
	// The caller may supply a render target directly. Validate this board
	// before dereferencing message IDs, span moments or route points.
	drawable := *board
	drawable.Layers, drawable.Scenarios, drawable.Steps, drawable.TweenFrames = nil, nil, nil, nil
	drawable.TweenDurationMS = 0
	if err := d2target.ValidateRenderTarget(&drawable); err != nil {
		return nil, err
	}
	length := 0
	for _, timeline := range board.SequenceTimelines {
		length = max(length, len(timeline.Messages))
	}
	slot := max(intervalMS, durationMS)
	finalHold := 0
	if durationMS == slot {
		// Keep the completed final message visible before playback loops.
		finalHold = intervalMS
	}
	// Incoming value samples play on the empty stage. They replace its initial
	// hold and keep their original timing, even if the value tween is longer.
	intro := intervalMS
	if len(incoming) > 0 {
		last := incoming[len(incoming)-1]
		intro = last.ElapsedMS + last.DurationMS
	}
	if intro > math.MaxInt-finalHold || slot > (math.MaxInt-intro-finalHold)/length {
		return nil, fmt.Errorf("sequence playback duration overflows milliseconds")
	}
	if length >= maxSequenceFrames {
		return nil, fmt.Errorf("sequence playback exceeds %d frames", maxSequenceFrames)
	}
	plan := newSequencePlan(board, length)
	flight := durationMS
	if mode == "reveal" {
		flight = 0
	}
	// Each scheduled sample records when the following frame becomes visible.
	// Preflight both the number of samples and their retained board size.
	type sample struct {
		row, elapsed, duration int
		progress, gap          float64
		moving                 bool
	}
	var samples []sample
	reserved := *budget
	for _, frame := range incoming {
		cost := sequenceBoardCost(frame.Diagram)
		if reserved.frames >= maxSequenceFrames || cost > maxSequenceFrameObjects-reserved.objects {
			return nil, fmt.Errorf("sequence playback exceeds its frame budget during the incoming value transition")
		}
		reserved.frames++
		reserved.objects += cost
	}
	if len(incoming) == 0 {
		samples = append(samples, sample{row: -1, duration: intro})
	}
	elapsed := intro
	objects := sequenceBoardCost(board)
	remaining := min(maxSequenceFrames-reserved.frames, (maxSequenceFrameObjects-reserved.objects)/objects)
	add := func(row, duration int, tracing, grow bool) error {
		if duration == 0 {
			return nil
		}
		n := 1
		if tracing || grow {
			// Same 25 fps sampling and per-transition cap as step interpolation.
			n = min(d2target.MaxTweenFrames, 1+(duration-1)/40)
		}
		if n > remaining-len(samples) {
			return fmt.Errorf("sequence playback exceeds its frame budget (%d frames available for this board)", remaining)
		}
		for i := range n {
			// Distribute remainders without multiplying a long interval by i.
			boundary := func(j int) int { return (duration/n)*j + (duration%n)*j/n }
			start, end := boundary(i), boundary(i+1)
			fraction := float64(start) / float64(duration)
			s := sample{row: row, elapsed: elapsed + start, duration: end - start, progress: 1, moving: tracing || grow}
			if tracing {
				s.progress = fraction
			} else if grow {
				s.gap = fraction
			}
			samples = append(samples, s)
		}
		elapsed += duration
		return nil
	}
	for row := range length {
		if err := add(row, flight, true, false); err != nil {
			return nil, err
		}
		hold := slot - flight
		if row == length-1 {
			hold += finalHold
		}
		if err := add(row, hold, false, plan.growing[row]); err != nil {
			return nil, err
		}
	}
	reserved.frames += len(samples)
	reserved.objects += len(samples) * objects
	*budget = reserved
	frames := make([]Frame, 0, len(incoming)+len(samples))
	for _, f := range incoming {
		f.Diagram = plan.frame(f.Diagram, -1, 0, 0)
		f.Continues = true
		frames = append(frames, f)
	}
	for i, s := range samples {
		d := board
		if s.row != length-1 || s.progress != 1 {
			d = plan.frame(board, s.row, s.progress, s.gap)
		}
		frames = append(frames, Frame{Diagram: d, DurationMS: s.duration, ElapsedMS: s.elapsed, TweenSample: s.moving, Continues: i+1 < len(samples)})
	}
	return frames, nil
}

func (p *sequencePlan) frame(board *d2target.Diagram, row int, progress, gap float64) *d2target.Diagram {
	d := p.template
	if board != p.board {
		d = board.SequenceFrame()
	}
	if len(p.spans) == 0 {
		d.Shapes = board.Shapes
	} else {
		d.Shapes = make([]d2target.Shape, 0, len(board.Shapes))
		for _, shape := range board.Shapes {
			if span, ok := p.spans[shape.ID]; ok {
				if !momentReached(span.Start, row, progress) {
					continue
				}
				if !momentReached(span.End, row, progress) {
					y := p.cursorY(span.timeline, row, progress, gap)
					shape.Height = min(shape.Height, max(1, int(math.Round(y))-shape.Pos.Y))
				}
			}
			d.Shapes = append(d.Shapes, shape)
		}
	}
	d.Connections = make([]d2target.Connection, 0, len(board.Connections))
	for _, c := range board.Connections {
		if m, ok := p.messages[c.ID]; ok {
			if m.row > row || (m.row == row && progress == 0) {
				continue
			}
			if m.row == row && progress < 1 {
				route := traceRoute(c.Route, m.reverse, progress)
				if len(route) < 2 {
					continue
				}
				c = c.WithTracedRoute(route)
			}
		}
		d.Connections = append(d.Connections, c)
	}
	return &d
}

func momentReached(m d2target.SequenceMoment, row int, progress float64) bool {
	return row > m.Message || row == m.Message && (!m.Arrival || progress >= 1)
}

func (p *sequencePlan) cursorY(timeline, row int, progress, gap float64) float64 {
	rows := p.rows[timeline]
	route := rows[row].Route
	// Time runs down a lifeline even for a source-arrow self-message.
	top := min(route[0].Y, route[len(route)-1].Y)
	bottom := max(route[0].Y, route[len(route)-1].Y)
	if gap > 0 && row+1 < len(rows) {
		next := rows[row+1].Route
		return bottom + gap*(min(next[0].Y, next[len(next)-1].Y)-bottom)
	}
	return top + progress*(bottom-top)
}

// Keep the authored path orientation so source-arrow messages keep their start
// marker, styles and dash direction. Include each corner the trace has passed.
func traceRoute(route geo.Route, reverse bool, fraction float64) geo.Route {
	if reverse {
		fraction = 1 - fraction
	}
	length := route.Length()
	if length == 0 {
		return nil
	}
	point, i := route.GetPointAtDistance(length * fraction)
	var partial geo.Route
	if reverse {
		if *point != *route[i+1] {
			partial = append(partial, point)
		}
		return append(partial, route[i+1:]...)
	}
	partial = append(partial, route[:i+1]...)
	if *point != *route[i] {
		partial = append(partial, point)
	}
	return partial
}
