package d2cli

import (
	"bytes"
	"image"
	"image/gif"
	"image/png"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestImplicitSequenceGIFAndStill(t *testing.T) {
	const source = `shape: sequence_diagram
style.animated: true
A -> B: Request {style.stroke: "#DB0018"}
B -> A: Reply {style.stroke: "#048013"}
`
	dir := t.TempDir()
	export := func(source, format string, extra ...string) []byte {
		t.Helper()
		args := append([]string{"--scale=.5", "--pad=4", "--stdout-format=" + format}, extra...)
		data, err := imageCLI(t, dir, source, append(args, "-")...)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	animation := export(source, "gif")
	g, err := gif.DecodeAll(bytes.NewReader(animation))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, delay := range g.Delay {
		total += delay
	}
	if total != 300 || len(g.Image) > 18 {
		t.Fatalf("two-message GIF has %dcs across %d frames, want 300cs with stationary holds", total, len(g.Image))
	}
	inks := func(frame image.Image) (red, green int) {
		for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
			for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
				r, g, b, _ := frame.At(x, y).RGBA()
				if r > 0xb000 && g < 0x6000 && b < 0x7000 {
					red++
				}
				if r < 0x6000 && g > r+0x3500 && g > b+0x3500 {
					green++
				}
			}
		}
		return red, green
	}
	if red, green := inks(g.Image[0]); red != 0 || green != 0 {
		t.Fatalf("starting diagram already shows message ink (%d red, %d green)", red, green)
	}
	if red, green := inks(animationFrameAt(t, animation, 80)); red == 0 || green != 0 {
		t.Fatalf("first message alone should be visible at 800ms (%d red, %d green)", red, green)
	}
	if red, green := inks(g.Image[len(g.Image)-1]); red == 0 || green == 0 {
		t.Fatalf("endpoint missing message ink (%d red, %d green)", red, green)
	}
	still, err := png.Decode(bytes.NewReader(export(source, "png")))
	if err != nil {
		t.Fatal(err)
	}
	if red, green := inks(still); red == 0 || green == 0 {
		t.Fatalf("static PNG missing message ink (%d red, %d green)", red, green)
	}
	multi := export(source+"steps: {next: {A -> B: Continue}}", "gif", "--animate-interval=100")
	if red, green := inks(animationFrameAt(t, multi, 0)); red == 0 || green == 0 {
		t.Fatalf("explicit board timeline should start with both authored messages (%d red, %d green)", red, green)
	}
}

func TestSequenceGIFContinuesAnimatedWireThroughLongHold(t *testing.T) {
	const source = `shape: sequence_diagram
style.animated: true
Client -> Server: Request {style.animated: true}
`
	for _, tc := range []struct {
		name, caption string
		interval      string
	}{
		{name: "explicit interval", interval: "3500"},
		{name: "reading time", caption: "Allow time to read this entire request before showing the exchange again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := source
			wantMS := 3500
			args := []string{"--scale=.4", "--pad=4", "--stdout-format=gif"}
			if tc.caption != "" {
				input += "annotation: " + tc.caption
				wantMS = (&d2target.Diagram{Root: d2target.Shape{Annotation: &d2target.Annotation{Label: tc.caption}}}).ReadingMS()
			} else {
				args = append(args, "--animate-interval="+tc.interval)
			}
			data, err := imageCLI(t, t.TempDir(), input, append(args, "-")...)
			if err != nil {
				t.Fatal(err)
			}
			animation, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			elapsed := 0
			var first *image.Paletted
			changed := false
			for i, delay := range animation.Delay {
				// The single-message clock normally finishes at two seconds.
				if elapsed >= 210 {
					if first == nil {
						first = animation.Image[i]
					} else if !bytes.Equal(first.Pix, animation.Image[i].Pix) {
						changed = true
					}
				}
				elapsed += delay
			}
			if !changed || elapsed != (wantMS+9)/10 {
				t.Fatalf("wire moved during extra hold = %v; GIF duration = %dcs, want %dms", changed, elapsed, wantMS)
			}
		})
	}
}
