package d2talalayout_test

import (
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func renderedTileCaption(s d2target.Shape) geo.Box {
	body := geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
	w, h := float64(s.LabelWidth), float64(s.LabelHeight)
	return *geo.NewBox(label.FromString(s.LabelPosition).GetPointOnBox(body, label.PADDING, w, h), w, h)
}

func TestPackingIncludesFixedCaptionsOnTilesWithIcons(t *testing.T) {
	const source = `status: Lost warehouse allocation reported via web {width: 75; label.near: outside-bottom-left; icon: health.svg; icon.near: outside-bottom-right}
review: Reschedule approval {height: 45; label.near: outside-top-right}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	a, b := renderedTileCaption(d.Shapes[0]), renderedTileCaption(d.Shapes[1])
	if a.Overlaps(b) {
		t.Fatalf("fixed captions overlap: %v, %v", a, b)
	}
}

func TestPackedFrameFitsAutomaticCaptionAmongFixedDecorations(t *testing.T) {
	const source = `Enclosure: {
  n0: Data {width: 100; height: 45; label.near: outside-bottom-right}
  n1: "Lost warehouse allocation: reported via web" {width: 50; height: 45; icon: health.svg; icon.near: outside-top-center}
  n2: Reschedule approval {width: 100; height: 45}
  n3: "Lost warehouse allocation: reported via web" {width: 100; height: 60; label.near: outside-left-center; icon: health.svg; icon.near: outside-bottom-center}
  n4: Reschedule approval {width: 50; height: 60; label.near: outside-bottom-left; icon: health.svg; icon.near: outside-top-left}
  n5: Independent import sequence {width: 75; height: 60; label.near: outside-top-left; icon: health.svg; icon.near: outside-bottom-center}
}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	parent := d.Shapes[0]
	// The independent tiles and all of their captions fit this drawing.
	tl, br := d.BoundingBox()
	if br.X-tl.X > 657 || br.Y-tl.Y > 436 {
		t.Fatalf("packed frame needs %d × %d pixels", br.X-tl.X, br.Y-tl.Y)
	}
	for _, s := range d.Shapes[1:] {
		caption := renderedTileCaption(s)
		if caption.TopLeft.X < float64(parent.Pos.X) || caption.TopLeft.Y < float64(parent.Pos.Y) ||
			caption.TopLeft.X+caption.Width > float64(parent.Pos.X+parent.Width) ||
			caption.TopLeft.Y+caption.Height > float64(parent.Pos.Y+parent.Height) {
			t.Errorf("%s caption at %v escapes %v", s.ID, caption, parent.Pos)
		}
		for _, other := range d.Shapes[1:] {
			if s.ID != other.ID && caption.Overlaps(renderedTileCaption(other)) {
				t.Errorf("%s caption overlaps %s", s.ID, other.ID)
			}
		}
	}
}

func TestAutomaticCaptionStaysInFrameWhenFixedCaptionsTouch(t *testing.T) {
	const source = `Enclosure: {
  status: "Lost warehouse allocation: reported via web" {width: 50; icon: health.svg; icon.near: outside-top-center}
  report: "Lost warehouse allocation: reported via web" {width: 100; label.near: outside-left-center}
  review: Reschedule approval {label.near: outside-bottom-left}
}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	parent, status := d.Shapes[0], renderedTileCaption(d.Shapes[1])
	if status.TopLeft.X < float64(parent.Pos.X) || status.TopLeft.X+status.Width > float64(parent.Pos.X+parent.Width) {
		t.Fatalf("automatic caption at %v escapes its %d px frame", status.TopLeft, parent.Width)
	}
	for _, s := range d.Shapes[2:] {
		if status.Overlaps(renderedTileCaption(s)) {
			t.Errorf("automatic caption intersects fixed caption on %s", s.ID)
		}
	}
}

func TestPackedCaptionClearsSiblingIcon(t *testing.T) {
	const source = `data: Data {height: 45; icon: health.svg; icon.near: outside-bottom-center}
status: "Lost warehouse allocation: reported via web" {width: 75; label.near: outside-bottom-right}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	data := d.Shapes[0]
	body := geo.NewBox(geo.NewPoint(float64(data.Pos.X), float64(data.Pos.Y)), float64(data.Width), float64(data.Height))
	size := float64(d2target.GetIconSize(body, data.IconPosition))
	icon := geo.NewBox(label.FromString(data.IconPosition).GetPointOnBox(body, label.PADDING, size, size), size, size)
	if caption := renderedTileCaption(d.Shapes[1]); caption.Overlaps(*icon) {
		t.Fatalf("caption at %v intersects its sibling's icon at %v", caption.TopLeft, icon.TopLeft)
	}
}
