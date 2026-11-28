package d2animate

import (
	"bytes"
	"math"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestLayerZoomPlayback(t *testing.T) {
	root, child := d2target.NewDiagram(), d2target.NewDiagram()
	child.Name = "detail"
	root.Layers = []*d2target.Diagram{child}
	root.Shapes = []d2target.Shape{{ID: "gateway", Type: "rectangle", Link: "root.layers.detail", Width: 40, Height: 20}}
	frames, err := Timeline(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	svgs := [][]byte{[]byte(`<g><a href="root.layers.detail" xlink:href="root.layers.detail"><g class="Z2F0ZXdheQ=="><rect id="outer-artwork"/></g></a></g>`), []byte(`<g><rect id="inner-artwork"/></g>`)}
	for _, tc := range []struct {
		name       string
		duration   int
		total      string
		transition bool
	}{
		{"positive duration", 40, "200ms", true},
		{"motion extends outgoing slot", 200, "400ms", true},
		{"disabled motion", 0, "200ms", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := WrapTimeline(root, nil, frames, svgs, d2svg.RenderOpts{}, tc.duration)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(out, []byte(tc.total+" steps(1,end) infinite")) {
				t.Errorf("missing timeline duration %s", tc.total)
			}
			if !bytes.Contains(out, []byte(`href="#d2-layer-link-`)) {
				t.Fatal("missing manual link during timed playback")
			}
			if tc.duration == 0 && bytes.Contains(out, []byte("@keyframes d2-click-")) {
				t.Fatal("zero-duration navigation emitted unused camera keyframes")
			}
			if got := bytes.Contains(out, []byte("@media (prefers-reduced-motion:no-preference){@keyframes d2Transition-")); got != tc.transition {
				t.Errorf("linked layers have camera keyframes = %v", got)
			}
			for _, name := range []string{"outer-artwork", "inner-artwork"} {
				if n := bytes.Count(out, []byte(name)); n != 1 {
					t.Errorf("found %d copies of %s", n, name)
				}
			}
			if tc.duration == 40 {
				// Root holds until 60ms, moves in over 40ms, returns over
				// the final 40ms of the loop, and ends at identity.
				for _, pose := range []string{"30%{transform:matrix(1,0,0,1,0,0);opacity:1", "80%{transform:matrix(1,0,0,1,0,0);opacity:1", "100%{transform:matrix(1,0,0,1,0,0);opacity:1"} {
					if !bytes.Contains(out, []byte(pose)) {
						t.Errorf("missing %s", pose)
					}
				}
			}
		})
	}
	out, err := WrapTimeline(root, nil, frames, [][]byte{[]byte("<g/>"), svgs[1]}, d2svg.RenderOpts{}, 40)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("200ms steps(1,end) infinite")) || bytes.Contains(out, []byte("d2-layer-link-")) {
		t.Fatal("a raw fragment without a portal needs a complete tour and no synthetic click route")
	}
	if frames[0].DurationMS != 100 || frames[1].DurationMS != 100 {
		t.Fatal("composing the animation mutated the original timeline")
	}
}

