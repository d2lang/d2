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

func TestTouchingPinnedModifiersHaveAnExteriorConnector(t *testing.T) {
	for _, tc := range []struct {
		shape, style, link string
		x, y, height       int
		peerStyle          string
		vertical           bool
	}{
		{"hexagon", "3d", "a -> b", 230, 121, 32, "", true},
		{"rectangle", "multiple", "b -> a", 320, 190, 32, "", false},
		{"hexagon", "3d", "b <-> a", 265, 68, 85, "; shape: hexagon; style.3d: true", true},
	} {
		source := fmt.Sprintf("a: A {shape: %s; left: 180; top: 160; width: 130; height: 90; style.%s: true}\nb: B {left: %d; top: %d; width: 52; height: %d%s}\n%s: active", tc.shape, tc.style, tc.x, tc.y, tc.height, tc.peerStyle, tc.link)
		diag := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		if len(diag.Connections) != 1 || diag.Connections[0].Label != "active" || len(diag.Connections[0].Route) != 4 {
			t.Fatalf("%s: missing exterior labeled route", tc.shape)
		}
		route := diag.Connections[0].Route
		if tc.vertical {
			if route[1].X != route[2].X || route[1].X >= 180 || route[0].Y != route[1].Y || route[2].Y != route[3].Y {
				t.Fatalf("vertical touching pair lacks a clear left lane: %v", route)
			}
		} else if route[1].Y != route[2].Y || route[1].Y <= 250 || route[0].X != route[1].X || route[2].X != route[3].X {
			t.Fatalf("horizontal touching pair lacks a clear lower lane: %v", route)
		}
		if _, err := d2svg.Render(diag, &d2svg.RenderOpts{}); err != nil {
			t.Fatal(err)
		}
	}
}
