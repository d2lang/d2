package sequencediagram_test

import (
	"context"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2sequence"
	sequencediagram "github.com/d2lang/d2/d2layouts/d2sequence2"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/textmeasure"
)

func compile(t *testing.T, script string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("index.d2", strings.NewReader("shape: sequence-diagram\n"+script), nil)
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := sequencediagram.Layout(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	return g
}
func obj(t *testing.T, g *d2graph.Graph, id string) *d2graph.Object {
	t.Helper()
	for _, o := range g.Objects {
		if o.AbsID() == id {
			return o
		}
	}
	t.Fatalf("object %s not found", id)
	return nil
}
func messages(g *d2graph.Graph) []*d2graph.Edge {
	var es []*d2graph.Edge
	for _, e := range g.Edges {
		if !d2sequence.IsLifelineEnd(e.Dst) {
			es = append(es, e)
		}
	}
	return es
}
func finiteBounds(t *testing.T, g *d2graph.Graph) {
	t.Helper()
	for _, o := range g.Objects {
		for _, v := range []float64{o.TopLeft.X, o.TopLeft.Y, o.Width, o.Height} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("nonfinite geometry for %s: %+v", o.AbsID(), o.Box)
			}
		}
		if o.Width <= 0 || o.Height <= 0 || o.TopLeft.X < 0 || o.TopLeft.Y < 0 || o.TopLeft.X+o.Width > g.Root.Width+.01 || o.TopLeft.Y+o.Height > g.Root.Height+.01 {
			t.Fatalf("%s outside diagram: %v root %v", o.AbsID(), o.Box, g.Root.Box)
		}
	}
	for _, e := range g.Edges {
		for _, p := range e.Route {
			if !g.Root.Box.Contains(p) {
				t.Fatalf("route outside diagram: %s %v", e.AbsID(), p)
			}
		}
	}
}
func TestChronologyAndAfterGap(t *testing.T) {
	g := compile(t, `a -> b: first {vertical-gap: 0}; b -> c: simultaneous; a.n: Note {shape: page}; c -> a: last`)
	es := messages(g)
	if len(es) != 3 {
		t.Fatalf("messages %d", len(es))
	}
	if es[0].Route[0].Y != es[1].Route[0].Y {
		t.Fatal("vertical-gap zero must place the next message at the same time")
	}
	note := obj(t, g, "a.n")
	if note.TopLeft.Y <= es[1].Route[0].Y || note.TopLeft.Y+note.Height >= es[2].Route[0].Y {
		t.Fatal("semicolon-separated note is not between messages")
	}
	finiteBounds(t, g)
}
func TestExplicitGapOverridesAutomaticOverlap(t *testing.T) {
	g := compile(t, `a -> b: one {vertical-gap: 13}; b -> a: two`)
	es := messages(g)
	if got := es[1].Route[0].Y - es[0].Route[0].Y; got != 13 {
		t.Fatalf("after gap = %v, want 13", got)
	}
}

func TestMixedSharedRowReservesTallestEvent(t *testing.T) {
	g := compile(t, `vars: {together: 0}
classes: {simultaneous: {vertical-gap: ${together}}}
a; b; c; d; e; f
f.vertical-gap: 1
a -> b: Before
a.note: Make key {shape: page; class: simultaneous}
b.ready: Ready {shape: diamond; height: 240; vertical-gap: 0}
c.again: {shape: actor; vertical-gap: 0}
d -> e: Exchange {vertical-gap: 0}
f.again: {shape: actor}
e -> f: After`)
	es := messages(g)
	y := es[1].Route[0].Y
	for _, id := range []string{"a.note", "b.ready", "c.again", "f.again"} {
		if got := obj(t, g, id).Center().Y; got != y {
			t.Fatalf("%s at y=%v, want shared row y=%v", id, got, y)
		}
	}
	tall := obj(t, g, "b.ready")
	if tall.TopLeft.Y-float64(es[0].LabelDimensions.Height)/2-es[0].Route[0].Y < 48 ||
		es[2].Route[0].Y-float64(es[2].LabelDimensions.Height)/2-(tall.TopLeft.Y+tall.Height) < 48 {
		t.Fatal("ordinary rows must clear the tallest shared event on both sides")
	}
	finiteBounds(t, g)
}

