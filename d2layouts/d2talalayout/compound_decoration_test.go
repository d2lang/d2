package d2talalayout_test

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
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

func TestAnchoredCaptionUsesSpaceInsideTheExistingFrame(t *testing.T) {
	for _, tc := range []struct {
		shape, anchor string
		width, height int
	}{
		{"rectangle", "border-top-left", 664, 180},
		{"rectangle", "border-top-right", 664, 180},
		{"circle", "border-top-right", 812, 812},
		{"oval", "border-top-left", 939, 313},
	} {
		source := fmt.Sprintf(`Panel: "" {shape: %s
 Worker: Warehouse inventory reconciliation for incoming international consignments {width: 80; height: 60; label.near: %s}
}`, tc.shape, tc.anchor)
		d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		p, c := d.Shapes[0], d.Shapes[1]
		body := geo.NewBox(geo.NewPoint(float64(c.Pos.X), float64(c.Pos.Y)), float64(c.Width), float64(c.Height))
		caption := label.FromString(c.LabelPosition).GetPointOnBox(body, label.PADDING, float64(c.LabelWidth), float64(c.LabelHeight))
		if p.Width != tc.width || p.Height != tc.height || c.Width != 80 || c.Height != 60 {
			t.Fatalf("%s %s: frame %d × %d, child %d × %d", tc.shape, tc.anchor, p.Width, p.Height, c.Width, c.Height)
		}
		if caption.X < float64(p.Pos.X)+20 || caption.X+float64(c.LabelWidth) > float64(p.Pos.X+p.Width)-20 {
			t.Fatalf("%s %s: caption starting at %v extends beyond its frame's gutter", tc.shape, tc.anchor, caption)
		}
	}
}

func TestIsolatedOvalClearsTitleWithoutGrowing(t *testing.T) {
	const source = `Pool: Expansion {shape: oval; label.near: top-center
 Worker: Worker {width: 120; height: 65}
}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	p, c := d.Shapes[0], d.Shapes[1]
	box := geo.NewBox(geo.NewPoint(float64(p.Pos.X), float64(p.Pos.Y)), float64(p.Width), float64(p.Height))
	inner := shape.NewOval(box).GetInnerBox()
	title := label.InsideTopCenter.GetPointOnBox(inner, label.PADDING, float64(p.LabelWidth), float64(p.LabelHeight))
	if p.Width != 319 || p.Height != 173 || title.Y+float64(p.LabelHeight)+label.PADDING > float64(c.Pos.Y) {
		t.Fatalf("oval %d × %d: title at %v, child at %v", p.Width, p.Height, title, c.Pos)
	}
}

func TestIsolatedFrameClearsCenteredIconOnlyWithRoom(t *testing.T) {
	for _, tc := range []struct{ height, workerY int }{{213, 74}, {320, 58}} {
		source := fmt.Sprintf(`Pool: Runtime {shape: rectangle; height: %d
 label.near: outside-top-center; icon: "https://example.org/status.svg"; icon.near: center-center
 Worker: Worker {width: 120; height: 65}
}`, tc.height)
		d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		p, c := d.Shapes[0], d.Shapes[1]
		if p.Height != tc.height || p.Width != 268 || c.Pos.X != p.Pos.X+74 || c.Pos.Y != p.Pos.Y+tc.workerY {
			t.Fatalf("height %d: frame %d × %d, child at %v", tc.height, p.Width, p.Height, c.Pos)
		}
	}
}

func TestFramePreservesTilePositionWhenNestedLayoutHidesAnEdge(t *testing.T) {
	const panel = `Panel: "" {shape: rectangle
 Worker: Warehouse inventory reconciliation for incoming international consignments {width: 80; height: 60; label.near: border-top-left}
}`
	for _, source := range []string{
		panel + "\nExtras: {near: top-center}\nPanel.Worker -> Extras",
		"Grid: {grid-columns: 1\n" + panel + "\n}\nGrid.Panel.Worker -> Extras",
	} {
		d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		var parentX, workerX int
		for _, s := range d.Shapes {
			switch s.ID {
			case "Panel", "Grid.Panel":
				parentX = s.Pos.X
			case "Panel.Worker", "Grid.Panel.Worker":
				workerX = s.Pos.X
			}
		}
		if len(d.Connections) != 1 || workerX-parentX != 292 {
			t.Fatalf("external connection count %d, worker x offset %d", len(d.Connections), workerX-parentX)
		}
	}
}
