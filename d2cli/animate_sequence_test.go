package d2cli

import (
	"bytes"
	"image"
	"image/gif"
	"strings"
	"testing"
)

func TestSequencePlaybackExport(t *testing.T) {
	const sequence = "shape: sequence_diagram\nclient -> api: request\napi -> client: reply"
	const configured = "vars: {d2-config: {animate-duration: 60ms; animate-interval: 100ms}}\n" + sequence
	for _, tc := range []struct {
		name, input string
		args        []string
		duration    string
		frames      int // negative means the trace must include intermediate samples
	}{
		{"ordinary sequence", sequence, nil, "", 0},
		{"ordinary flow with duration", "vars: {d2-config.animate-duration: 60ms}\na -> b", nil, "", 0},
		{"interval only", sequence, []string{"--animate-interval=100"}, "100ms", 1},
		{"standalone trace", "vars: {d2-config.animate-duration: 60ms}\n" + sequence, nil, "3000ms", -1},
		{"source trace", configured, nil, "300ms", -1},
		{"flag enables trace", sequence, []string{"--animate-duration=60"}, "3000ms", -1},
		{"zero duration override", "vars: {d2-config.animate-duration: 60ms}\n" + sequence, []string{"--animate-duration=0"}, "", 0},
		{"source zero duration", "vars: {d2-config.animate-duration: 0}\n" + sequence, nil, "", 0},
		{"explicit zero interval", configured, []string{"--animate-interval=0"}, "", 0},
		{"explicit child", "vars: {d2-config.animate-duration: 60ms}\nlayers: {detail: {" + sequence + "}}", []string{"--target=layers.detail"}, "3000ms", -1},
		{"no messages", "vars: {d2-config.animate-duration: 60ms}\nshape: sequence_diagram\nactor", nil, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := imageCLI(t, t.TempDir(), tc.input, append(tc.args, "--stdout-format=svg", "-")...)
			if err != nil {
				t.Fatal(err)
			}
			n := bytes.Count(out, []byte("@keyframes d2Transition-"))
			if (tc.frames == -1 && n <= 3) || (tc.frames >= 0 && n != tc.frames) || !strings.Contains(string(out), tc.duration) {
				t.Fatalf("got %d frames, want %d with duration %s", n, tc.frames, tc.duration)
			}
		})
	}
	// Fractional centiseconds from each row carry over to the following row.
	out, err := imageCLI(t, t.TempDir(), configured, "--animate-interval=101", "--stdout-format=gif", "--scale=.4", "-")
	if err != nil {
		t.Fatal(err)
	}
	anim, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, delay := range anim.Delay {
		total += delay
	}
	if total != 31 || len(anim.Image) < 4 {
		t.Fatalf("GIF has %d centiseconds and %d frames", total, len(anim.Image))
	}
}

func TestDefaultSequenceIntervalMatchesExplicitBoardClock(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 160ms}}
shape: sequence_diagram
a.style.fill: blue
a -> b: First
steps: {change: {
  a.style.fill: red
  b -> a: Second
}}`
	render := func(flags ...string) []byte {
		t.Helper()
		out, err := imageCLI(t, t.TempDir(), source, append(flags, "--stdout-format=svg", "-")...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if a, b := render(), render("--animate-interval=1000"); !bytes.Equal(a, b) {
		t.Fatal("implicit interval failed to preserve step interpolation")
	}
}

func TestSequenceStepKeepsPriorMessageVisible(t *testing.T) {
	const source = `vars: {d2-config: {animate-interval: 100ms; animate-duration: 80ms}}
shape: sequence_diagram
client -> api: Request
steps: {
  respond: {api -> client: Reply}
  clarify: {(api -> client)[0].label: Confirmed}
}`
	// Select each incoming step in isolation. The exported SVG must start with
	// the settled request already visible and then draw the added or edited reply.
	for _, test := range []struct{ step, message string }{{"respond", "Reply"}, {"clarify", "Confirmed"}} {
		t.Run(test.step, func(t *testing.T) {
			out, err := imageCLI(t, t.TempDir(), source, "--target=steps."+test.step, "--stdout-format=svg", "-")
			if err != nil {
				t.Fatal(err)
			}
			frames := bytes.Split(out, []byte(`<g style="animation: d2Transition-`))[1:]
			if len(frames) < 2 {
				t.Fatalf("want a traced reply, got %d frames", len(frames))
			}
			for i, frame := range frames {
				if !bytes.Contains(frame, []byte(">Request</text>")) {
					t.Fatalf("inherited request missing from frame %d", i)
				}
			}
			label := []byte(">" + test.message + "</text>")
			if bytes.Contains(frames[0], label) || !bytes.Contains(frames[len(frames)-1], label) {
				t.Fatalf("reply %q should appear during the step", test.message)
			}
		})
	}
}

func TestSequenceEditKeepsActorPositionInGIF(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 80ms; animate-interval: 120ms}}
shape: sequence_diagram
client: Client {style.fill: "#f000bc"}
client -> api: Request
steps: {answer: {api -> client: Reply}}`
	out, err := imageCLI(t, t.TempDir(), source, "--stdout-format=gif", "--scale=.5", "-")
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	actorPosition := func(frame image.Image) image.Point {
		t.Helper()
		bounds := frame.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, _ := frame.At(x, y).RGBA()
				if r>>8 > 220 && g>>8 < 30 && b>>8 > 160 {
					return image.Pt(x, y)
				}
			}
		}
		t.Fatal("could not find the client header")
		return image.Point{}
	}
	start := actorPosition(animation.Image[0])
	end := actorPosition(animation.Image[len(animation.Image)-1])
	if start != end {
		t.Errorf("adding a reply moved the client header from %v to %v", start, end)
	}
}
