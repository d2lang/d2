package d2animate

import (
	"math"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func sequenceFixture() *d2target.Diagram {
	board := d2target.NewDiagram()
	for i, id := range []string{"client", "server", "server.active"} {
		s := d2target.BaseShape()
		s.ID = id
		s.Type = d2target.ShapeRectangle
		s.Width, s.Height = 50, 20
		s.Pos = d2target.Point{X: i * 80, Y: 10}
		if i == 2 {
			s.Pos = d2target.Point{X: 110, Y: 95}
			s.Width, s.Height = 10, 66
		}
		board.Shapes = append(board.Shapes, *s)
	}
	call := d2target.BaseConnection()
	call.ID, call.Src, call.Dst = "call", "client", "server.active"
	call.Label, call.LabelPosition, call.LabelWidth, call.LabelHeight = "Get", "INSIDE_MIDDLE_CENTER", 20, 10
	call.DstArrow = d2target.TriangleArrowhead
	call.Route = geo.Route{{X: 0, Y: 100}, {X: 120, Y: 100}}
	ret := *call
	ret.ID, ret.Label = "reply", "Ok"
	ret.StrokeDash = 3
	ret.SrcArrow, ret.DstArrow = d2target.TriangleArrowhead, d2target.NoArrowhead
	ret.Route = geo.Route{{X: 0, Y: 150}, {X: 120, Y: 150}}
	board.Connections = []d2target.Connection{*call, ret}
	board.SequenceTimelines = []d2target.SequenceTimeline{{
		Messages: []d2target.SequenceMessage{{ConnectionID: "call"}, {ConnectionID: "reply", Reverse: true}},
		Spans:    []d2target.SequenceSpan{{ShapeID: "server.active", Start: d2target.SequenceMoment{Message: 0, Arrival: true}, End: d2target.SequenceMoment{Message: 1}}},
	}}
	return board
}

func atMS(t *testing.T, frames []Frame, ms int) *d2target.Diagram {
	t.Helper()
	for _, f := range frames {
		if ms >= f.ElapsedMS && ms < f.ElapsedMS+f.DurationMS {
			return f.Diagram
		}
	}
	t.Fatalf("no frame at %d ms", ms)
	return nil
}

func TestSequencePlayback(t *testing.T) {
	board := sequenceFixture()
	frames, err := trace(board, 1000, 400)
	if err != nil {
		t.Fatal(err)
	}
	empty := atMS(t, frames, 999)
	if len(empty.Connections) != 0 || len(empty.Shapes) != 2 {
		t.Fatal("future rows or spans visible before first send")
	}
	first := atMS(t, frames, 1200)
	if len(first.Connections) != 1 {
		t.Fatalf("first flight has %d messages", len(first.Connections))
	}
	want := 60.

	c := first.Connections[0]
	if c.Route[1].X != want || c.DstArrow != d2target.TriangleArrowhead {
		t.Fatalf("outbound at midpoint = %+v", c)
	}
	if *c.GetLabelTopLeft() != *board.Connections[0].GetLabelTopLeft() {
		t.Fatal("message caption moved during flight")
	}
	if len(first.Shapes) != 2 {
		t.Fatal("receiver activated before arrival")
	}
	receiving := atMS(t, frames, 1650)
	if len(receiving.Shapes) != 3 || receiving.Shapes[2].Height <= 5 || receiving.Shapes[2].Height >= 66 {
		t.Fatal("activation should grow between messages")
	}
	reply := atMS(t, frames, 2200)
	if len(reply.Connections) != 2 || reply.Shapes[2].Height != 66 {
		t.Fatalf("previous message or completed span missing: %+v", reply)
	}
	r := reply.Connections[1]
	start := 60.

	if r.StrokeDash != 3 || r.SrcArrow != d2target.TriangleArrowhead || r.Route[0].X != start || r.Route[1].X != 120 {
		t.Fatalf("return traced in wrong direction or lost style: %+v", r)
	}
	elapsed := 0
	for i, f := range frames {
		if f.Continues != (i < len(frames)-1) {
			t.Fatalf("wrong rounding boundary for frame %d", i)
		}
		if f.ElapsedMS != elapsed || f.DurationMS <= 0 {
			t.Fatalf("broken schedule at %+v", f)
		}
		if err := d2target.ValidateRenderTarget(f.Diagram); err != nil {
			t.Fatal(err)
		}
		tl, br := f.Diagram.BoundingBox()
		wantTL, wantBR := board.BoundingBox()
		if tl != wantTL || br != wantBR {
			t.Fatalf("viewport moved to %v–%v", tl, br)
		}
		elapsed += f.DurationMS
	}
	if elapsed != 3000 || frames[len(frames)-1].Diagram != board || board.Connections[0].Route[1].X != 120 || board.Shapes[2].Height != 66 {
		t.Fatal("incorrect endpoint, timing or source mutation")
	}
}

func trace(board *d2target.Diagram, intervalMS, durationMS int) ([]Frame, error) {
	return sequenceFrames(board, intervalMS, durationMS, new(sequenceBudget), nil)
}

func TestSequenceSelfCallAndIndependentTimelines(t *testing.T) {
	board := sequenceFixture()
	self := board.Connections[0]
	self.ID = "self"
	self.Src, self.Dst = "server", "server"
	self.Route = geo.Route{{X: 120, Y: 45}, {X: 180, Y: 45}, {X: 180, Y: 65}, {X: 120, Y: 65}}
	board.Connections = append(board.Connections, self)
	board.SequenceTimelines = append(board.SequenceTimelines, d2target.SequenceTimeline{Messages: []d2target.SequenceMessage{{ConnectionID: "self"}}})
	frames, err := trace(board, 1000, 400)
	if err != nil {
		t.Fatal(err)
	}
	middle := atMS(t, frames, 1200).Connections
	if len(middle) != 2 || middle[1].ID != "self" || len(middle[1].Route) != 3 || *middle[1].Route[2] != (geo.Point{X: 180, Y: 55}) {
		t.Fatalf("self-call did not trace around its bend: %+v", middle)
	}
	if got := atMS(t, frames, 2200).Connections[2]; len(got.Route) != 4 {
		t.Fatal("shorter independent sequence reset after its final row")
	}
}

func TestSequenceSamplingBounds(t *testing.T) {
	b := sequenceFixture()
	if _, err := trace(b, math.MaxInt, 1); err == nil {
		t.Fatal("unrepresentable timeline accepted")
	}
	b.SequenceTimelines[0].Spans[0].Start.Message = -1
	if _, err := trace(b, 100, 40); err == nil {
		t.Fatal("invalid span moment accepted")
	}
	for _, duration := range []int{40, 100, 150} {
		b := sequenceFixture()
		frames, err := trace(b, 100, duration)
		if err != nil {
			t.Fatal(err)
		}
		last := frames[len(frames)-1]
		expected := 100 + 2*max(100, duration)
		if duration >= 100 {
			expected += 100
		}
		if last.Diagram != b || last.ElapsedMS+last.DurationMS != expected {
			t.Fatalf("%dms final frame = %+v", duration, last)
		}
	}
}

func TestSequenceBudgetSpansMultipleBoards(t *testing.T) {
	board := sequenceFixture()
	budget := &sequenceBudget{frames: maxSequenceFrames - 4}
	if _, err := sequenceFrames(board, 1, 1, budget, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := sequenceFrames(board, 1, 1, budget, nil); err == nil {
		t.Fatal("second board exceeded the shared frame budget")
	}

	objects := 1 + len(board.Shapes) + len(board.Connections)
	for _, c := range board.Connections {
		objects += len(c.Route)
	}
	budget = &sequenceBudget{objects: maxSequenceFrameObjects - 4*objects}
	if _, err := sequenceFrames(board, 1, 1, budget, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := sequenceFrames(board, 1, 1, budget, nil); err == nil {
		t.Fatal("second board exceeded the retained object budget")
	}
}

func TestOneMillisecondSequenceTrace(t *testing.T) {
	board := sequenceFixture()
	frames, err := trace(board, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 4 {
		t.Fatalf("expected four 1ms slots, got %d", len(frames))
	}
	if len(atMS(t, frames, 1).Connections) != 0 || len(atMS(t, frames, 2).Connections) != 1 || atMS(t, frames, 2).Connections[0].ID != "call" || atMS(t, frames, 3) != board {
		t.Fatal("last trace sample did not carry completed messages into the next slot")
	}
}

func TestIncomingValueTweenUsesSequenceStage(t *testing.T) {
	const interval = 200
	for _, duration := range []int{80, 400} {

		board := sequenceFixture()
		ms := int64(duration)
		board.Config = &d2target.Config{AnimateDuration: &ms}
		board.TweenDurationMS = duration
		for i, fill := range []string{"#0033AA", "#AACCFF"} {
			sample := sequenceFixture()
			sample.SequenceTimelines = nil // compiler exports metadata on the authored board
			sample.Shapes[0].Fill = fill
			sample.Shapes[0].Width = 55 + i*170 // bounds must come from this sample
			board.TweenFrames = append(board.TweenFrames, sample)
		}
		original := AppendBoardFrames(nil, board, interval)
		frames, err := (&Playback{}).AppendBoard(nil, board, interval)
		if err != nil {
			t.Fatal(err)
		}
		for i, expected := range original {
			f := frames[i]
			if f.ElapsedMS != expected.ElapsedMS || f.DurationMS != expected.DurationMS ||
				f.TweenSample != expected.TweenSample || !f.Continues {
				t.Fatalf("%d changed incoming timing at %d", duration, i)
			}
			if len(f.Diagram.Connections) != 0 || len(f.Diagram.Shapes) != 2 {
				t.Fatal("future messages or activations flashed during the incoming step")
			}
			if f.Diagram.Shapes[0].Fill != expected.Diagram.Shapes[0].Fill || f.Diagram.Shapes[0].Width != expected.Diagram.Shapes[0].Width {
				t.Fatal("incoming actor lost its interpolated style or geometry")
			}
			tl, br := f.Diagram.BoundingBox()
			originalTL, originalBR := expected.Diagram.BoundingBox()
			if tl != originalTL || br != originalBR {
				t.Fatal("incoming sample changed its viewport")
			}
		}
		send := max(duration, interval)
		if next := frames[len(original)]; next.ElapsedMS != send {
			t.Fatalf("expected first message at %d, got %+v", send, next)
		}
		if len(atMS(t, frames, send+duration/2).Connections) != 1 {
			t.Fatal("first message did not trace following the tween")
		}
		if frames[len(frames)-1].Diagram != board {
			t.Fatal("completion must use the exact endpoint")
		}
		for _, orig := range board.TweenFrames {
			if len(orig.Connections) != 2 || len(orig.Shapes) != 3 {
				t.Fatal("original tween target mutated")
			}
		}
		budget := sequenceBudget{objects: maxSequenceFrameObjects - sequenceBoardCost(board)}
		playback := Playback{sequence: budget}
		if _, err := playback.AppendBoard(nil, board, interval); err == nil || playback.sequence != budget {
			t.Fatal("partial incoming frames bypassed or consumed the budget on failure")
		}
	}
}

func TestEditedSequenceTracesOnlyChangedRows(t *testing.T) {
	b := sequenceFixture()
	b.SequenceContinuation = true
	b.SequenceTimelines[0].Messages[0].Unchanged = true
	frames, err := trace(b, 1000, 400)
	if err != nil {
		t.Fatal(err)
	}
	start := atMS(t, frames, 0)
	if len(start.Connections) != 1 || geo.Route(start.Connections[0].Route).Length() != 120 || len(start.Shapes) != 3 {
		t.Fatal("the previous call or its activation disappeared at the edit")
	}
	mid := atMS(t, frames, 200)
	if len(mid.Connections) != 2 || geo.Route(mid.Connections[0].Route).Length() != 120 || geo.Route(mid.Connections[1].Route).Length() != 60 || mid.Connections[1].StrokeDash != 3 {
		t.Fatal("only the new reply should trace, starting at board entry")
	}
	end := frames[len(frames)-1]
	if end.Diagram != b || end.ElapsedMS+end.DurationMS != 1000 {
		t.Fatalf("one edit should take one message slot, got %+v", end)
	}
	// With all rows retained, the normal board/step timeline is used intact.
	b.SequenceTimelines[0].Messages[1].Unchanged = true
	duration := int64(400)
	b.Config = &d2target.Config{AnimateDuration: &duration}
	b.TweenFrames = []*d2target.Diagram{sequenceFixture()}
	b.TweenDurationMS = 400
	got, err := (&Playback{}).AppendBoard(nil, b, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range AppendBoardFrames(nil, b, 1000) {
		if got[i] != want {
			t.Fatal("unrelated node edit should retain ordinary step frames")
		}
	}
	if len(got) != 2 {
		t.Fatal("unrelated node edit retraced a sequence")
	}
}

func TestMessageTraceOverlaysIncomingStyleChanges(t *testing.T) {
	b := sequenceFixture()
	b.SequenceContinuation = true
	b.SequenceTimelines[0].Messages[0].Unchanged = true
	duration := int64(400)
	b.Config = &d2target.Config{AnimateDuration: &duration}
	b.TweenDurationMS = 400
	for _, stroke := range []string{"red", "magenta"} {
		sample := sequenceFixture()
		sample.SequenceTimelines = nil
		sample.Connections[1].Stroke = stroke
		sample.Shapes[0].Fill = stroke
		b.TweenFrames = append(b.TweenFrames, sample)
	}
	got, err := (&Playback{}).AppendBoard(nil, b, 1000)
	if err != nil {
		t.Fatal(err)
	}
	quarter := atMS(t, got, 100)
	half := atMS(t, got, 200)
	if quarter.Connections[1].Stroke != "red" || geo.Route(quarter.Connections[1].Route).Length() >= geo.Route(half.Connections[1].Route).Length() ||
		half.Connections[1].Stroke != "magenta" || half.Shapes[0].Fill != "magenta" || geo.Route(half.Connections[1].Route).Length() != 60 || geo.Route(half.Connections[0].Route).Length() != 120 {
		t.Fatal("trace and style edits should share the step clock; the previous call should stay visible")
	}
	if got[len(got)-1].Diagram != b || geo.Route(b.TweenFrames[1].Connections[1].Route).Length() != 120 {
		t.Fatal("value target was mutated or the endpoint was lost")
	}
}

func TestRetainedRowsWithNestedSpansAndIndependentSequences(t *testing.T) {
	b := sequenceFixture()
	b.SequenceContinuation = true
	timeline := &b.SequenceTimelines[0]
	outer := b.Shapes[2]
	outer.ID = "server.active.inner"
	b.Shapes = append(b.Shapes, outer)
	b.Connections[0].Dst = outer.ID
	b.Connections[1].Dst = outer.ID
	b.Connections[1].SrcArrow, b.Connections[1].DstArrow = d2target.NoArrowhead, d2target.TriangleArrowhead
	timeline.Messages[1].Reverse = false
	timeline.Messages[0].Unchanged = true
	timeline.Spans[0].End.Arrival = true
	nested := timeline.Spans[0]
	nested.ShapeID = outer.ID
	timeline.Spans = append(timeline.Spans, nested)
	var sequenceTwo d2target.SequenceTimeline
	for i, id := range []string{"new", "retained", "later"} {
		c := b.Connections[0]
		c.ID = id
		c.Src, c.Dst = "client", "server"
		b.Connections = append(b.Connections, c)
		sequenceTwo.Messages = append(sequenceTwo.Messages, d2target.SequenceMessage{ConnectionID: id, Unchanged: i == 1})
	}
	b.SequenceTimelines = append(b.SequenceTimelines, sequenceTwo)
	got, err := trace(b, 1000, 400)
	if err != nil {
		t.Fatal(err)
	}
	before := atMS(t, got, 0)
	if len(before.Connections) != 2 || before.Connections[1].ID != "retained" || len(before.Shapes) != 4 {
		t.Fatal("retained rows and their nested spans should be present before the edited messages")
	}
	for _, s := range before.Shapes[2:] {
		if s.Height < 5 || s.Height >= outer.Height {
			t.Fatalf("span %s must reach its retained call, got %d", s.ID, s.Height)
		}
	}
	half := atMS(t, got, 200)
	if len(half.Connections) != 4 || geo.Route(half.Connections[1].Route).Length() != 60 || geo.Route(half.Connections[2].Route).Length() != 60 || geo.Route(half.Connections[3].Route).Length() != 120 {
		t.Fatal("first changed rows on independent sequences should trace together")
	}
	later := atMS(t, got, 1200)
	if len(later.Connections) != 5 || geo.Route(later.Connections[1].Route).Length() != 120 || geo.Route(later.Connections[4].Route).Length() != 60 {
		t.Fatal("retained middle row consumed a message slot")
	}
}

func TestContinuationEndsWithinItsMessageSlots(t *testing.T) {
	for _, tc := range []struct{ interval, duration int }{{100, 100}, {100, 250}, {1, 1}} {
		for _, retained := range []bool{false, true} {
			b := sequenceFixture()
			b.SequenceContinuation = true
			b.SequenceTimelines[0].Messages[0].Unchanged = retained
			got, err := trace(b, tc.interval, tc.duration)
			if err != nil {
				t.Fatal(err)
			}
			n := 2
			if retained {
				n = 1
			}
			want := n * max(tc.interval, tc.duration)
			last := got[len(got)-1]
			if last.Diagram != b || last.ElapsedMS+last.DurationMS != want || last.DurationMS < 1 {
				t.Fatalf("%+v with %d edits has an invalid endpoint at %d+%dms, want %dms", tc, n, last.ElapsedMS, last.DurationMS, want)
			}
			if n == 2 && tc.duration > 1 {
				interior := atMS(t, got, tc.duration)
				if len(interior.Connections) != 1 || interior.Connections[0].ID != "call" {
					t.Fatal("expected the first message to persist at the second send")
				}
			}
		}
	}
}
