package d2svg_test

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestSequencePlaybackPaintAndMasks(t *testing.T) {
	t.Parallel()
	d := annotationDiagram()
	message := &d.Connections[0]
	message.Label, message.LabelWidth, message.LabelHeight = "message", 58, 18
	message.LabelPosition = "INSIDE_MIDDLE_CENTER"
	message.Opacity, message.SequenceStep, message.ZIndex = .5, 1, 2
	// The vertical lifeline crosses the message away from its label. Both
	// its crossing gap and the global label hole must wait for the message.
	line := *d2target.BaseConnection()
	line.ID, line.Stroke = "lifeline", "#000"
	line.Route = []*geo.Point{{X: 140, Y: -90}, {X: 140, Y: 150}}
	d.Connections = append(d.Connections, line)

	for _, tt := range []struct {
		name string
		opts *d2svg.RenderOpts
		play bool
	}{
		{"standalone", nil, true},
		{"static preview", &d2svg.RenderOpts{StaticSequence: true}, false},
		{"explicit board timeline", &d2svg.RenderOpts{MasterID: "d2-master"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data, err := d2svg.Render(d, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err := xml.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			out := string(data)
			const step = `data-d2-sequence-step="1"`
			if !tt.play {
				if strings.Contains(out, step) || strings.Contains(out, "-message-1") {
					t.Fatal("static view contains an independent message animation")
				}
				return
			}
			for _, want := range []string{
				`style='opacity:0.5'><g ` + step, // preserve authored opacity
				`<g ` + step + `><rect`,          // global label mask
				`<g class="annotation" ` + step,  // message annotation
				`fill="black" ` + step,           // hole in the crossed lifeline
				`@media screen and (prefers-reduced-motion:no-preference)`,
			} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %s", want)
				}
			}
		})
	}
}

func TestSequenceShapePlayback(t *testing.T) {
	t.Parallel()
	d := annotationDiagram()
	span := &d.Shapes[0]
	span.SequenceStep, span.SequenceAtArrival = 1, true
	span.SequenceGrowth = []d2target.SequenceExtent{{Step: 1, Height: 20}, {Step: 2, Height: 60}}
	span.Link, span.Tooltip = "https://example.com/span", "Activation"
	d.Shapes[1].SequenceStep = 2

	for _, tc := range []struct {
		name string
		opts *d2svg.RenderOpts
		play bool
	}{
		{"screen", nil, true},
		{"static", &d2svg.RenderOpts{StaticSequence: true}, false},
		{"explicit board", &d2svg.RenderOpts{MasterID: "d2-master"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := d2svg.Render(d, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			svg := string(out)
			for _, want := range []string{
				`data-d2-sequence-shape="1"`,
				`data-d2-sequence-start="2"`,
				`class="annotation" data-d2-sequence-complete="1"`,
				"transform:scaleY(0.333333)",
			} {
				if strings.Contains(svg, want) != tc.play {
					t.Errorf("playback = %v for %s", tc.play, want)
				}
			}
			if !strings.Contains(svg, "https://example.com/span") {
				t.Fatal("missing linked shape")
			}
		})
	}
}
