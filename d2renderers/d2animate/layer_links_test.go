package d2animate

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
)

func TestClickedLayerUsesItsOwnShape(t *testing.T) {
	const href = "root.layers.\"a & b\""
	anchor := svg.OpenElement("a", svg.HrefAttributes(href)...)
	external := svg.OpenElement("a", svg.HrefAttributes("https://example.org/")...)
	other := anchor + `<text>another link with the same href</text></a>`
	one := anchor + `<g class="YQ=="><rect/></g></a>`
	two := anchor + `<clipPath id="round"><rect/></clipPath><g class="Yg== animated-shape"><rect/></g></a>`
	frame := []byte("<g>" + one + two + other + external + `<g class="YQ=="><rect/></g></a></g>`)
	frames := [][]byte{frame, []byte(`<g/>`)}
	links := []layerLink{
		{from: 0, to: 1, shapeID: "a", href: href, legs: []frameLeg{{from: 0, to: 1}}, cameras: []string{"Aout", "Ain"}, duration: 1000},
		{from: 0, to: 1, shapeID: "b", href: href, legs: []frameLeg{{from: 0, to: 1}}, cameras: []string{"Bout", "Bin"}, duration: 1000},
	}
	var buf bytes.Buffer
	slots := []layerSlot{{&layerSequence{count: 1}, 0}, {&layerSequence{id: 1, count: 1}, 0}}
	out, links := bindLayerLinks(frames, links, "d2-test")
	writeLayerNavigation(&buf, "d2-test", links, slots, "", false)
	writeLayerTargets(&buf, "d2-test", links)
	got := string(out[0])
	for _, want := range []string{
		`href="#d2-layer-link-d2-test-0" xlink:href="#d2-layer-link-d2-test-0"><g class="YQ=="`,
		`href="#d2-layer-link-d2-test-1" xlink:href="#d2-layer-link-d2-test-1"><clipPath`,
		other, external,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing anchor %s", want)
		}
	}
	if !bytes.Equal(frame, frames[0]) || !bytes.Contains(frames[0], []byte(one)) || !bytes.Equal(out[1], frames[1]) {
		t.Fatal("navigation modified the input or an unrelated frame")
	}
	for _, want := range []string{
		`id="d2-layer-link-d2-test-0"`,
		"#d2-layer-link-d2-test-1:target~#d2-frame-d2-test-0",
		"#d2-layer-link-d2-test-1:target~#d2-frame-d2-test-1",
		"animation-play-state:paused!important",
		"animation:Aout var(--d2-click-time,1000ms) linear both}",
		"animation:Ain var(--d2-click-time,1000ms) linear}",
		"prefers-reduced-motion:reduce",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing navigation state %q", want)
		}
	}
}

func TestLayerLinksUnchangedOnUnsupportedMarkup(t *testing.T) {
	open := svg.OpenElement("a", svg.HrefAttributes("root.layers.x")...)
	targets := map[shapeAnchor]int{{open, "YQ=="}: 0}
	decoy := open + `<g class="YQ=="><rect/></g></a>`
	for _, input := range []string{
		"<g><svg>" + decoy + "</svg></g>",
		"<g><foreignObject><svg>" + decoy + "</svg></foreignObject></g>",
		"<g><defs>" + decoy + "</defs></g>",
		"<g><!--" + decoy + "--></g>",
		"<g><![CDATA[" + decoy + "]]></g>",
		`<g><a href="root.layers.x" target="_blank"><g class="YQ=="/></a></g>`,
		`<g><a href="root.layers.x" xlink:href="root.layers.x"><g class="YQ=="></a>`,
	} {
		if got := findLayerLinks([]byte(input), targets); len(got) != 0 {
			t.Errorf("matched unexpected anchors: %v", got)
		}
	}
}

func TestLayerLinksInCustomGroupWrappers(t *testing.T) {
	const href = "root.layers.x"
	open := svg.OpenElement("a", svg.HrefAttributes(href)...)
	frame := []byte(`<svg><g transform="scale(2)"><g>` + open + `<clipPath><g/></clipPath><g class="YQ=="></g><g class="YQ=="/></a></g></g></svg>`)
	matches := findLayerLinks(frame, map[shapeAnchor]int{{open, "YQ=="}: 1})
	if len(matches) != 1 || matches[0].route != 1 || string(frame[matches[0].start:matches[0].end]) != open {
		t.Fatalf("custom group wrapper lost its native shape anchor: %v", matches)
	}
}