func TestLayerStepTimelines(t *testing.T) {
	root, child, peer := d2target.NewDiagram(), d2target.NewDiagram(), d2target.NewDiagram()
	outerStep, innerStep, sample := d2target.NewDiagram(), d2target.NewDiagram(), d2target.NewDiagram()
	child.Name, peer.Name = "child", "peer"
	r := d2target.Shape{ID: "portal", Width: 10, Height: 10, Link: "root.layers.child"}
	c := d2target.Shape{ID: "back", Width: 10, Height: 10, Link: "root"}
	root.Shapes, child.Shapes = []d2target.Shape{r}, []d2target.Shape{c}
	outerStep.Shapes, innerStep.Shapes, sample.Shapes = []d2target.Shape{r}, []d2target.Shape{c}, []d2target.Shape{c}
	root.Layers = []*d2target.Diagram{child, peer}
	root.Steps = []*d2target.Diagram{outerStep}
	child.Steps = []*d2target.Diagram{innerStep}
	innerStep.TweenFrames, innerStep.TweenDurationMS = []*d2target.Diagram{sample}, 50
	frames, err := Timeline(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	nav := NewLayerNavigation(root, nil)
	links := layerLinks(nav, frames, 25)
	slots := layerSlots(nav, frames, links)
	// Root, child, its tween and step, unlinked peer, root step.
	for i, expected := range []struct {
		lane, start, total int
	}{
		{0, 0, 300}, {1, 0, 200}, {1, 100, 200}, {1, 150, 200}, {0, 100, 300}, {0, 200, 300},
	} {
		if got := slots[i]; got.id != expected.lane || got.startMS != expected.start || got.totalMS != expected.total {
			t.Errorf("frame %d: got %+v start %d, want %+v", i, *got.layerSequence, got.startMS, expected)
		}
	}
	if len(links) != 5 || links[len(links)-1].from != 5 || links[len(links)-1].to != 1 {
		t.Fatalf("expected portals in both layers' authored steps and sample, got %+v", links)
	}
}

func TestLayerZoomPlaybackGuards(t *testing.T) {
	root, child, sample := d2target.NewDiagram(), d2target.NewDiagram(), d2target.NewDiagram()
	child.Name = "child"
	root.Layers = []*d2target.Diagram{child}
	root.Shapes = []d2target.Shape{{ID: "portal", Width: 10, Height: 10, Link: "root.layers.child"}}
	child.TweenFrames = []*d2target.Diagram{sample}
	child.TweenDurationMS = 10
	frames, err := Timeline(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	nav := NewLayerNavigation(root, nil)
	zooms := playbackZooms(nav, frames)
	if len(zooms) != 1 || zooms[len(frames)-1][0].zoom.Inward {
		t.Fatalf("only the return from the authored endpoint should zoom, got %+v", zooms)
	}

	for _, check := range []struct {
		name     string
		frames   []Frame
		duration int
	}{
		{"missing diagram", []Frame{{Diagram: nil, DurationMS: 100}}, 10},
		{"invalid slot", []Frame{{Diagram: root, DurationMS: 0}, {Diagram: child, DurationMS: 100}}, 100},
		{"overflow", []Frame{{Diagram: root, DurationMS: math.MaxInt}, {Diagram: child, DurationMS: 1}}, 10},
		{"invalid duration", []Frame{{Diagram: root, DurationMS: 100}}, -1},
	} {
		t.Run(check.name, func(t *testing.T) {
			svgs := make([][]byte, len(check.frames))
			_, err := WrapTimeline(root, nil, check.frames, svgs, d2svg.RenderOpts{}, check.duration)
			if err == nil {
				t.Fatalf("expected timeline validation error, got %v", err)
			}
		})
	}
}

func TestTransitUsesExistingFramesAndOneWindow(t *testing.T) {
	portal := func(id, link string) d2target.Shape {
		return d2target.Shape{ID: id, Link: link, Width: 20, Height: 20}
	}
	a := &d2target.Diagram{Name: "a", Shapes: []d2target.Shape{portal("deep", "root.layers.a.layers.deep")}}
	deep, b := &d2target.Diagram{Name: "deep"}, &d2target.Diagram{Name: "b"}
	a.Layers = []*d2target.Diagram{deep}
	root := &d2target.Diagram{
		Layers: []*d2target.Diagram{a, b},
		Shapes: []d2target.Shape{portal("a", "root.layers.a"), portal("b", "root.layers.b")},
	}
	frames, err := Timeline(root, 1000)
	if err != nil {
		t.Fatal(err)
	}
	nav := NewLayerNavigation(root, nil)
	if routes := playbackZooms(nav, []Frame{frames[0], frames[2], frames[3]}); len(routes[0]) != 0 || len(routes[1]) != 0 {
		t.Fatal("a subset missing its intermediate board must cut")
	}
	svgs := [][]byte{[]byte(`<g><rect id="overview-art"/></g>`), []byte(`<g><rect id="a-art"/></g>`), []byte(`<g><rect id="deep-art"/></g>`), []byte(`<g><rect id="b-art"/></g>`)}
	for _, duration := range []int{300, 1} {
		out, err := WrapTimeline(root, nil, frames, svgs, d2svg.RenderOpts{}, duration)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(out, []byte("4000ms steps(1,end) infinite")) || bytes.Count(out, []byte(`id="overview-art"`)) != 1 || bytes.Count(out, []byte(`id="a-art"`)) != 1 {
			t.Fatal("branch movement duplicated artwork or extended the tour")
		}
		if duration == 300 {
			// Three equal hops occupy 2700..3000ms of the original 4s loop.
			for _, midpoint := range []string{"70%", "72.5%"} {
				if !bytes.Contains(out, []byte(midpoint+"{transform:matrix(1,0,0,1,0,0);opacity:1")) {
					t.Fatalf("missing full intermediate artwork at %s", midpoint)
				}
			}
		}
	}
}