func TestEventGapAndSelfMessageReturn(t *testing.T) {
	g := compile(t, `a -> a: Work {vertical-gap: 0}
b.note: Done {shape: page; vertical-gap: 19}
b -> c: Report`)
	es := messages(g)
	noteY := obj(t, g, "b.note").Center().Y
	if got := es[0].Route[len(es[0].Route)-1].Y; got != noteY {
		t.Fatalf("self-message returns at %v, next event at %v", got, noteY)
	}
	if got := es[1].Route[0].Y - noteY; got != 19 {
		t.Fatalf("gap after note = %v, want 19", got)
	}
}
func TestSelfMessagesNestedSpansAndNotes(t *testing.T) {
	g := compile(t, `a -> b.work: request
 b.work: Processing {label.near: top-right}
 b.work.inner -> b.work.inner: self
 b.note: A rather wide explanatory note {shape: page}
 b.work.inner -> a: reply
 unused: {span}
 `)
	es := messages(g)
	self := es[1]
	if len(self.Route) != 4 || self.Route[0].Y >= self.Route[3].Y {
		t.Fatal("self message must have a vertical return")
	}
	outer, inner := obj(t, g, "b.work"), obj(t, g, "b.work.inner")
	if outer.Label.Value != "Processing" || inner.Label.Value != "" {
		t.Fatal("span labels lost or default labels visible")
	}
	if outer.TopLeft.Y >= inner.TopLeft.Y || outer.TopLeft.Y+outer.Height <= inner.TopLeft.Y+inner.Height {
		t.Fatal("parent span does not contain nested interval")
	}
	if self.Route[0].X != inner.TopLeft.X+inner.Width {
		t.Fatal("self message must leave activation boundary")
	}
	finiteBounds(t, g)
}
func TestGroupsMirrorsAndIdentity(t *testing.T) {
	g := compile(t, `shape.mirrored: true
 participants: {shape: actor-group; a; b}
 exchange: {shape: edge-group; participants.a -> participants.b: start; nested: {shape: edge-group; participants.b -> participants.a: done}}
 participants.a.again: {shape: actor}
 empty: {shape: edge-group}
 `)
	finiteBounds(t, g)
	group := obj(t, g, "exchange")
	nested := obj(t, g, "exchange.nested")
	if !group.Box.Contains(nested.TopLeft) || !group.Box.Contains(nested.Center()) {
		t.Fatal("nested frame escapes outer frame")
	}
	a := obj(t, g, "participants.a")
	mirror := obj(t, g, "participants.a.mirror")
	if mirror.Parent != a || mirror.Label.Value != a.Label.Value {
		t.Fatal("mirror loses actor parent or label")
	}
	for _, o := range g.Objects {
		if o.Parent != nil && o.Parent.Children[strings.ToLower(o.ID)] != o {
			t.Fatalf("inconsistent parent child map: %s", o.AbsID())
		}
	}
	actors := make(map[*d2graph.Object]bool)
	ids := make(map[string]bool)
	for _, e := range g.Edges {
		if d2sequence.IsLifelineEnd(e.Dst) {
			actors[e.Src] = true
			if ids[e.AbsID()] {
				t.Fatal("duplicate lifeline identity")
			}
			ids[e.AbsID()] = true
			if e.Dst.ID != d2sequence.LifelineEndID(e.Src.ID) {
				t.Fatal("unstable lifeline ID")
			}
		}
	}
	if len(actors) != 2 {
		t.Fatalf("actors with lifelines %d want 2", len(actors))
	}
}
func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sequencediagram.Layout(ctx, d2graph.NewGraph()); err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}

func TestWideParticipantGroupsDoNotOverlap(t *testing.T) {
	g := compile(t, `producers: A very wide participant group name {shape: actor-group; inner: {shape: actor-group; a}}
 consumers: Another wide participant group name {shape: actor-group; inner: {shape: actor-group; b}}
 producers.inner.a -> consumers.inner.b: message`)
	a, b := obj(t, g, "producers"), obj(t, g, "consumers")
	if a.TopLeft.X+a.Width >= b.TopLeft.X {
		t.Fatalf("participant groups overlap: %v and %v", a.Box, b.Box)
	}
	finiteBounds(t, g)
}

