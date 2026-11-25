package d2lib

import (
	"image"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
)

func continuityRect(t *testing.T, d *d2target.Diagram, id string) image.Rectangle {
	t.Helper()
	s := shapeByID(t, d, id)
	return image.Rect(s.Pos.X, s.Pos.Y, s.Pos.X+s.Width, s.Pos.Y+s.Height)
}

func continuityWire(t *testing.T, d *d2target.Diagram, src, dst string) d2target.Connection {
	t.Helper()
	for _, e := range d.Connections {
		if e.Src == src && e.Dst == dst {
			return e
		}
	}
	t.Fatalf("%s: missing %s -> %s", d.Name, src, dst)
	return d2target.Connection{}
}

func requireStableBoardGeometry(t *testing.T, old, current *d2target.Diagram, ids []string, wires [][2]string) {
	t.Helper()
	if !current.LayoutContinuity {
		t.Fatalf("%s lost the predecessor coordinate frame", current.Name)
	}
	for _, id := range ids {
		if continuityRect(t, old, id) != continuityRect(t, current, id) {
			t.Errorf("%s moved unaffected shape %s", current.Name, id)
		}
	}
	for _, w := range wires {
		a, b := continuityWire(t, old, w[0], w[1]), continuityWire(t, current, w[0], w[1])
		if !reflect.DeepEqual(a.Route, b.Route) || a.IsCurve != b.IsCurve {
			t.Errorf("%s rerouted unaffected edge %s -> %s", current.Name, w[0], w[1])
		}
	}
}

func TestTALABoardContinuityCompoundEdits(t *testing.T) {
	const source = `direction: right
intake: Intake {
  gateway: Gateway; queue: Order queue; worker: Order worker
  gateway -> queue -> worker
}
fulfillment: Fulfillment {pack: Packing; ship: Shipping; pack -> ship}
intake.worker -> fulfillment.pack
steps: {
  audit: {
    audit: Audit {log: Audit log; reports: Reports; log -> reports}
    intake.worker -> audit.log
  }
  transfer: {
    intake.worker: null
    fulfillment.worker: Order worker
    intake.queue -> fulfillment.worker
    fulfillment.worker -> fulfillment.pack
    fulfillment.worker -> audit.log
  }
  dispatch: {fulfillment.ship -> audit.log}
}`
	board, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
	audit, transfer, dispatch := board.Steps[0], board.Steps[1], board.Steps[2]
	familiar := []string{"intake.gateway", "intake.queue", "fulfillment.pack", "fulfillment.ship"}
	wires := [][2]string{{"intake.gateway", "intake.queue"}, {"fulfillment.pack", "fulfillment.ship"}}
	requireStableBoardGeometry(t, board, audit, append(familiar, "intake.worker"), append(wires, [2]string{"intake.worker", "fulfillment.pack"}))
	requireStableBoardGeometry(t, audit, transfer, append(familiar, "audit.log", "audit.reports"), append(wires, [2]string{"audit.log", "audit.reports"}))
	if !continuityRect(t, transfer, "fulfillment.worker").In(continuityRect(t, transfer, "fulfillment")) {
		t.Fatal("transferred worker escaped its destination")
	}
	for _, s := range transfer.Shapes {
		if s.ID == "intake.worker" {
			t.Fatal("transferred worker remains in its original panel")
		}
	}
	if got, want := len(dispatch.Connections), len(transfer.Connections)+1; got != want {
		t.Fatalf("edge-only addition produced %d connections, want %d", got, want)
	}
	continuityWire(t, dispatch, "fulfillment.ship", "audit.log")
	var ids []string
	for _, s := range transfer.Shapes {
		ids = append(ids, s.ID)
	}
	var retained [][2]string
	for _, e := range transfer.Connections {
		retained = append(retained, [2]string{e.Src, e.Dst})
	}
	requireStableBoardGeometry(t, transfer, dispatch, ids, retained)
}

func TestTALABoardContinuityInsertedGateHonorsFlow(t *testing.T) {
	const source = `direction: down
build: Build; staging: Staging
build -> staging
steps: {
  production: {production: Production; staging -> production}
  smoke: {smoke: Smoke tests; staging -> smoke}
  gate: {
    (staging -> production)[0]: null
    approval: Approval
    staging -> approval -> production
  }
}`
	d, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
	production, smoke, gate := d.Steps[0], d.Steps[1], d.Steps[2]
	requireStableBoardGeometry(t, d, production, []string{"build", "staging"}, [][2]string{{"build", "staging"}})
	requireStableBoardGeometry(t, production, smoke, []string{"build", "staging", "production"}, [][2]string{{"build", "staging"}, {"staging", "production"}})
	requireStableBoardGeometry(t, smoke, gate, []string{"build", "staging", "smoke"}, [][2]string{{"build", "staging"}, {"staging", "smoke"}})
	centerY := func(id string) int {
		rect := continuityRect(t, gate, id)
		return rect.Min.Y + rect.Max.Y
	}
	if !(centerY("staging") < centerY("approval") && centerY("approval") < centerY("production")) {
		t.Fatal("inserted gate placed a forward dependency upstream")
	}
	continuityWire(t, gate, "staging", "approval")
	continuityWire(t, gate, "approval", "production")
}

