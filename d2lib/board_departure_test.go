package d2lib

import (
	"reflect"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestBoardDepartureShrinksToPortAndRetractsWire(t *testing.T) {
	for _, edge := range []string{"hub -> worker", "worker -> hub", "hub <-> worker"} {
		t.Run(edge, func(t *testing.T) {
			src := `vars: {d2-config: {animate-interval: 900ms; animate-duration: 600ms}}
direction: right
core -> hub
steps: {
  add: {` + edge + ` {style.stroke-dash: 3}; worker.style.opacity: 0.7}
  remove: {worker: null}
}`
			d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
			before, after := d.Steps[0], d.Steps[1]
			if len(after.TweenFrames) < 5 || after.TweenDurationMS != 600 {
				t.Fatal("missing departure samples")
			}
			first := after.TweenFrames[0]
			if !reflect.DeepEqual(first.Shapes, before.Shapes) || !reflect.DeepEqual(first.Connections, before.Connections) {
				t.Fatal("the departure must start at the preceding step's exact endpoint")
			}
			node := shapeByID(t, before, "worker")
			wire := before.Connections[len(before.Connections)-1]
			full := geo.Route(wire.Route).Length()
			width, length, alpha := node.Width, full, node.Opacity
			shrank, retracted := false, false
			for _, frame := range after.TweenFrames[1:] {
				for _, survivor := range after.Shapes {
					if !reflect.DeepEqual(survivor, shapeByID(t, frame, survivor.ID)) {
						t.Fatalf("surviving %s moved or changed appearance", survivor.ID)
					}
				}
				s := shapeByID(t, frame, node.ID)
				if s.Width > width || s.Opacity > alpha {
					t.Fatal("a departing node rebounded or flashed")
				}
				ei := slices.IndexFunc(frame.Connections, func(e d2target.Connection) bool { return e.ID == wire.ID })
				current := 0.
				if ei >= 0 {
					e := frame.Connections[ei]
					current = geo.Route(e.Route).Length()
					anchor, oldAnchor := e.Route[0], wire.Route[0]
					if wire.Src == node.ID {
						anchor, oldAnchor = e.Route[len(e.Route)-1], wire.Route[len(wire.Route)-1]
					}
					if *anchor != *oldAnchor || e.SrcArrow != wire.SrcArrow || e.DstArrow != wire.DstArrow {
						t.Fatal("the stationary end or arrow direction changed")
					}
					if current < full && (e.Label != "" || e.Tooltip != "" || e.Link != "") {
						t.Fatal("a partial wire kept its full-route caption or badge")
					}
				}
				if current > length || full-current > 8 && s.Opacity > .02 {
					t.Fatal("the retracting wire grew or separated from a visible node")
				}
				shrank = shrank || s.Width < node.Width && s.Opacity > 0
				retracted = retracted || current > 0 && current < full
				width, alpha, length = s.Width, s.Opacity, current
			}
			if !shrank || !retracted || length != 0 || alpha != 0 {
				t.Fatal("the node and wire did not visibly depart and finish hidden")
			}
			if err := d2target.ValidateRenderTarget(d); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBoardDepartureKeepsEmptyStepAndSkipsInterruptedFolder(t *testing.T) {
	const root = `vars: {d2-config: {animate-interval: 500ms; animate-duration: 300ms}}
cache
`
	const remove = "steps: {empty: {cache: null}}"
	for _, tc := range []struct {
		name, src string
		visible   bool
	}{
		{"remove last node", root + remove, true},
		{"layer interrupts logical predecessor", root + "layers: {other: {logger}}\n" + remove, false},
		{"empty folder with nested boards", root + "steps: {empty: {cache: null; layers: {other: {logger}}}}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := compileContinuity(t, tc.src, d2talalayout.ContinueBoard)
			empty := d.Steps[0]
			if empty.IsFolderOnly == tc.visible || (len(empty.TweenFrames) > 0) != tc.visible || len(empty.Shapes) != 0 {
				t.Fatalf("empty step folder=%t, samples=%d, shapes=%d", empty.IsFolderOnly, len(empty.TweenFrames), len(empty.Shapes))
			}
		})
	}
}

func TestBoardDepartureDrainsOuterBranchBeforeInnerWire(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 800ms; animate-duration: 600ms}}
direction: right
router -> queue: post
worker -> queue: consume
steps: {remove: {queue: null; worker: null}}`
	d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	step := d.Steps[0]
	inner := continuityWire(t, d, "router", "queue")
	full := geo.Route(inner.Route).Length()
	var innerRetracting, outerRetracting bool
	for _, f := range step.TweenFrames {
		for _, e := range f.Connections {
			if e.Src == "worker" && e.MotionTip != "" {
				outerRetracting = true
			}
			if e.ID == inner.ID && geo.Route(e.Route).Length() < full {
				innerRetracting = true
				if shapeByID(t, f, "worker").Opacity != 0 || len(f.Connections) != 1 {
					t.Fatal("wire to the outer tier was still present when its parent retracted")
				}
			}
		}
	}
	if !innerRetracting || !outerRetracting {
		t.Fatal("both branches must visibly retract")
	}
}

func TestBoardDepartureAndRebuildFromDisplayedEmptyCanvas(t *testing.T) {
	const root = `vars: {d2-config: {animate-interval: 700ms; animate-duration: 420ms}}
A
`
	for _, tc := range []struct {
		name, steps string
		restore     int
		wantShapes  int
	}{
		{"restore one node", "clear: {A: null}; restore: {B}; hold: {}", 1, 1},
		{"empty holds then restore branch", "clear: {A: null}; hold: {}; holdAgain: {}; restore: {B -> C}", 3, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := compileContinuity(t, root+"steps: {"+tc.steps+"}", d2talalayout.ContinueBoard)
			for i, step := range d.Steps[:tc.restore] {
				if step.IsFolderOnly || len(step.Shapes) != 0 || (len(step.TweenFrames) > 0) != (i == 0) {
					t.Fatalf("empty step %d has folder=%t, %d shapes, %d samples", i, step.IsFolderOnly, len(step.Shapes), len(step.TweenFrames))
				}
			}
			next := d.Steps[tc.restore]
			if len(next.TweenFrames) < 3 || len(next.TweenFrames[0].Shapes) != 0 || len(next.Shapes) != tc.wantShapes {
				t.Fatal("rebuilding did not start on the displayed blank canvas")
			}
			partial := false
			for _, frame := range next.TweenFrames[1:] {
				s := shapeByID(t, frame, "B")
				partial = partial || s.Width < next.Shapes[0].Width && s.Opacity > 0
			}
			if !partial {
				t.Fatal("the restored node did not visibly grow")
			}
			if err := d2target.ValidateRenderTarget(d); err != nil {
				t.Fatal(err)
			}
		})
	}

	d, _ := compileContinuity(t, root+"layers: {other: {logger}}\nsteps: {clear: {A: null}; hold: {}; restore: {B}}", d2talalayout.ContinueBoard)
	if !d.Steps[0].IsFolderOnly || !d.Steps[1].IsFolderOnly || len(d.Steps[2].TweenFrames) != 0 {
		t.Fatal("an unrelated layer incorrectly acquired a blank hold or a rebuild from an undisplayed board")
	}
}

func TestBoardDepartureEdgeOnlyKeepsCardsAndDropsCaption(t *testing.T) {
	const src = `vars: {d2-config: {animate-interval: 800ms; animate-duration: 600ms}}
sender: {left: 0; top: 0}
receiver: {left: 300; top: 0}
sender -> receiver: RPC {tooltip: Wire metadata; icon.style.animated: 2/s}
steps: {remove: {(sender -> receiver)[0]: null}}`
	d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	step := d.Steps[0]
	partial := false
	for _, f := range step.TweenFrames {
		if !reflect.DeepEqual(f.Shapes, step.Shapes) {
			t.Fatal("removing only a wire changed its endpoint cards")
		}
		if len(f.Connections) > 0 && f.Connections[0].MotionTip != "" {
			e := f.Connections[0]
			if e.Label != "" || e.Tooltip != "" || e.IconShape != "" || e.IconAnimation != nil {
				t.Fatal("a detached caption, icon or badge remained on the retracting route")
			}
			partial = true
		}
	}
	if !partial || len(step.Connections) != 0 {
		t.Fatal("missing a partial wire or an exact wireless endpoint")
	}
}

func TestBoardDepartureRequiresUnchangedSurvivors(t *testing.T) {
	const root = `vars: {d2-config: {animate-interval: 900ms; animate-duration: 600ms}}
direction: right
gateway -> worker
`
	for _, edit := range []string{"worker: null; gateway.tooltip: changed", "worker: null; gateway: Different", "worker: null; logger"} {
		d, g := compileContinuity(t, root+"steps: {change: {"+edit+"}}", d2talalayout.ContinueBoard)
		if planBoardDeparture(d, d.Steps[0], g) != nil {
			t.Fatalf("unrelated edit was treated as pure removal: %q", edit)
		}
	}
	const panel = "anchor\npanel: {worker; logger}\nsteps: {remove: {panel: null}}"
	d, g := compileContinuity(t, panel, d2talalayout.ContinueBoard)
	if planBoardDeparture(d, d.Steps[0], g) != nil {
		t.Fatal("removing a whole container left an unanimated panel around its departing leaves")
	}
}