func TestRepeatedStructuredActor(t *testing.T) {
	g := d2graph.NewGraph()
	g.Root.Shape.Value = d2target.ShapeSequenceDiagramV2
	a := &d2graph.Object{Graph: g, Parent: g.Root, ID: "a", IDVal: "a", Class: &d2target.Class{}, Box: geo.NewBox(geo.NewPoint(0, 0), 100, 60), Attributes: d2graph.Attributes{Shape: d2graph.Scalar{Value: d2target.ShapeClass}}, Children: make(map[string]*d2graph.Object)}
	repeat := &d2graph.Object{Graph: g, Parent: a, ID: "again", IDVal: "again", Box: geo.NewBox(geo.NewPoint(0, 0), 60, 60), Attributes: d2graph.Attributes{Shape: d2graph.Scalar{Value: d2target.ShapeSequenceDiagramActor}}}
	a.Children["again"] = repeat
	a.ChildrenArray = []*d2graph.Object{repeat}
	g.Root.Children["a"] = a
	g.Root.ChildrenArray = []*d2graph.Object{a}
	g.Objects = []*d2graph.Object{a, repeat}
	if err := sequencediagram.Layout(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if repeat.Class == nil || repeat.Shape.Value != d2target.ShapeSequenceDiagramActor {
		t.Fatal("class actor repeat lost structured appearance or semantic role")
	}
	finiteBounds(t, g)
}

func TestQuotedMirrorIdentity(t *testing.T) {
	g := compile(t, `shape.mirrored: true
 "a.b" -> "with spaces": message`)
	if obj(t, g, `"a.b".mirror`).IDVal != "mirror" {
		t.Fatal("quoted actor mirror ID is not escaped")
	}
	finiteBounds(t, g)
}

func TestNestedSpanLabelsStayBelowHeaders(t *testing.T) {
	g := compile(t, `a; b
 a.outer: Outer activation {label.near: top-right}
 a.outer.inner: Inner activation {label.near: top-right}
 b -> a.outer.inner: Start`)
	a, s := obj(t, g, "a"), obj(t, g, "a.outer")
	if s.TopLeft.Y < a.TopLeft.Y+a.Height {
		t.Fatal("nested activation label overlaps actor header")
	}
	finiteBounds(t, g)
}

func TestEmptyGroupOccupiesItsTextualTime(t *testing.T) {
	g := compile(t, `a -> b: before
 empty: {shape: edge-group}
 a.note: Note {shape: page}
 b -> a: after`)
	group, note := obj(t, g, "empty"), obj(t, g, "a.note")
	es := messages(g)
	if group.TopLeft.Y <= es[0].Route[0].Y || group.TopLeft.Y+group.Height >= note.TopLeft.Y || note.TopLeft.Y+note.Height >= es[1].Route[0].Y {
		t.Fatal("empty message group does not reserve its position in the timeline")
	}
	finiteBounds(t, g)
}

func TestImportedTimelineUsesImportSite(t *testing.T) {
	fs := fstest.MapFS{"part.d2": {Data: []byte(`a.note: Imported note {shape: page}
 b -> c: imported`)}}
	g, _, err := d2compiler.Compile("index.d2", strings.NewReader(`shape: sequence-diagram
 first; a; b
 a -> b: before
 ...@part
 c -> a: after`), &d2compiler.CompileOptions{FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := sequencediagram.Layout(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	// The spread introduces c and references previously declared actors.
	// Those references must not move the earlier actors to new columns.
	previousX := math.Inf(-1)
	for _, id := range []string{"first", "a", "b", "c"} {
		x := obj(t, g, id).Center().X
		if x <= previousX {
			t.Fatalf("actor %s at x=%v follows actor at x=%v", id, x, previousX)
		}
		previousX = x
	}
	es := messages(g)
	ys := make(map[string]float64)
	for _, e := range es {
		ys[e.Label.Value] = e.Route[0].Y
	}
	note := obj(t, g, "a.note")
	if !(ys["before"] < note.TopLeft.Y && note.TopLeft.Y+note.Height < ys["imported"] && ys["imported"] < ys["after"]) {
		t.Fatalf("wrong imported chronology: %v, note %v", ys, note.Box)
	}
	finiteBounds(t, g)
}

func TestSelfMessageTravelsOutsideWideActivation(t *testing.T) {
	g := compile(t, `a.work: A deliberately wide activation label {label.near: top-right}
 a.work -> a.work: Recurse`)
	e := messages(g)[0]
	if e.Route[1].X <= e.Route[0].X || e.Route[2].X <= e.Route[3].X {
		t.Fatal("self message bends inside its activation")
	}
	finiteBounds(t, g)
}

func TestUndeclaredActorsFollowEndpointOrderWithinGroup(t *testing.T) {
	g := compile(t, `request: {
 shape: edge-group
 client -> server: Request
 processing: {shape: edge-group; server.work -> database.query: Read}
 }`)
	client, server, database := obj(t, g, "client"), obj(t, g, "server"), obj(t, g, "database")
	if !(client.Center().X < server.Center().X && server.Center().X < database.Center().X) {
		t.Fatalf("actors not in first endpoint order: client=%v server=%v database=%v", client.Center().X, server.Center().X, database.Center().X)
	}
	finiteBounds(t, g)
}

func TestDefaultNestedSpanLabelIsVisibleAboveEvents(t *testing.T) {
	g := compile(t, `client -> server.work: Start
 server.work: Processing {label.near: top-right}
 server.work.inner: Transaction
 server.work.inner -> server.work.inner: Validate
 server.status: Waiting {shape: page}
 server.checkpoint: Checkpoint {shape: diamond}
 server.work.inner -> client: Finished`)
	span := obj(t, g, "server.work.inner")
	if span.LabelPosition == nil || *span.LabelPosition != label.InsideTopLeft.String() {
		t.Fatal("explicit span label should default to the top of its activation")
	}
	p := label.InsideTopLeft.GetPointOnBox(span.Box, label.PADDING, float64(span.LabelDimensions.Width), float64(span.LabelDimensions.Height))
	text := geo.NewBox(p, float64(span.LabelDimensions.Width), float64(span.LabelDimensions.Height))
	if text.TopLeft.X < span.TopLeft.X || text.TopLeft.X+text.Width > span.TopLeft.X+span.Width {
		t.Fatal("activation does not contain its label")
	}
	for _, id := range []string{"server.status", "server.checkpoint"} {
		if text.Overlaps(*obj(t, g, id).Box) {
			t.Fatalf("span label obscured by %s", id)
		}
	}
	self := messages(g)[1]
	if text.TopLeft.Y+text.Height >= self.Route[0].Y {
		t.Fatal("activation label overlaps self message")
	}
	if self.Route[1].X <= span.TopLeft.X+span.Width {
		t.Fatal("self loop bends inside labeled activation")
	}
	finiteBounds(t, g)
}

func TestMirroredSQLTableRecreatesChildMap(t *testing.T) {
	g := compile(t, `shape.mirrored: true
 a: {shape: sql_table; id: int}
 a -> b`)
	a, mirror := obj(t, g, "a"), obj(t, g, "a.mirror")
	if a.SQLTable == nil || mirror.SQLTable != a.SQLTable {
		t.Fatal("mirror lost SQL table appearance")
	}
	if a.Children["mirror"] != mirror || mirror.Parent != a {
		t.Fatal("SQL table mirror is absent from its parent child map")
	}
	finiteBounds(t, g)
}

func TestMessageLabelNear(t *testing.T) {
	g := compile(t, `a; b
vars: {place: outside-top-center}
frame: {
  shape: edge-group
  a -> b: request {label.near: ${place}; style.stroke-width: 12}
  b -> a: reply with a long label anchored toward the left end of the arrow {label.near: outside-top-left}
  b -> b: self above {label.near: outside-top-center}
  b -> b: self below {label.near: outside-bottom-center}
  a -> b: last
}
frame.(a -> b)[1].label.near: center-right
`)
	frame := obj(t, g, "frame")
	for i, e := range messages(g) {
		conn := d2exporter.ToConnection(e, nil)
		text := conn.GetLabelTopLeft()
		cx := text.X + float64(conn.LabelWidth)/2
		stroke := conn.RenderedStrokeWidth()
		first, last := conn.Route[0], conn.Route[len(conn.Route)-1]
		mid := (first.X + last.X) / 2
		bottom := text.Y + float64(conn.LabelHeight)
		if !frame.Box.Contains(text) || !frame.Box.Contains(geo.NewPoint(text.X+float64(conn.LabelWidth), bottom)) {
			t.Errorf("label %q outside its group", e.Label.Value)
		}
		if i <= 2 && bottom > first.Y-stroke/2-label.PADDING+.5 {
			t.Errorf("label %q crosses its top wire", e.Label.Value)
		}
		switch i {
		case 0:
			if math.Abs(cx-mid) > .5 {
				t.Errorf("request label %s is off-center. actual %f want %f position %s", e.Label.Value, cx, mid, conn.LabelPosition)
			}
		case 1:
			if cx >= mid || text.X <= last.X {
				t.Error("right-to-left reply label should fit between the left endpoint and the midpoint")
			}
		case 2, 3:
			if math.Abs(cx-(first.X+conn.Route[1].X)/2) > .5 {
				t.Errorf("label %q should center on the horizontal leg", e.Label.Value)
			}
			if i == 3 && text.Y < last.Y+stroke/2+label.PADDING-.5 {
				t.Error("self-message's bottom label crosses its return wire")
			}
		case 4:
			if cx <= mid || math.Abs((text.Y+bottom)/2-first.Y) > .5 {
				t.Error("center-right label should stay on its arrow's right half")
			}
		}
	}
}

func TestSharedRowsStayInsideTheirBranch(t *testing.T) {
	g := compile(t, `a; b; c
c.before: Previous {shape: page; height: 100; vertical-gap: 0}
frame: {shape: edge-group {branched: true}
  yes: {a -> b: Yes {vertical-gap: 0}; c.yes: OK {shape: page; height: 100; vertical-gap: 0}}
  no: {a -> b: No {vertical-gap: 0}; c.no: Retry {shape: page; height: 100; vertical-gap: 0}}
}
c.after: Next {shape: page; height: 100}`)
	before, after := obj(t, g, "c.before"), obj(t, g, "c.after")
	frame := obj(t, g, "frame")
	first, second := obj(t, g, "c.yes"), obj(t, g, "c.no")
	if before.TopLeft.Y+before.Height > frame.TopLeft.Y || after.TopLeft.Y < frame.TopLeft.Y+frame.Height {
		t.Fatalf("frame %.1f..%.1f intersects preceding bottom %.1f or following top %.1f", frame.TopLeft.Y, frame.TopLeft.Y+frame.Height, before.TopLeft.Y+before.Height, after.TopLeft.Y)
	}
	divider := obj(t, g, "frame.no").TopLeft.Y
	if first.TopLeft.Y+first.Height > divider || second.TopLeft.Y < divider {
		t.Fatalf("divider at %.1f crosses a branch row (%v, %v)", divider, first.Box, second.Box)
	}
}

func TestWideActivationKeepsMessageDirectionAndLabelSpace(t *testing.T) {
	g := compile(t, `a; b
a.work: {width: 600}
b.task: {width: 420}
a.work -> b.task: request
b.task -> a.work: a return with a very long label anchored to the left {label.near: outside-top-left}`)
	a, b := obj(t, g, "a.work"), obj(t, g, "b.task")
	if a.TopLeft.X+a.Width >= b.TopLeft.X {
		t.Fatal("wide activations overlap")
	}
	for _, e := range messages(g) {
		c := d2exporter.ToConnection(e, nil)
		p := c.GetLabelTopLeft()
		if p.X < a.TopLeft.X+a.Width || p.X+float64(c.LabelWidth) > b.TopLeft.X {
			t.Fatalf("caption %q does not fit between activations", c.Label)
		}
	}
	finiteBounds(t, g)
}

func TestZeroGapClearsNestedFrameBoundaries(t *testing.T) {
	g := compile(t, `a; b
a.pre: Before {shape: page; height: 150; vertical-gap: 0}
frame: F {shape: edge-group; inner: {shape: edge-group; a -> b: request {vertical-gap: 0}}}
a.post: After {shape: page; height: 150}`)
	pre, frame, inner, post := obj(t, g, "a.pre"), obj(t, g, "frame"), obj(t, g, "frame.inner"), obj(t, g, "a.post")
	if pre.TopLeft.Y+pre.Height > frame.TopLeft.Y || frame.TopLeft.Y+frame.Height > post.TopLeft.Y {
		t.Fatalf("frame crosses adjacent shared rows: %v %v %v", pre.Box, frame.Box, post.Box)
	}
	if inner.TopLeft.Y <= frame.TopLeft.Y || inner.TopLeft.Y+inner.Height >= frame.TopLeft.Y+frame.Height {
		t.Fatal("outer frame does not contain both inner borders")
	}
}

func TestEmptyActorGroupsGetOwnColumns(t *testing.T) {
	g := compile(t, `first: {shape: actor-group}
a; nested: {shape: actor-group; blank: {shape: actor-group}}; b
a -> b`)
	right := 0.
	for _, id := range []string{"first", "a", "nested", "b"} {
		o := obj(t, g, id)
		if o.TopLeft.X <= right {
			t.Fatalf("header %s overlaps a preceding column", id)
		}
		right = o.TopLeft.X + o.Width
	}
	for _, e := range g.Edges {
		if d2sequence.IsLifelineEnd(e.Dst) && e.Src.Shape.Value == d2target.ShapeSequenceDiagramActorGroup {
			t.Fatal("empty group acquired a lifeline")
		}
	}
	finiteBounds(t, g)
}

func TestArrowheadCaptionsStayInFrameAndClearAdjacentRows(t *testing.T) {
	g := compile(t, `a; b
frame: F {shape: edge-group
a -> b: request {label.near: outside-top-left; source-arrowhead: A long description on the source side; target-arrowhead: An equally long destination caption}
b -> a: reply {target-arrowhead: return caption}
a -> a: self {source-arrowhead: outgoing caption; target-arrowhead: returning caption}
}`)
	frame := obj(t, g, "frame")
	for _, e := range messages(g) {
		c := d2exporter.ToConnection(e, nil)
		for i, caption := range []*d2target.Text{c.SrcLabel, c.DstLabel} {
			if caption == nil {
				continue
			}
			p := c.GetArrowheadLabelPosition(i == 1)
			if !frame.Box.Contains(p) || !frame.Box.Contains(geo.NewPoint(p.X+float64(caption.LabelWidth), p.Y+float64(caption.LabelHeight))) {
				t.Fatalf("endpoint caption %q outside frame %v", caption.Label, frame.Box)
			}
		}
		if c.Label == "request" {
			src, dst := c.GetArrowheadLabelPosition(false), c.GetArrowheadLabelPosition(true)
			main := c.GetLabelTopLeft()
			if src.X+float64(c.SrcLabel.LabelWidth) >= main.X || main.X+float64(c.LabelWidth) >= dst.X || src.Y <= frame.TopLeft.Y+float64(frame.LabelDimensions.Height) {
				t.Fatal("endpoint captions overlap each other or frame title")
			}
		}
	}
	finiteBounds(t, g)
}

func TestSequenceSpreadUsesTimelineOccurrence(t *testing.T) {
	g := compile(t, `
shape.numbered: true
vars: {retry: {c -> a: Retry; a.pause: Pause {shape: page}}}
a -> b: First
...${retry}
b -> c: Last
`)
	rows := make(map[string]float64)
	for _, e := range messages(g) {
		rows[e.Label.Value] = e.Route[0].Y
	}
	_, first := rows["1. First"]
	_, retry := rows["2. Retry"]
	_, last := rows["3. Last"]
	note := obj(t, g, "a.pause")
	if !first || !retry || !last || !(rows["1. First"] < rows["2. Retry"] && rows["2. Retry"] < note.TopLeft.Y && note.TopLeft.Y+note.Height < rows["3. Last"]) {
		t.Fatalf("expected First, Retry, Pause, Last in order, got rows %v, note %v", rows, note.Box)
	}
	if !(obj(t, g, "a").Center().X < obj(t, g, "b").Center().X && obj(t, g, "b").Center().X < obj(t, g, "c").Center().X) {
		t.Fatal("a participant introduced by the spread moved ahead of an existing participant")
	}
}

func TestFixedHeightActivationClearsLaterRows(t *testing.T) {
	g := compile(t, `a; b
a.outer.inner: {height: 450}
a.outer -> a.outer.inner: start
a.outer.inner -> b: finish
a.note: Follow-up {shape: page}`)
	outer, inner, note := obj(t, g, "a.outer"), obj(t, g, "a.outer.inner"), obj(t, g, "a.note")
	if inner.Height < 450 || outer.TopLeft.Y+outer.Height <= inner.TopLeft.Y+inner.Height || note.TopLeft.Y <= outer.TopLeft.Y+outer.Height {
		t.Fatalf("fixed nested activation crosses following note: outer %v inner %v note %v", outer.Box, inner.Box, note.Box)
	}
	finiteBounds(t, g)
}

func TestBranchStyleOverridesKeepNoteInDeclaringFrame(t *testing.T) {
	g := compile(t, `a; b; c
outer: {shape: edge-group {branched: true}
 one: {a.note: Remember {shape: page}; a -> b: First}
 two: {b -> c: Second; a.note.style.fill: yellow; a -> b: Third}
}`)
	note, second := obj(t, g, "a.note"), obj(t, g, "outer.two")
	if note.TopLeft.Y+note.Height >= second.TopLeft.Y || note.Style.Fill == nil || note.Style.Fill.Value != "yellow" {
		t.Fatalf("note was moved or lost its override: %v", note)
	}
}