func TestTALABoardContinuityReversedDependencyHonorsFlow(t *testing.T) {
	for _, direction := range []string{"down", "up", "right", "left"} {
		t.Run(direction, func(t *testing.T) {
			const edit = `
a: Prepare; b: Publish
a -> b
steps: {reverse: {(a -> b)[0]: null; b -> a}}`
			d, _ := compileContinuity(t, "direction: "+direction+edit, d2talalayout.ContinueBoard)
			step := d.Steps[0]
			a, b := continuityRect(t, step, "a"), continuityRect(t, step, "b")
			travel := map[string]int{
				"down":  a.Min.Y + a.Max.Y - b.Min.Y - b.Max.Y,
				"up":    b.Min.Y + b.Max.Y - a.Min.Y - a.Max.Y,
				"right": a.Min.X + a.Max.X - b.Min.X - b.Max.X,
				"left":  b.Min.X + b.Max.X - a.Min.X - a.Max.X,
			}
			if travel[direction] <= 0 || len(step.Connections) != 1 {
				t.Fatal("reversing a dependency preserved its old direction")
			}
			continuityWire(t, step, "b", "a")
		})
	}
}

const nestedContinuityExample = `direction: right
platform: Platform {
  api: API
  telemetry: Telemetry {ingest: Collector; index: Search index; ingest -> index}
  api -> telemetry.ingest
}
reliability: Reliability {dashboard: On-call dashboard; pager: Pager; dashboard -> pager}
platform.telemetry.index -> reliability.dashboard
steps: {
  clients: {
    clients: Customers {
      mobile: Mobile; web: Web; app: Public gateway
      mobile -> app; web -> app
    }
    clients.app -> platform.api
  }
  transfer: {
    platform.telemetry: null
    reliability.telemetry: Telemetry {ingest: Collector; index: Search index; ingest -> index}
    platform.api -> reliability.telemetry.ingest
    reliability.telemetry.index -> reliability.dashboard
  }
  archive: {
    reliability.telemetry: {
      archive: Archive {batch: Batcher; store: Cold storage; batch -> store}
      index -> archive.batch
    }
  }
}`

func TestTALABoardContinuityNestedContainment(t *testing.T) {
	d, _ := compileContinuity(t, nestedContinuityExample, d2talalayout.ContinueBoard)
	for _, frame := range d.Steps {
		for _, s := range frame.Shapes {
			index := strings.LastIndexByte(s.ID, '.')
			if index >= 0 && !continuityRect(t, frame, s.ID).In(continuityRect(t, frame, s.ID[:index])) {
				t.Errorf("%s: %s escaped its parent", frame.Name, s.ID)
			}
		}
	}
	clients, transfer, archive := d.Steps[0], d.Steps[1], d.Steps[2]
	oldPanel := continuityRect(t, clients, "platform.telemetry")
	newPanel := continuityRect(t, transfer, "reliability.telemetry")
	shift := newPanel.Min.Sub(oldPanel.Min)
	for _, suffix := range []string{"", ".ingest", ".index"} {
		oldBox := continuityRect(t, clients, "platform.telemetry"+suffix)
		newBox := continuityRect(t, transfer, "reliability.telemetry"+suffix)
		if oldBox.Add(shift) != newBox {
			t.Fatalf("moving Telemetry changed the internal geometry of %q", "telemetry"+suffix)
		}
	}
	oldRoute := continuityWire(t, clients, "platform.telemetry.ingest", "platform.telemetry.index").Route
	newRoute := continuityWire(t, transfer, "reliability.telemetry.ingest", "reliability.telemetry.index").Route
	if len(oldRoute) != len(newRoute) {
		t.Fatal("moving Telemetry rerouted its unchanged internal edge")
	}
	for i, point := range oldRoute {
		if point.X+float64(shift.X) != newRoute[i].X || point.Y+float64(shift.Y) != newRoute[i].Y {
			t.Fatalf("moving Telemetry displaced the internal route at point %d", i)
		}
	}
	requireStableBoardGeometry(t, d, clients, []string{"platform.telemetry.ingest", "platform.telemetry.index"}, [][2]string{{"platform.telemetry.ingest", "platform.telemetry.index"}})
	unaffected := []string{"clients.mobile", "clients.web", "clients.app"}
	wires := [][2]string{{"clients.mobile", "clients.app"}, {"clients.web", "clients.app"}}
	requireStableBoardGeometry(t, clients, transfer, unaffected, wires)
	requireStableBoardGeometry(t, transfer, archive, unaffected, wires)
	continuityWire(t, archive, "reliability.telemetry.archive.batch", "reliability.telemetry.archive.store")
}