func TestClickRoutesReuseKeyframesAcrossTweenSamples(t *testing.T) {
	child := &d2target.Diagram{Name: "inner"}
	root := &d2target.Diagram{
		Layers: []*d2target.Diagram{child},
		Shapes: []d2target.Shape{{ID: "portal", Link: "root.layers.inner", Pos: d2target.NewPoint(5, 10), Width: 50, Height: 60}},
	}
	sample := &d2target.Diagram{Shapes: append([]d2target.Shape(nil), root.Shapes...)}
	frames := []Frame{{Diagram: sample, TweenSample: true}, {Diagram: root}, {Diagram: child}}
	var css bytes.Buffer
	links := layerLinks(NewLayerNavigation(root, nil), frames, 1000)
	writeClickZooms(&css, links, "d2-test", image.Rect(0, 0, 150, 150), nil)
	if len(links) != 2 || links[0].from != 0 || links[1].from != 1 || links[0].to != 2 || links[1].to != 2 {
		t.Fatalf("unexpected click routes: %+v", links)
	}
	if got := strings.Count(css.String(), "@keyframes "); got != 2 {
		t.Errorf("emitted %d camera keyframes for one shared portal geometry, want 2", got)
	}
	sample.Shapes[0].Pos.X++
	css.Reset()
	writeClickZooms(&css, layerLinks(NewLayerNavigation(root, nil), frames, 1000), "d2-test", image.Rect(0, 0, 150, 150), nil)
	if got := strings.Count(css.String(), "@keyframes "); got != 4 {
		t.Errorf("emitted %d camera keyframes for two distinct portal positions, want 4", got)
	}
}

func TestClickedSiblingUsesBothParentPortals(t *testing.T) {
	left, right := &d2target.Diagram{Name: "left"}, &d2target.Diagram{Name: "right"}
	toRight := "root.layers.right"
	left.Shapes = []d2target.Shape{{ID: "shortcut", Link: toRight, Width: 15, Height: 20}}
	root := &d2target.Diagram{
		Layers: []*d2target.Diagram{left, right},
		Shapes: []d2target.Shape{
			{ID: "a", Link: "root.layers.left", Width: 40, Height: 35},
			{ID: "b", Link: toRight, Width: 80, Height: 50},
		},
	}
	nav := NewLayerNavigation(root, nil)
	frames := []Frame{{Diagram: root}, {Diagram: left}, {Diagram: right}}
	for _, link := range layerLinks(nav, frames, 900) {
		if link.shapeID != "shortcut" {
			continue
		}
		if len(link.legs) != 2 {
			t.Fatalf("got %d legs for sibling shortcut", len(link.legs))
		}
		out, in := link.legs[0], link.legs[1]
		if out.from != 1 || out.to != 0 || out.zoom.Inward || out.zoom.Focus != &root.Shapes[0] {
			t.Fatalf("sibling visit must first exit through the left portal: %+v", out)
		}
		if in.from != 0 || in.to != 2 || !in.zoom.Inward || in.zoom.Focus != &root.Shapes[1] {
			t.Fatalf("sibling visit must enter through the right portal: %+v", in)
		}
		return
	}
	t.Fatal("sibling shortcut missing")
}

func TestLinksWithoutCamerasSelectOnlyIncludedBoards(t *testing.T) {
	root := d2target.NewDiagram()
	step := &d2target.Diagram{Name: "two"}
	root.Steps = []*d2target.Diagram{step}
	root.Shapes = []d2target.Shape{
		{ID: "jump", Link: "root.steps.two", Width: 30, Height: 30},
		{ID: "self", Link: "root"},
		{ID: "outside", Link: "root.layers.missing", Width: 30, Height: 30},
		{ID: "url", Link: "https://example.com/", Width: 30, Height: 30},
	}
	frames := []Frame{{Diagram: root, DurationMS: 100}, {Diagram: step, DurationMS: 60}}
	links := layerLinks(NewLayerNavigation(root, nil), frames, 40)
	if len(links) != 2 || links[0].to != 1 || links[1].to != 0 || len(links[0].legs) != 0 || len(links[1].legs) != 0 {
		t.Fatalf("unexpected jumps: %+v", links)
	}
	svgs := [][]byte{[]byte(`<g viewBox="-100 10 50 60"><a href="root.steps.two" xlink:href="root.steps.two"><g class="anVtcA=="><rect/></g></a><a href="https://example.com/" xlink:href="https://example.com/"><g class="dXJs"><rect/></g></a></g>`), []byte(`<g viewBox="300 400 50 60"/>`)}
	on := true
	opts := d2svg.RenderOpts{AnimateProgress: &on}
	legacy, err := WrapTimed(root, svgs, opts, []int{100, 60})
	if err != nil {
		t.Fatal(err)
	}
	out, err := WrapTimeline(root, nil, frames, svgs, opts, 40)
	if err != nil {
		t.Fatal(err)
	}
	// An instant jump doesn't recenter the viewport or create a private step clock.
	header := func(svg []byte) string {
		a := strings.Index(string(svg), `class="d2-svg"`)
		return strings.SplitN(string(svg)[a:], ">", 2)[0]
	}
	if header(legacy) != header(out) || bytes.Contains(out, []byte("d2LayerStep-")) || !bytes.Contains(out, []byte("160ms steps(1,end) infinite")) {
		t.Fatal("links without cameras changed automatic playback or the world viewport")
	}
	if !bytes.Contains(out, []byte(`href="#d2-layer-link-`)) || !bytes.Contains(out, []byte(`href="https://example.com/"`)) {
		t.Fatal("internal and external anchor destinations were not preserved")
	}
	hash, _ := root.HashID(nil)
	if !bytes.Contains(out, []byte(".d2-layer-link-"+hash+":target~.d2-progress-"+hash+"{animation-play-state:paused!important}")) {
		t.Fatal("manual board navigation must pause the global progress clock")
	}
}
