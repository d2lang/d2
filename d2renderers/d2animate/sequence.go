package d2animate

import (
	"fmt"
	"math"
	"strings"

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
	// Retained messages may have already opened this activation.
	retainedY float64
	retained  bool
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
	// Translate authored message indices to playback rows. Retained messages
	// stay on the canvas and consume no time in their sequence's schedule.
	openUntil := make([]int, length)
	for i, timeline := range board.SequenceTimelines {
		var rows []*d2target.Connection
		rowIndex := make([]int, len(timeline.Messages))
		// Only exact span IDs (or their qualified ancestors) can be opened by
		// an endpoint. An unrelated message between two calls doesn't open a span.
		retainedY := make(map[string]float64)
		for _, span := range timeline.Spans {
			retainedY[span.ShapeID] = math.Inf(-1)
		}
		recordEndpoint := func(id string, y float64) {
			for {
				if floor, ok := retainedY[id]; ok {
					retainedY[id] = max(floor, y)
				}
				dot := strings.LastIndexByte(id, '.')
				if dot < 0 {
					break
				}
				id = id[:dot]
			}
		}
		for j, message := range timeline.Messages {
			edge := edges[message.ConnectionID]
			if message.Unchanged {
				rowIndex[j] = -1
				if len(retainedY) > 0 {
					recordEndpoint(edge.Src, edge.Route[0].Y)
					recordEndpoint(edge.Dst, edge.Route[len(edge.Route)-1].Y)
				}
				continue
			}
			rowIndex[j] = len(rows)
			plan.messages[message.ConnectionID] = messagePosition{len(rows), message.Reverse}
			rows = append(rows, edge)
		}
		plan.rows = append(plan.rows, rows)
		for _, span := range timeline.Spans {
			span.Start.Message = rowIndex[span.Start.Message]
			span.End.Message = rowIndex[span.End.Message]
			if span.End.Message < 0 {
				// The final endpoint has already been shown; draw the full bar.
				continue
			}
			floor := retainedY[span.ShapeID]
			plan.spans[span.ShapeID] = spanPosition{span, i, floor, !math.IsInf(floor, -1)}
			start := max(0, span.Start.Message)
			openUntil[start] = max(openUntil[start], span.End.Message)
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

// sequenceFrames samples the new and edited messages in authored order.
// Independent sequence containers run concurrently on the same board clock.
func sequenceFrames(board *d2target.Diagram, intervalMS, durationMS int, budget *sequenceBudget, incoming []Frame) ([]Frame, error) {
	if board == nil {
		return nil, fmt.Errorf("sequence playback requires a diagram")
	}
	if len(board.SequenceTimelines) == 0 || durationMS == 0 {
		return nil, nil
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
		changed := 0
		for _, message := range timeline.Messages {
			if !message.Unchanged {
				changed++
			}
		}
		length = max(length, changed)
	}
	if length == 0 {
		return nil, nil
	}
	slot := max(intervalMS, durationMS)
	finalHold := 0
	if durationMS == slot && !board.SequenceContinuation {
		// Allow the completed standalone sequence to be read before it loops.
		finalHold = intervalMS
	}
	incomingEnd := 0
	if len(incoming) > 0 {
		last := incoming[len(incoming)-1]
		incomingEnd = last.ElapsedMS + last.DurationMS
	}
	// Root sequences introduce their participants before the first send.
	// A step has already shown the preceding board, so its first edit starts
	// together with any incoming color, size, or opacity changes.
	intro := 0
	if !board.SequenceContinuation {
		intro = max(intervalMS, incomingEnd)
	}
	if intro > math.MaxInt-finalHold || slot > (math.MaxInt-intro-finalHold)/length {
		return nil, fmt.Errorf("sequence playback duration overflows milliseconds")
	}
	total := intro + slot*length + finalHold
	if incomingEnd > total {
		finalHold += incomingEnd - total
	}
	if length >= maxSequenceFrames {
		return nil, fmt.Errorf("sequence playback exceeds %d frames", maxSequenceFrames)
	}
	plan := newSequencePlan(board, length)
	// Each scheduled sample records when the following frame becomes visible.
	// Preflight both the number of samples and their retained board size.
	type sample struct {
		row, elapsed, duration int
		progress, gap          float64
		moving                 bool
		base                   *d2target.Diagram
	}
	var samples []sample
	// Merge the value and arrow sample boundaries before copying geometry.
	// A change on either clock must reach the renderer.
	costs := map[*d2target.Diagram]int{board: sequenceBoardCost(board)}
	reserved := *budget
	in := 0
	schedule := func(s sample) error {
		end := s.elapsed + s.duration
		for start := s.elapsed; start < end; {
			next := s
			next.base = board
			until := end
			if in < len(incoming) {
				f := incoming[in]
				next.base = f.Diagram
				until = min(end, f.ElapsedMS+f.DurationMS)
				next.moving = next.moving || f.TweenSample
				if until == f.ElapsedMS+f.DurationMS {
					in++
				}
			}
			next.elapsed, next.duration = start, until-start
			cost, ok := costs[next.base]
			if !ok {
				cost = sequenceBoardCost(next.base)
				costs[next.base] = cost
			}
			if reserved.frames >= maxSequenceFrames || cost > maxSequenceFrameObjects-reserved.objects {
				return fmt.Errorf("sequence playback exceeds its frame budget")
			}
			reserved.frames++
			reserved.objects += cost
			samples = append(samples, next)
			start = until
		}
		return nil
	}
	if err := schedule(sample{row: -1, duration: intro}); err != nil {
		return nil, err
	}
	elapsed := intro
	add := func(row, duration int, tracing, grow bool) error {
		if duration == 0 {
			return nil
		}
		n := 1
		if tracing || grow {
			// Same 25 fps sampling and per-transition cap as step interpolation.
			n = min(d2target.MaxTweenFrames, 1+(duration-1)/40)
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
			if tracing && i == n-1 && row == length-1 && board.SequenceContinuation && durationMS == slot {
				// Use the last sampling period for both the in-flight arrow and
				// its exact endpoint, as for an ordinary value transition.
				mid := s
				mid.duration /= 2
				if err := schedule(mid); err != nil {
					return err
				}
				s.elapsed += mid.duration
				s.duration -= mid.duration
				s.progress, s.moving = 1, false
			}
			if err := schedule(s); err != nil {
				return err
			}
		}
		elapsed += duration
		return nil
	}
	for row := range length {
		if err := add(row, durationMS, true, false); err != nil {
			return nil, err
		}
		hold := slot - durationMS
		if row == length-1 {
			hold += finalHold
		}
		if err := add(row, hold, false, plan.growing[row]); err != nil {
			return nil, err
		}
	}
	*budget = reserved
	frames := make([]Frame, 0, len(samples))
	for i, s := range samples {
		d := s.base
		if s.row != length-1 || s.progress != 1 {
			d = plan.frame(d, s.row, s.progress, s.gap)
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
				if !span.retained && !momentReached(span.Start, row, progress) {
					continue
				}
				if !momentReached(span.End, row, progress) {
					y := max(float64(shape.Pos.Y), span.retainedY)
					if row >= 0 && momentReached(span.Start, row, progress) {
						y = max(y, p.cursorY(span.timeline, row, progress, gap))
					}
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
