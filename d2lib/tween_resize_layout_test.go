package d2lib

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
)

func TestResizeLayoutKeepsSiblingOrderAndReleasesSpace(t *testing.T) {
	const source = `vars: {
  d2-config: {animate-interval: 1500ms; animate-duration: 400ms}
  wide: 130
}
direction: right
title: Orders and inventory {
  shape: text
  near: top-center
}
orders: Orders {
  direction: down
  ingress: API
  workers: Worker group {consumer: "Order consumer" {width: ${wide}}; checkpoint: Checkpoint}
  log: Journal {shape: cylinder}
  ingress -> workers.consumer -> log
  workers.consumer -> workers.checkpoint
}
stores: Inventory {
  cache: Cache {shape: cylinder}
  update: Apply changes
  cache -> update
}
audit: Reconciliation {collector: Collect; monthly: Report; collector -> monthly}
orders.workers.consumer -> stores.cache: reserve
orders.log -> audit.collector
stores.update -> audit.collector
legend: Types {near: bottom-right; key: Worker}
steps.expand.vars.wide: 520
steps.release.vars.wide: 130
steps.repeat.vars.wide: 520`
	board, graph := compileContinuity(t, source, d2talalayout.ContinueBoard)
	initial := shapeByID(t, board, "orders.workers.consumer")
	for i, step := range board.Steps {
		if len(step.TweenFrames) < 4 || graph.Steps[i].ResizeLayoutOrigin != graph {
			t.Fatalf("step %d lost the resize layout (%d samples, origin %p, root %p)", i, len(step.TweenFrames), graph.Steps[i].ResizeLayoutOrigin, graph)
		}
		var last d2target.Shape
		for j, frame := range append(append([]*d2target.Diagram(nil), step.TweenFrames...), step) {
			worker := shapeByID(t, frame, "orders.workers.consumer")
			stores := shapeByID(t, frame, "stores")
			orders := shapeByID(t, frame, "orders")
			title := shapeByID(t, frame, "title")
			legend := shapeByID(t, frame, "legend")
			if worker.Pos != initial.Pos || worker.Pos.X+worker.Width > orders.Pos.X+orders.Width ||
				worker.Pos.Y+worker.Height > orders.Pos.Y+orders.Height {
				t.Fatalf("step %d sample %d worker escaped its original anchor or parent", i, j)
			}
			overlap := orders.Pos.X < stores.Pos.X+stores.Width && stores.Pos.X < orders.Pos.X+orders.Width && orders.Pos.Y < stores.Pos.Y+stores.Height && stores.Pos.Y < orders.Pos.Y+orders.Height
			if overlap || title.Pos.Y+title.Height > min(stores.Pos.Y, orders.Pos.Y) ||
				legend.Pos.X < orders.Pos.X+orders.Width {
				t.Fatalf("step %d sample %d crossed sibling or decoration: stores %v %dx%d, orders %v %dx%d, title %v %dx%d, legend %v", i, j, stores.Pos, stores.Width, stores.Height, orders.Pos, orders.Width, orders.Height, title.Pos, title.Width, title.Height, legend.Pos)
			}
			if j > 0 && (i == 1 && worker.Width > last.Width || i != 1 && worker.Width < last.Width) {
				t.Fatalf("step %d sample %d width reversed", i, j)
			}
			last = worker
		}
	}
	if shapeByID(t, board.Steps[0], "stores").Pos == shapeByID(t, board, "stores").Pos {
		t.Fatal("fixture did not push the adjacent service container")
	}
	for _, pair := range [][2]*d2target.Diagram{{board, board.Steps[1]}, {board.Steps[0], board.Steps[2]}} {
		for _, want := range pair[0].Shapes {
			got := shapeByID(t, pair[1], want.ID)
			if got.Pos != want.Pos || got.Width != want.Width || got.Height != want.Height || got.LabelPosition != want.LabelPosition {
				t.Fatalf("revisiting the same size moved %s from %v to %v", want.ID, want.Pos, got.Pos)
			}
		}
		repeated := indexTweenRoutes(pair[1])
		for _, want := range pair[0].Connections {
			got := repeated.edges[want.ID]
			if got == nil {
				t.Fatalf("missing connection %s", want.ID)
			}
			if measureTweenRoute(got.Route).displacement2(measureTweenRoute(want.Route)) > 1e-12 || got.LabelPosition != want.LabelPosition || got.LabelPercentage != want.LabelPercentage {
				t.Fatalf("revisiting the same size changed %s: %v %v/%v, want %v %v/%v", want.ID, measureTweenRoute(got.Route), got.LabelPosition, got.LabelPercentage, measureTweenRoute(want.Route), want.LabelPosition, want.LabelPercentage)
			}
		}
	}
}

