package d2cli

import (
	"bytes"
	"image"
	"image/gif"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAnimateDurationExports(t *testing.T) {
	const source = `vars: {charge: 100%; d2-config: {animate-duration: 80ms; animate-interval: 100ms}}
bar: "${charge}" {
  width: 100
  height: 40
  style.fill: "linear-gradient(to right, red ${charge}, transparent ${charge})"
}
steps: {empty: {vars: {charge: 0%}}}
`
	dir := t.TempDir()
	out, err := imageCLI(t, dir, source, "--pad=0", "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{`offset="50%"`, ">50%</text>", "200ms steps(1,end) infinite"} {
		if !bytes.Contains(out, []byte(token)) {
			t.Fatalf("animated SVG missing %q", token)
		}
	}

	out, err = imageCLI(t, dir, source, "--pad=0", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	// The step spends 80ms draining and 20ms displaying the empty bar.
	if !reflect.DeepEqual(decoded.Delay, []int{3, 3, 4, 4, 4, 2}) {
		t.Fatalf("GIF delays=%v", decoded.Delay)
	}

	// Check the rendered gradient as well as the schedule: the center sample
	// should contain roughly half as many red pixels as the start.
	redPixels := func(frame *image.Paletted) int {
		count := 0
		for _, idx := range frame.Pix {
			r, g, b, _ := frame.Palette[idx].RGBA()
			if r > 0xe000 && g < 0x4000 && b < 0x4000 {
				count++
			}
		}
		return count
	}
	full, halfway, empty := redPixels(decoded.Image[0]), redPixels(decoded.Image[4]), redPixels(decoded.Image[len(decoded.Image)-1])
	if full == 0 || halfway < full*4/10 || halfway > full*6/10 || empty != 0 {
		t.Fatalf("unexpected red fill coverage at start/midpoint/end: %d/%d/%d", full, halfway, empty)
	}

	iso, err := imageCLI(t, dir, source, "--isometric", "--scale=.15", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	isoGIF, err := gif.DecodeAll(bytes.NewReader(iso))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(isoGIF.Delay, []int{10, 4, 4, 2}) {
		t.Fatalf("isometric GIF delays=%v", isoGIF.Delay)
	}

	_, err = imageCLI(t, dir, source, "--animate-interval=0", "--target=steps.empty", "endpoint.svg")
	if err != nil {
		t.Fatal(err)
	}
	static, err := os.ReadFile(filepath.Join(dir, "endpoint.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(static), ">0%</text>") || strings.Contains(string(static), ">50%</text>") {
		t.Fatal("static export should render only the endpoint")
	}
}

func TestAnimateDurationOverrides(t *testing.T) {
	const source = `vars: {charge: 100%; d2-config: {animate-duration: 80ms; animate-interval: 100ms}}
bar: "${charge}"
steps: {empty: {vars: {charge: 0%}}}`
	for _, tc := range []struct {
		name      string
		env, args []string
		timing    string
		midpoint  bool
	}{
		{"source", nil, nil, "200ms steps(1,end) infinite", true},
		{"environment", []string{"D2_ANIMATE_DURATION=160"}, nil, "260ms steps(1,end) infinite", true},
		{"flag over environment", []string{"D2_ANIMATE_DURATION=0"}, []string{"--animate-duration=80"}, "200ms steps(1,end) infinite", true},
		{"disable with flag", []string{"D2_ANIMATE_DURATION=160"}, []string{"--animate-duration=0"}, "200ms infinite", false},
		{"disable with environment", []string{"D2_ANIMATE_DURATION=0"}, nil, "200ms infinite", false},
		{"single board", nil, []string{"--target=steps.empty"}, "100ms infinite", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := isometricCLI(t, source, tc.env, append(tc.args, "--stdout-format=svg", "-")...)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(out, []byte(tc.timing)) || bytes.Contains(out, []byte(">50%</text>")) != tc.midpoint {
				t.Errorf("expected timing %q, midpoint = %v", tc.timing, tc.midpoint)
			}
		})
	}
}

func TestAnimateDurationAloneKeepsSeparateSVGBoards(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 80ms}; charge: 100%}
bar: "${charge}"
steps: {empty: {vars: {charge: 0%}}}`
	_, dir, err := isometricCLI(t, source, nil, "out.svg")
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "out", "index.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(">100%</text>")) || bytes.Contains(out, []byte("d2Transition-")) {
		t.Fatal("expected the static root board")
	}
}

func TestAnimateDurationSolidFill(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 80ms; animate-interval: 120ms}}
swatch: "" {width: 100; height: 40; style: {fill: blue; stroke-width: 0}}
steps: {alert: {swatch.style.fill: red}}`
	dir := t.TempDir()
	svg, err := imageCLI(t, dir, source, "--pad=0", "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(svg, []byte(`fill="#800080"`)) {
		t.Fatal("the SVG swatch did not pass through purple")
	}

	out, err := imageCLI(t, dir, source, "--pad=0", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	// At 160ms the root has held for 120ms and the 80ms color change is halfway.
	elapsed := 0
	for i, delay := range animation.Delay {
		if elapsed <= 16 && 16 < elapsed+delay {
			frame := animation.Image[i]
			r, g, b, _ := frame.At(animation.Config.Width/2, animation.Config.Height/2).RGBA()
			if r>>8 != 128 || g != 0 || b>>8 != 128 {
				t.Fatalf("GIF swatch at 160ms = #%02x%02x%02x, want #800080", r>>8, g>>8, b>>8)
			}
			return
		}
		elapsed += delay
	}
	t.Fatal("GIF ended before the color transition reached its midpoint")
}
