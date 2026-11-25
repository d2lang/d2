package d2lib

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
)

func TestBoardFillWipeAcrossGrid(t *testing.T) {
	const src = `vars: {d2-config: {animate-duration: 400ms; animate-interval: 500ms}}
grid-columns: 4
grid-gap: 12
classes: {tile: {width: 100; height: 60; style: {fill: red; fill-transition: wipe-right}}}
a.class: tile
b.class: tile
c.class: tile
unchanged.class: tile
steps: {print: {a.style.fill: blue; b.style.fill: blue; c.style.fill: blue}}`
	d, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	s := d.Steps[0]
	if len(s.TweenFrames) != 24 {
		t.Fatalf("got %d frames for 400ms wipe", len(s.TweenFrames))
	}
	at := func(frame *d2target.Diagram, id string) *d2target.Shape { return gridEdgeShapeMap(frame.Shapes)[id] }
	mid := s.TweenFrames[len(s.TweenFrames)/2]
	if at(mid, "a").Fill != "blue" || at(mid, "c").Fill != "red" || at(mid, "unchanged").Fill != "red" {
		t.Fatalf("midpoint must reach the middle tile only: %v", mid.Shapes)
	}
	g, err := color.ParseGradient(at(mid, "b").Fill)
	if err != nil || g.Direction != "to right" || len(g.ColorStops) != 2 || g.ColorStops[0].Color != "blue" || g.ColorStops[1].Color != "red" || g.ColorStops[0].Position != "50%" || g.ColorStops[1].Position != "50%" {
		t.Fatalf("midpoint gradient = %+v, %v", g, err)
	}
	for _, f := range s.TweenFrames {
		for _, sf := range f.Shapes {
			final := at(s, sf.ID)
			if !sameSlot(&sf, final) || sf.Label != final.Label || sf.Color != final.Color || sf.Stroke != final.Stroke {
				t.Fatal("wipe moved a tile or its lettering")
			}
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if at(s.TweenFrames[0], id).Fill != "red" || at(s, id).Fill != "blue" {
			t.Fatalf("wrong endpoints for %s", id)
		}
	}
}

func TestBoardFillWipeDirectionsAndFallbacks(t *testing.T) {
	const src = `vars: {d2-config: {animate-duration: 160ms; animate-interval: 200ms}}
fill-transition: Sample {style.fill:red}
steps: {color: {fill-transition.style: {fill: blue; fill-transition: MODE}}}`
	for _, tc := range []struct{ mode, direction string }{{"wipe-left", "to left"}, {"wipe-up", "to top"}, {"wipe-down", "to bottom"}, {"wipe-right", "to right"}, {"blend", ""}} {
		t.Run(tc.mode, func(t *testing.T) {
			d, _ := compileContinuity(t, strings.Replace(src, "MODE", tc.mode, 1), nil)
			s := d.Steps[0]
			found := false
			for _, f := range s.TweenFrames {
				if f.Shapes[0].ID != "fill-transition" {
					t.Fatalf("object ID changed: %q", f.Shapes[0].ID)
				}
				if color.IsGradient(f.Shapes[0].Fill) {
					found = true
					g, err := color.ParseGradient(f.Shapes[0].Fill)
					if err != nil || g.Direction != tc.direction {
						t.Fatalf("unexpected paint %s", f.Shapes[0].Fill)
					}
				}
			}
			if found != (tc.mode != "blend") {
				t.Fatalf("directional gradient found = %v", found)
			}
		})
	}
	for _, fill := range []string{`"linear-gradient(red, green)"`, "null"} {
		d, _ := compileContinuity(t, strings.Replace(strings.Replace(src, "MODE", "wipe-right", 1), "fill: blue", "fill: "+fill, 1), nil)
		for _, f := range d.Steps[0].TweenFrames {
			if strings.Contains(f.Shapes[0].Fill, "linear-gradient(to right") {
				t.Fatalf("rewrote authored or theme fill: %s", f.Shapes[0].Fill)
			}
		}
	}
}

func TestFillTransitionDoesNotChangeStaticOutput(t *testing.T) {
	const src = `swatch: {style.fill: blue}`
	plain, _ := compileContinuity(t, src, nil)
	opted, _ := compileContinuity(t, src+"\nswatch.style.fill-transition: wipe-down", nil)
	a, err := plain.HashID(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := opted.HashID(nil)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("a transition preference changed the static render hash")
	}
}

func TestWipeRunsStopAtMissingTilesAndWideGaps(t *testing.T) {
	const src = `vars: {d2-config: {animate-duration: 200ms; animate-interval: 250ms}}
grid-columns: 3
grid-gap: GAP
classes: {tile: {width: 100; height: 60; style: {fill: red; fill-transition: wipe-left}}}
first.class: tile
middle.class: tile
last.class: tile
steps: {paint: {first.style.fill: blue; last.style.fill: blue; MIDDLE}}`
	for _, tc := range []struct{ name, gap, middle string }{
		{"unchanged tile between", "8", ""},
		{"distant cells", "140", "middle.style.fill: blue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.NewReplacer("GAP", tc.gap, "MIDDLE", tc.middle).Replace(src)
			d, _ := compileContinuity(t, input, nil)
			frames := d.Steps[0].TweenFrames
			shapes := gridEdgeShapeMap(frames[len(frames)/2].Shapes)
			for _, id := range []string{"first", "last"} {
				g, err := color.ParseGradient(shapes[id].Fill)
				if err != nil || g.ColorStops[0].Position != "50%" {
					t.Fatalf("%s waited for a distant tile, fill = %q", id, shapes[id].Fill)
				}
			}
		})
	}
}
