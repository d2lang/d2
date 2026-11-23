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

func TestVariableTweenExports(t *testing.T) {
	const source = `vars: {charge: 100%}
bar: "${charge}" {
  width: 100
  height: 40
  style.fill: "linear-gradient(to right, red ${charge}, transparent ${charge})"
}
steps: {empty: {vars: {charge: 0% {tween: 80ms}}}}
`
	dir := t.TempDir()
	out, err := imageCLI(t, dir, source, "--animate-interval=100", "--pad=0", "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{`offset="50%"`, ">50%</text>", "280ms steps(1,end) infinite"} {
		if !bytes.Contains(out, []byte(token)) {
			t.Fatalf("animated SVG missing %q", token)
		}
	}

	out, err = imageCLI(t, dir, source, "--animate-interval=100", "--pad=0", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	// The normal board holds retain 30 fps sampling. Two 40ms transition
	// samples appear between the normal 100ms holds.
	if !reflect.DeepEqual(decoded.Delay, []int{3, 3, 4, 4, 4, 3, 3, 4}) {
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
	full, halfway, empty := redPixels(decoded.Image[0]), redPixels(decoded.Image[4]), redPixels(decoded.Image[7])
	if full == 0 || halfway < full*4/10 || halfway > full*6/10 || empty != 0 {
		t.Fatalf("unexpected red fill coverage at start/midpoint/end: %d/%d/%d", full, halfway, empty)
	}

	iso, err := imageCLI(t, dir, source, "--isometric", "--scale=.15", "--animate-interval=100", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	isoGIF, err := gif.DecodeAll(bytes.NewReader(iso))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(isoGIF.Delay, []int{10, 4, 4, 10}) {
		t.Fatalf("isometric GIF delays=%v", isoGIF.Delay)
	}

	_, err = imageCLI(t, dir, source, "--target=steps.empty", "endpoint.svg")
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
