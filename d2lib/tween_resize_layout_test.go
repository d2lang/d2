package d2lib

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2graph"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
)

func TestResizeTweensPreserveEveryAuthoredBoard(t *testing.T) {
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
steps.repeat.vars.wide: 520
steps.observe: {orders.log.style.fill: blue}
steps.observe.layers.other: {a -> b}`
	ordinary, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
	adjusted := 0
	board, _ := compileContinuityTween(t, source, d2talalayout.ContinueBoard, func(ctx context.Context, a, b *d2graph.Graph) (bool, error) {
		changed, err := d2talalayout.TweenResize(ctx, a, b)
		if changed {
			adjusted++
		}
		return changed, err
	})
	if adjusted != 3 {
		t.Fatalf("refined %d resize tracks, want 3", adjusted)
	}
	if err := d2target.ValidateRenderTarget(board); err != nil {
		t.Fatal(err)
	}
	var checkAuthored func(*d2target.Diagram, *d2target.Diagram)
	checkAuthored = func(got, want *d2target.Diagram) {
		t.Helper()
		if !reflect.DeepEqual(got.Shapes, want.Shapes) || !reflect.DeepEqual(got.Connections, want.Connections) ||
			!reflect.DeepEqual(got.Root, want.Root) || !reflect.DeepEqual(got.Legend, want.Legend) {
			t.Fatalf("tween changed the ordinary held layout of %q", got.Name)
		}
		for i, step := range got.Steps {
			checkAuthored(step, want.Steps[i])
		}
		for i, layer := range got.Layers {
			checkAuthored(layer, want.Layers[i])
		}
	}
	checkAuthored(board, ordinary)

	// Verify the emitted samples too: keeping the previous layout right up
	// to the held board would pass endpoint equality but still jump on arrival.
	for i, step := range board.Steps[:3] {
		start := board
		if i > 0 {
			start = board.Steps[i-1]
		}
		if len(step.TweenFrames) < 4 {
			t.Fatalf("step %d has no useful interstitial samples", i)
		}
		end := shapeByID(t, step, "orders.workers.consumer")
		begin := shapeByID(t, start, end.ID)
		prevWidth := begin.Width
		for j, frame := range step.TweenFrames {
			worker := shapeByID(t, frame, end.ID)
			if j == 0 && (worker.Pos != begin.Pos || worker.Width != begin.Width) {
				t.Fatalf("step %d starts at %v with width %d, want %v with width %d", i, worker.Pos, worker.Width, begin.Pos, begin.Width)
			}
			if i == 1 && worker.Width > prevWidth || i != 1 && worker.Width < prevWidth {
				t.Fatalf("step %d sample %d width reversed", i, j)
			}
			prevWidth = worker.Width
		}
		last := step.TweenFrames[len(step.TweenFrames)-1]
		for _, shape := range step.Shapes {
			before, penultimate := shapeByID(t, start, shape.ID), shapeByID(t, last, shape.ID)
			travel := math.Hypot(float64(shape.Pos.X-before.Pos.X), float64(shape.Pos.Y-before.Pos.Y))
			if travel < 100 {
				continue
			}
			tail := math.Hypot(float64(shape.Pos.X-penultimate.Pos.X), float64(shape.Pos.Y-penultimate.Pos.Y))
			if tail > travel*0.4 {
				t.Fatalf("step %d defers %.0f of %s's %.0f-unit movement to the held board", i, tail, shape.ID, travel)
			}
		}
	}
}

func TestResizeLayoutHonorsOtherLayoutConstraints(t *testing.T) {
	for _, constraint := range []string{"a.left: 50\na.top: 40", "group: {grid-columns: 2; l; r}", "c.near: a", "a.style.3d: true", "a.style.multiple: true"} {
		src := "vars: {d2-config: {animate-interval: 400ms; animate-duration: 180ms}; wide: 100}\na: {width: ${wide}}\na -> b\nc\n" + constraint + "\nsteps.grow.vars.wide: 220"
		adjusted := false
		d, _ := compileContinuityTween(t, src, d2talalayout.ContinueBoard, func(ctx context.Context, start, end *d2graph.Graph) (bool, error) {
			var err error
			adjusted, err = d2talalayout.TweenResize(ctx, start, end)
			return adjusted, err
		})
		if len(d.Steps[0].TweenFrames) == 0 || adjusted {
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
	board, _ := compileContinuityTween(t, source, d2talalayout.ContinueBoard, d2talalayout.TweenResize)
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
				// SVG shape positions are truncated to integer coordinates, but route
				// points retain their subpixel positions during the translation.
				if math.Abs(point.X-wire.Route[j].X-float64(legend.Pos.X-key.Pos.X)) > 1 ||
					math.Abs(point.Y-wire.Route[j].Y-float64(legend.Pos.Y-key.Pos.Y)) > 1 {
					t.Fatalf("corner key left an internal connection behind at %v", point)
				}
			}
		}
	}
	ordinary, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
	for i, step := range board.Steps {
		if shapeByID(t, step, key.ID).Pos != shapeByID(t, ordinary.Steps[i], key.ID).Pos ||
			!reflect.DeepEqual(indexTweenRoutes(step).edges[wire.ID], indexTweenRoutes(ordinary.Steps[i]).edges[wire.ID]) {
			t.Fatalf("step %d changed the ordinary position or wire of the corner key", i)
		}
	}
}
