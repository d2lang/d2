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
