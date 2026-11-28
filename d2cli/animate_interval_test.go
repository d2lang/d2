package d2cli

import (
	"bytes"
	"image/gif"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnimateIntervalSourcePrecedence(t *testing.T) {
	const source = "vars: {d2-config: {animate-interval: 80ms}}\na\nsteps: {next: {b}}"
	for _, tc := range []struct {
		name      string
		env, args []string
		svgTiming string
	}{
		{"source", nil, nil, "160ms steps(1,end) infinite"},
		{"environment", []string{"D2_ANIMATE_INTERVAL=120"}, nil, "240ms steps(1,end) infinite"},
		{"flag over environment", []string{"D2_ANIMATE_INTERVAL=120"}, []string{"--animate-interval=50"}, "100ms steps(1,end) infinite"},
		{"explicit zero flag", []string{"D2_ANIMATE_INTERVAL=120"}, []string{"--animate-interval=0"}, ""},
		{"explicit zero environment", []string{"D2_ANIMATE_INTERVAL=0"}, nil, ""},
		{"selected board", nil, []string{"--target=steps.next"}, "80ms steps(1,end) infinite"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := imageCLIWithEnv(t, dir, source, tc.env, append(tc.args, "out.svg")...)
			if err != nil {
				t.Fatal(err)
			}
			path := "out.svg"
			if tc.svgTiming == "" {
				path = filepath.Join("out", "index.svg")
			}
			data, err := os.ReadFile(filepath.Join(dir, path))
			if err != nil {
				t.Fatal(err)
			}
			if tc.svgTiming == "" {
				if bytes.Contains(data, []byte("@keyframes d2Transition")) {
					t.Fatal("zero interval produced an animated SVG")
				}
			} else if !bytes.Contains(data, []byte(tc.svgTiming)) {
				t.Fatalf("animated SVG missing %q", tc.svgTiming)
			}
		})
	}
}

func TestAnimateIntervalSourceGIF(t *testing.T) {
	const source = "vars: {d2-config: {animate-interval: 80ms}}\na\nsteps: {next: {b}}"
	for _, tc := range []struct {
		name       string
		args       []string
		durationCS int
	}{
		{"source", nil, 16},
		{"zero uses GIF default", []string{"--animate-interval=0", "--scale=.15"}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := imageCLI(t, t.TempDir(), source, append(tc.args, "--stdout-format=gif", "-")...)
			if err != nil {
				t.Fatal(err)
			}
			animation, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			var total int
			for _, delay := range animation.Delay {
				total += delay
			}
			if total != tc.durationCS {
				t.Fatalf("GIF duration = %d centiseconds, want %d", total, tc.durationCS)
			}
		})
	}
}

func TestAnimateIntervalSourceUnsupportedFormat(t *testing.T) {
	const source = "vars: {d2-config: {animate-interval: 80ms}}\na"
	for _, args := range [][]string{
		{"out.png"},
		{"--stdout-format=png", "-"},
	} {
		data, err := imageCLI(t, t.TempDir(), source, args...)
		if err == nil || !strings.Contains(err.Error(), "animate-interval") || len(data) != 0 {
			t.Errorf("%v: got %d bytes, %v", args, len(data), err)
		}
	}
}

func TestNegativeAnimationInterval(t *testing.T) {
	for _, format := range []string{"svg", "gif", "mp4", "webm"} {
		data, err := imageCLI(t, t.TempDir(), "a\nsteps: {next: {b}}", "--animate-interval=-1", "--stdout-format="+format, "-")
		if err == nil || !strings.Contains(err.Error(), "--animate-interval") || len(data) != 0 {
			t.Errorf("%s: got %d bytes, %v", format, len(data), err)
		}
	}
}
