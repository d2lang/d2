package d2talalayout_test

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestCompoundSeparatesFixedExteriorCaptions(t *testing.T) {
	const source = `Batch: {
  extract: Extract orders from all regions {width: 80; height: 60; label.near: outside-top-left}
  load: Load payments into the archive {width: 80; height: 60; label.near: outside-top-right}
}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	var captions []geo.Box
	for _, tile := range d.Shapes[1:] {
		body := geo.NewBox(geo.NewPoint(float64(tile.Pos.X), float64(tile.Pos.Y)), float64(tile.Width), float64(tile.Height))
		w, h := float64(tile.LabelWidth), float64(tile.LabelHeight)
		topLeft := label.FromString(tile.LabelPosition).GetPointOnBox(body, label.PADDING, w, h)
		captions = append(captions, *geo.NewBox(topLeft, w, h))
	}
	if len(captions) != 2 || captions[0].Overlaps(captions[1]) {
		t.Fatalf("exterior captions overlap or are missing: %v", captions)
	}
}

func TestCompoundFitsAuthoredExteriorIcon(t *testing.T) {
	for _, tc := range []struct {
		height, iconSize int
	}{{28, 28}, {60, 32}, {95, 48}, {160, 64}} {
		t.Run(fmt.Sprint(tc.height), func(t *testing.T) {
			source := fmt.Sprintf(`Deployment: {
  api: API {width: %d; height: %d; icon: "https://example.org/icon.svg"; icon.near: outside-right-center}
}`, tc.height+20, tc.height)
			d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
			frame, tile := d.Shapes[0], d.Shapes[1]
			if frame.Height != max(tile.Height, tc.iconSize+4*label.PADDING)+120 || frame.Width != tile.Width+120+tc.iconSize+3*label.PADDING {
				t.Fatalf("frame around %d px exterior icon is %d × %d", tc.iconSize, frame.Width, frame.Height)
			}
		})
	}
}

func TestCompoundKeepsCenteredTitleClearOfChild(t *testing.T) {
	for _, size := range []string{"", "height: 300", "width: 380"} {
		source := fmt.Sprintf("Pool: Runtime {label.near: center-center\n%s\nworker: Worker {width: 120; height: 80}\n}", size)
		d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		p, n := d.Shapes[0], d.Shapes[1]
		titleBottom := float64(p.Pos.Y) + float64(p.Height+p.LabelHeight)/2
		if n.Pos.Y < int(titleBottom)+label.PADDING || n.Pos.Y+n.Height > p.Pos.Y+p.Height {
			t.Fatalf("%s: tile %v does not fit below the centered title in %v", size, n.Pos, p.Pos)
		}
	}
}

func TestCompoundCircleHonorsAuthoredHeight(t *testing.T) {
	const source = `Zone: {shape: circle; height: 320
  tile: Window {width: 80; height: 60}
}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	p, n := d.Shapes[0], d.Shapes[1]
	if p.Width != 320 || p.Height != 320 || n.Width != 80 || n.Height != 60 {
		t.Fatalf("circle = %d × %d, tile = %d × %d", p.Width, p.Height, n.Width, n.Height)
	}
	if p.Pos.X*2+p.Width != n.Pos.X*2+n.Width || p.Pos.Y*2+p.Height != n.Pos.Y*2+n.Height {
		t.Fatalf("tile at %v is off center in circle at %v", n.Pos, p.Pos)
	}
}

func TestCenteredTitleUsesRoomInsideNeighboringFrames(t *testing.T) {
	const source = `Pool: Runtime {label.near: center-center
  worker: Worker {width: 120; height: 48}
}
Ledger`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	p, n := d.Shapes[0], d.Shapes[1]
	if p.Height != n.Height+120 {
		t.Fatalf("frame grows to %d px despite available room for the child", p.Height)
	}
	titleBottom := float64(p.Pos.Y) + float64(p.Height+p.LabelHeight)/2
	clearance := float64(n.Pos.Y) - titleBottom
	borderGap := p.Pos.Y + p.Height - n.Pos.Y - n.Height
	if clearance < label.PADDING || borderGap < label.PADDING {
		t.Fatalf("child has %.1f px from the title and %d px from the lower border", clearance, borderGap)
	}
}

func TestCompoundSquareCentersTitleUsingItsFittedSide(t *testing.T) {
	for _, tc := range []struct {
		name              string
		tileWidth         int
		titleAlreadyClear bool
	}{
		{"wide tile above title", 240, true},
		{"tile overlaps title", 120, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("Pool: Runtime {shape: square; label.near: center-center\nworker: Worker {width: %d; height: 80}\n}", tc.tileWidth)
			d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
			p, n := d.Shapes[0], d.Shapes[1]
			if p.Width != p.Height || n.Width != tc.tileWidth || n.Height != 80 {
				t.Fatalf("square = %d × %d, tile = %d × %d", p.Width, p.Height, n.Width, n.Height)
			}
			tileTop := n.Pos.Y - p.Pos.Y
			titleTop := float64(p.Height-p.LabelHeight) / 2
			if tc.titleAlreadyClear {
				if p.Width != 360 || tileTop != 60 || float64(tileTop+n.Height+label.PADDING) > titleTop {
					t.Fatalf("square side %d, tile top %d, title top %.1f", p.Width, tileTop, titleTop)
				}
			} else if float64(tileTop) < titleTop+float64(p.LabelHeight)+label.PADDING || tileTop+n.Height > p.Height {
				t.Fatalf("tile at %d is not clear of the title in the %d px square", tileTop, p.Height)
			}
		})
	}
}

func TestAuthoredFrameFitsOnePlainTile(t *testing.T) {
	for _, tc := range []struct{ w, h, wantW, wantH int }{
		{180, 130, 180, 130}, {120, 80, 140, 100}, {240, 210, 240, 210},
	} {
		source := fmt.Sprintf("Panel: Checkout {width: %d; height: %d; label.near: outside-top-center\nWorker: Ready {width: 100; height: 60}}", tc.w, tc.h)
		d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		p, n := d.Shapes[0], d.Shapes[1]
		if p.Width != tc.wantW || p.Height != tc.wantH || n.Width != 100 || n.Height != 60 {
			t.Fatalf("request %d × %d produced frame %d × %d, tile %d × %d", tc.w, tc.h, p.Width, p.Height, n.Width, n.Height)
		}
	}
}

func TestExteriorFrameIconLeavesInnerMarginsAvailable(t *testing.T) {
	for _, pos := range []string{"outside-top-center", "outside-left-center", "outside-bottom-right", "outside-right-top"} {
		source := fmt.Sprintf("Region: Edge services {icon: \"https://example.org/icon.svg\"; icon.near: %s\nworker: Ingest {width: 120; height: 60}}", pos)
		d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		p, n := d.Shapes[0], d.Shapes[1]
		if p.Width != 240 || p.Height != 180 || n.Pos.X-p.Pos.X != 60 || n.Pos.Y-p.Pos.Y != 60 {
			t.Fatalf("%s frame %d × %d, child offset %d,%d", pos, p.Width, p.Height, n.Pos.X-p.Pos.X, n.Pos.Y-p.Pos.Y)
		}
	}
}
