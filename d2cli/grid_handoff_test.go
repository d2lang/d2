package d2cli

import (
	"bytes"
	"image"
	"image/gif"
	"testing"
)

func TestGridHandoffNativeExports(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 800ms; animate-interval: 1000ms}}
grid-columns: 2
anchor: writer {width: 124; height: 54}
classes: {writer: {width: 124; height: 54; style: {font: mono; font-size: 20; fill: "#00FF99"; font-color: black; stroke-width: 0}}}
a: pg-03 {class: writer}
anchor -> a
steps: {promote: {a: null; b: pg-09 {class: writer}; anchor -> b}}`
	for _, format := range []string{"svg", "gif"} {
		t.Run(format, func(t *testing.T) {
			out, err := imageCLI(t, t.TempDir(), source, "--pad=4", "--stdout-format="+format, "-")
			if err != nil {
				t.Fatal(err)
			}
			if format == "svg" {
				for _, run := range []string{">pg-0</text>", ">3</text>", ">9</text>"} {
					if !bytes.Contains(out, []byte(run)) {
						t.Fatalf("missing intermediate text run %s", run)
					}
				}
				return
			}
			g, err := gif.DecodeAll(bytes.NewReader(out))
			if err != nil {
				t.Fatal(err)
			}
			var slot, beforeInk image.Rectangle
			moved := false
			for i, frame := range g.Image {
				var fill image.Rectangle
				for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
					for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
						r, green, b, _ := frame.At(x, y).RGBA()
						if r < 0x2000 && green > 0xe000 && b > 0x7000 && b < 0xb000 {
							fill = fill.Union(image.Rect(x, y, x+1, y+1))
						}
					}
				}
				if i == 0 {
					slot = fill
				}
				if fill.Empty() || fill != slot {
					t.Fatalf("slot fill moved in GIF frame %d: %v, initial %v", i, fill, slot)
				}
				var ink image.Rectangle
				for y := slot.Min.Y; y < slot.Max.Y; y++ {
					for x := (slot.Min.X + slot.Max.X) / 2; x < slot.Max.X; x++ {
						r, green, b, _ := frame.At(x, y).RGBA()
						if r < 0x6000 && green < 0x6000 && b < 0x6000 {
							ink = ink.Union(image.Rect(x, y, x+1, y+1))
						}
					}
				}
				if i == 0 {
					beforeInk = ink
				}
				if !ink.Empty() && (ink.Min.Y < beforeInk.Min.Y-1 || ink.Max.Y > beforeInk.Max.Y+1) {
					moved = true
				}
			}
			if !moved {
				t.Fatal("GIF never rolled the changed suffix vertically")
			}
		})
	}
}