func TestResizeLayoutHonorsOtherLayoutConstraints(t *testing.T) {
	for _, constraint := range []string{"a.left: 50\na.top: 40", "group: {grid-columns: 2; l; r}", "c.near: a", "c.near: top-center\na -> c", "a.style.3d: true", "a.style.multiple: true"} {
		src := "vars: {d2-config: {animate-interval: 400ms; animate-duration: 180ms}; wide: 100}\na: {width: ${wide}}\na -> b\nc\n" + constraint + "\nsteps.grow.vars.wide: 220"
		d, graph := compileContinuity(t, src, d2talalayout.ContinueBoard)
		if len(d.Steps[0].TweenFrames) == 0 || graph.Steps[0].ResizeLayoutOrigin != nil {
			t.Fatalf("resize placement took over %q", constraint)
		}
	}
}

func TestResizeLayoutMovesCornerKeyWithItsWires(t *testing.T) {
	const source = `vars: {
  d2-config: {animate-interval: 1400ms; animate-duration: 320ms}
  depth: 90
}
direction: down
title: Pipeline {near: top-center; shape: text}
ingest: "Source data" {
  validate: "Check"
  queue: "Queue" {height: ${depth}}
  validate -> queue: "validated data"
}
transform: "Batch processing" {
  split: "Partitions"
  reduce: "Combiner"
  split -> reduce
}
archive: "Warehouse" {live: Hot; cold: Cold; live -> cold}
ingest.queue -> transform.split: "pending batches"
transform.reduce -> archive.live: "checkpoint"
legend: "Transport" {
  near: bottom-right
  in: Inbound
  out: Outbound
  in -> out
}
steps.expand.vars.depth: 390
steps.collapse.vars.depth: 90`
	board, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
	key := shapeByID(t, board, "legend")
	var wire *d2target.Connection
	for i := range board.Connections {
		if board.Connections[i].Src == "legend.in" {
			wire = &board.Connections[i]
			break
		}
	}
	if wire == nil {
		t.Fatal("corner key has no internal connection")
	}
	for _, step := range board.Steps {
		for _, frame := range append(append([]*d2target.Diagram(nil), step.TweenFrames...), step) {
			legend := shapeByID(t, frame, key.ID)
			moved := indexTweenRoutes(frame).edges[wire.ID]
			if moved == nil || len(moved.Route) != len(wire.Route) {
				t.Fatal("moving the corner key changed its internal route")
			}
			for j, point := range moved.Route {
				if point.X-wire.Route[j].X != float64(legend.Pos.X-key.Pos.X) ||
					point.Y-wire.Route[j].Y != float64(legend.Pos.Y-key.Pos.Y) {
					t.Fatalf("corner key left an internal connection behind at %v", point)
				}
			}
		}
	}
	if shapeByID(t, board.Steps[0], key.ID).Pos.Y <= key.Pos.Y ||
		shapeByID(t, board.Steps[1], key.ID).Pos != key.Pos {
		t.Fatal("corner key did not follow the height change and return")
	}
}
