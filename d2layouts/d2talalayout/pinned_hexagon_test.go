package d2talalayout_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestPinned3DHexagonKeepsShortVerticalConnector(t *testing.T) {
	for _, link := range []string{"a -> b", "b -> a", "a <- b", "b <- a"} {
		t.Run(link, func(t *testing.T) {
			source := fmt.Sprintf("a: A {shape: hexagon; left: 180; top: 160; width: 130; height: 90; style.3d: true}\nb: B {left: 240; top: 120; width: 52; height: 32}\n%s: phase", link)
			diag := compileRoutingDiagram(t, t.Context(), source, 1, &d2svg.RenderOpts{})
			if len(diag.Connections) != 1 {
				t.Fatalf("got %d connections, want one", len(diag.Connections))
			}
			c := diag.Connections[0]
			route := c.Route
			if len(route) != 2 {
				t.Fatalf("route %v, want a straight connector", route)
			}
			top, bottom := route[0].Y, route[1].Y
			if top > bottom {
				top, bottom = bottom, top
			}
			if top != 152 || bottom != 160-d2target.THREE_DEE_OFFSET/2 || route[0].X != route[1].X {
				t.Fatalf("route from %v to %v misses painted border", route[0], route[1])
			}
			svg, err := d2svg.Render(diag, &d2svg.RenderOpts{})
			if err != nil || strings.Contains(string(svg), "NaN") {
				t.Fatalf("invalid rendered geometry for %s: %v", link, err)
			}
		})
	}
}
