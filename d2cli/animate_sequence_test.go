package d2cli

import (
	"bytes"
	"image/gif"
	"strings"
	"testing"
)

func TestSequencePlaybackExport(t *testing.T) {
	const sequence = "shape: sequence_diagram\nclient -> api: request\napi -> client: reply"
	const configured = "vars: {d2-config: {animate-sequence: trace; animate-duration: 60ms; animate-interval: 100ms}}\n" + sequence
	for _, tc := range []struct {
		name, input string
		args        []string
		duration    string
		frames      int // negative means the trace must include intermediate samples
	}{
		{"ordinary sequence", sequence, nil, "", 0},
		{"standalone trace", "vars: {d2-config.animate-sequence: trace}\n" + sequence, nil, "3000ms", -1},
		{"source trace", configured, nil, "300ms", -1},
		{"flag reveal overrides trace", configured, []string{"--animate-sequence=reveal"}, "300ms", 3},
		{"flag disables automatic playback", "vars: {d2-config.animate-sequence: trace}\n" + sequence, []string{"--animate-sequence=off"}, "", 0},
		{"explicit zero interval", configured, []string{"--animate-interval=0"}, "", 0},
		{"explicit child", "vars: {d2-config.animate-sequence: reveal}\nlayers: {detail: {" + sequence + "}}", []string{"--target=layers.detail"}, "3000ms", 3},
		{"no messages", "vars: {d2-config.animate-sequence: trace}\nshape: sequence_diagram\nactor", nil, "", 0},
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
	const source = `vars: {d2-config: {animate-sequence: trace; animate-duration: 160ms}}
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
