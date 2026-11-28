package d2target

import (
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestRoundedWireWithShrinkingTerminalRun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		route []*geo.Point
	}{
		{"source", []*geo.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 200}}},
		{"destination", []*geo.Point{{X: 0, Y: 0}, {X: 0, Y: 200}, {X: 1, Y: 200}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := BaseConnection()
			c.BorderRadius = 10
			c.Route = tc.route
			from, to := c.ArrowheadAdjustmentsForWidths(4, 4)
			var last geo.Point
			for cmd := range c.PathCommands(from, to) {
				if cmd.Kind != 'M' && (cmd.End.X < last.X || cmd.End.Y < last.Y) {
					t.Fatalf("rounded wire backed up from %v to %v", last, cmd.End)
				}
				last = cmd.End
			}
		})
	}
}
