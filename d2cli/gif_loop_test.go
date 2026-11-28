package d2cli

import (
	"bytes"
	"image/gif"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestGIFIconLoopTiming(t *testing.T) {
	const quarter = "direction: right\na -> b: {icon.style.animated: 0.25/s}"
	const steps = quarter + "\nsteps.second: {(a -> b)[0].style.stroke: green}"
	for _, tt := range []struct {
		name     string
		source   string
		interval string
		wantMS   int
	}{
		{"sparse stream", quarter, "", 4000},
		{"one timed icon", "a -> b: {icon.style.animated: 200ms}", "", 200},
		{"separate timed and rate-based connections", "a -> b: {icon.style.animated: 200ms}\na -> c: {icon.style.animated: 2/s}", "", 1000},
		{"empty legend entries", "a -> b: {icon.style.animated: 2/s}\nvars.d2-legend: {invisible: \"\" {style.animated: true}}", "", 500},
		{"different rates", quarter + "\nc -> d: {icon.style.animated: 0.6/s}", "", 20000},
		{"exact authored period", quarter, "4000", 4000},
		{"longer authored capture", quarter, "4500", 4500},
		{"default step holds extend", steps, "", 4000},
		{"whole film includes both boards", steps, "2000", 4000},
		{"reading hold reaches encoder", "annotation: This caption matters\na -> b: {icon.style.animated: 1/s}", "1000", 1860},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"--scale=.4", "--pad=4", "--stdout-format=gif"}
			if tt.interval != "" {
				args = append(args, "--animate-interval="+tt.interval)
			}
			data, err := imageCLI(t, t.TempDir(), tt.source, append(args, "-")...)
			if err != nil {
				t.Fatal(err)
			}
			animation, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			ms := 0
			for _, cs := range animation.Delay {
				ms += cs * 10
			}
			if ms != tt.wantMS {
				t.Errorf("encoded loop lasts %dms, want %dms", ms, tt.wantMS)
			}
		})
	}
	for _, src := range []string{quarter, steps} {
		interval := "3999"
		if src == steps {
			interval = "1999"
		}
		_, err := imageCLI(t, t.TempDir(), src, "--stdout-format=gif", "--animate-interval="+interval, "-")
		if err == nil || !strings.Contains(err.Error(), "icon") {
			t.Errorf("short capture %sms: got %v", interval, err)
		}
	}
}

func TestGIFAutoLoopUsesRouteAndPaintPeriods(t *testing.T) {
	board := animatedConnectionRasterDiagram()
	wire := &board.Connections[0]
	wire.SrcArrow, wire.DstArrow = d2target.NoArrowhead, d2target.NoArrowhead
	wire.Animated = false
	// The 2px stroke trims a unit off each end of this 96-unit route.
	wire.Route = []*geo.Point{{}, {X: 96}}
	wire.IconAnimation = &d2target.ConnectionIconAnimation{}
	if ms, err := automaticGIFLoopMS(board); err != nil || ms != 520 {
		t.Fatalf("one 94-unit journey: %dms, %v", ms, err)
	}
	wire.IconAnimation.Rate = new(100.)
	if _, err := automaticGIFLoopMS(board); err != nil {
		t.Fatalf("a 100/s stream is valid even when every GIF tick aliases: %v", err)
	}
	wire.IconAnimation.Rate = new(2.)
	if ms, err := automaticGIFLoopMS(board); err != nil || ms != 500 {
		t.Fatalf("two departures per second: %dms, %v", ms, err)
	}
	board.Shapes[0].Animated = true
	if ms, err := automaticGIFLoopMS(board); err != nil || ms != 1000 {
		t.Fatalf("500ms departures and 1s pulse: %dms, %v", ms, err)
	}
	board.Shapes[0].Animated = false
	wire.Animated, wire.StrokeWidth, wire.StrokeDash = true, 1, 15 // one visible dash period lasts 750ms
	if ms, err := automaticGIFLoopMS(board); err != nil || ms != 1500 {
		t.Fatalf("500ms departures and 750ms dash period: %dms, %v", ms, err)
	}
	wire.Animated = false
	wire.IconAnimation.Rate = new(.01)
	if _, err := automaticGIFLoopMS(board); err == nil || !strings.Contains(err.Error(), "SVG") {
		t.Fatalf("unrepresentable 100s loop: %v", err)
	}
	wire.IconAnimation.Rate = new(.25)
	second := *wire
	second.ID = "other"
	second.IconAnimation = &d2target.ConnectionIconAnimation{Rate: new(.042)}
	board.Connections = append(board.Connections, second)
	if _, err := automaticGIFLoopMS(board); err == nil || !strings.Contains(err.Error(), "set animate-interval") {
		t.Fatalf("periods without an affordable common return: %v", err)
	}
}

func TestGIFAutoLoopSamplesDistinctIconPhases(t *testing.T) {
	const source = "direction: right\na -> b: {icon.style.animated: 50/s; style.stroke: \"#CC1122\"}"
	data, err := imageCLI(t, t.TempDir(), source, "--scale=.6", "--pad=4", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// A 40ms GIF would sample at 0 and 20ms, always at the start of
	// a 50/s departure. A 60ms GIF samples the second frame halfway through.
	if len(animation.Image) != 2 || animation.Delay[0] != 3 || animation.Delay[1] != 3 {
		t.Fatalf("50/s frame delays = %v, want [3 3]", animation.Delay)
	}
	first, second := animation.Image[0], animation.Image[1]
	for y := first.Rect.Min.Y; y < first.Rect.Max.Y; y++ {
		for x := first.Rect.Min.X; x < first.Rect.Max.X; x++ {
			r, _, _, _ := first.At(x, y).RGBA()
			s, _, _, _ := second.At(x, y).RGBA()
			if max(r, s)-min(r, s) > 20*257 {
				return
			}
		}
	}
	t.Fatal("GIF sampled the same visible particle positions twice")
}

func TestGIFAutoLoopSamplesTheEncodedDashClock(t *testing.T) {
	const wire = "direction: right\na -> b: {style.animated: true; style.stroke-width: 1; style.stroke-dash: 1}"
	render := func(source string) []byte {
		t.Helper()
		data, err := imageCLI(t, t.TempDir(), source, "--scale=2", "--stdout-format=gif", "-")
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	// The 50ms dash cycle has two samples, at 0 and 20ms in the GIF.
	// An inactive icon must not change the phase of the second sample.
	if !bytes.Equal(render(wire), render(wire+"\n(a -> b)[0].icon.style.animated: 0/s")) {
		t.Fatal("inactive icon changed the dash animation")
	}
}
