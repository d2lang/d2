package d2scenebuild

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
)

func TestBuildConnectionLabelsAndDiagramWideMask(t *testing.T) {
	t.Parallel()

	diagram := validDiagram()
	connection := &diagram.Connections[0]
	connection.Text = d2target.Text{
		Label: "line\nlabel", FontSize: 16, FontFamily: "default", Italic: true,
		LabelWidth: 40, LabelHeight: 40,
	}
	connection.LabelPosition = "INSIDE_MIDDLE_CENTER"
	connection.Fill = "#eeeeee"
	connection.SrcLabel = &d2target.Text{Label: "src", LabelWidth: 24, LabelHeight: 18, Color: "#ff0000"}
	connection.DstLabel = &d2target.Text{Label: "dst", LabelWidth: 24, LabelHeight: 18, Color: "#0000ff"}
	before, err := json.Marshal(diagram)
	if err != nil {
		t.Fatal(err)
	}
	pad := int64(0)
	document, err := Build(context.Background(), diagram, Options{Pad: &pad})
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(diagram)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("connection-label scene build mutated d2target diagram")
	}

	node := findSceneNode(t, document.Root, "a-b")
	assertChildIDs(t, node, []string{
		"a-b:geometry", "a-b:label-fill", "a-b:label:0", "a-b:label:1", "a-b:src-label:0", "a-b:dst-label:0",
	})
	geometry := node.Children[0]
	if geometry.Mask == nil || geometry.Mask.Type != d2scene.MaskLuminance || len(geometry.Children) != 1 || geometry.Children[0].ID != "a-b:path" {
		t.Fatalf("connection geometry/mask = %+v", geometry)
	}
	maskRoot := geometry.Mask.Root
	assertChildIDs(t, maskRoot, []string{"connection-label-mask:base", "a-b:label-mask-hole"})
	hole := maskRoot.Children[1]
	holeRect := hole.Primitive.(d2scene.Rect)
	if holeRect.Box != (d2scene.Box{X: 28, Y: -10, Width: 44, Height: 40}) || hole.Opacity != 1 {
		t.Fatalf("connection label mask hole = %+v opacity %v", holeRect.Box, hole.Opacity)
	}

	background := node.Children[1].Primitive.(d2scene.Rect)
	if background.Box != (d2scene.Box{X: 26, Y: -13, Width: 48, Height: 46}) || background.RadiusX != 10 || background.RadiusY != 10 {
		t.Fatalf("connection label fill = %+v", background)
	}
	first := node.Children[2].Primitive.(d2scene.TextRun)
	second := node.Children[3].Primitive.(d2scene.TextRun)
	if first.Text != "line" || first.Origin != (d2scene.Point{X: 50, Y: 6}) || first.Anchor != d2scene.AnchorMiddle {
		t.Fatalf("first connection label run = %+v", first)
	}
	if second.Text != "label" || second.Origin != (d2scene.Point{X: 50, Y: 26}) {
		t.Fatalf("second connection label run = %+v", second)
	}
	for index, endpoint := range []struct {
		child int
		text  *d2target.Text
		isDst bool
	}{
		{child: 4, text: connection.SrcLabel},
		{child: 5, text: connection.DstLabel, isDst: true},
	} {
		run := node.Children[endpoint.child].Primitive.(d2scene.TextRun)
		topLeft := connection.GetArrowheadLabelPosition(endpoint.isDst)
		wantOrigin := d2scene.Point{X: topLeft.X + float64(endpoint.text.LabelWidth)/2, Y: topLeft.Y + float64(connection.FontSize)}
		if run.Text != endpoint.text.Label || run.Origin != wantOrigin || run.Font.Style != "italic" {
			t.Errorf("endpoint %d label = %+v, want text %q origin %+v italic", index, run, endpoint.text.Label, wantOrigin)
		}
	}
}

func TestBuildOwnsSampledUnderlineProgress(t *testing.T) {
	diagram := validDiagram()
	half, full := .5, 1.
	label := d2target.Text{Label: "ok", FontSize: 16, LabelWidth: 20, LabelHeight: 20, UnderlineProgress: &half}
	diagram.Shapes[0].Text = label
	diagram.Connections[0].Text = label
	diagram.Connections[0].LabelPosition = "INSIDE_MIDDLE_CENTER"
	diagram.Shapes[1].Text = label
	diagram.Shapes[1].Language = "markdown"
	diagram.Shapes[1].UnderlineProgress = &full
	document, err := Build(context.Background(), diagram, Options{})
	if err != nil {
		t.Fatal(err)
	}

	half, full = 0, 0
	for id, want := range map[string]float64{"a:label:0": .5, "a-b:label:0": .5, "b:markdown:primitive:0": 1} {
		run := findSceneNode(t, document.Root, id).Primitive.(d2scene.TextRun)
		if got := d2scene.UnderlineFraction(run.Underline, run.UnderlineProgress); got != want {
			t.Errorf("mutating the input changed %s underline to %g, want %g", id, got, want)
		}
	}
}

func TestBuildOutsideConnectionLabelUsesPartialMask(t *testing.T) {
	t.Parallel()

	diagram := validDiagram()
	connection := &diagram.Connections[0]
	connection.Text = d2target.Text{Label: "above", FontSize: 16, LabelWidth: 40, LabelHeight: 20}
	connection.LabelPosition = "OUTSIDE_TOP_CENTER"
	pad := int64(0)
	document, err := Build(context.Background(), diagram, Options{Pad: &pad})
	if err != nil {
		t.Fatal(err)
	}
	geometry := findSceneNode(t, document.Root, "a-b:geometry")
	hole := findSceneNode(t, geometry.Mask.Root, "a-b:label-mask-hole")
	if hole.Opacity != .75 {
		t.Fatalf("outside label mask opacity = %v, want .75", hole.Opacity)
	}
}

func TestBuildShapeBorderLabelContributesGlobalConnectionHole(t *testing.T) {
	t.Parallel()

	diagram := validDiagram()
	diagram.Shapes[0].Text = d2target.Text{Label: "top", FontSize: 16, LabelWidth: 10, LabelHeight: 18}
	diagram.Shapes[0].LabelPosition = "BORDER_TOP_CENTER"
	pad := int64(0)
	document, err := Build(context.Background(), diagram, Options{Pad: &pad})
	if err != nil {
		t.Fatal(err)
	}
	shapeLabel := findSceneNode(t, document.Root, "a:label:0").Primitive.(d2scene.TextRun)
	if shapeLabel.Origin != (d2scene.Point{X: 10, Y: 7}) {
		t.Fatalf("border label origin = %+v, want outer-box placement", shapeLabel.Origin)
	}
	geometry := findSceneNode(t, document.Root, "a-b:geometry")
	if geometry.Mask == nil {
		t.Fatal("unlabelled connection has no diagram-wide shape-label mask")
	}
	hole := findSceneNode(t, geometry.Mask.Root, "a:border-label-mask-hole")
	rect := hole.Primitive.(d2scene.Rect)
	if rect.Box != (d2scene.Box{X: 3, Y: -1, Width: 14, Height: 2}) {
		t.Fatalf("shape border-label hole = %+v", rect.Box)
	}
}

func TestBuildRejectsInvalidConnectionLabelContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edit func(*d2target.Connection)
		want string
	}{
		{name: "position", edit: func(c *d2target.Connection) {
			c.Text = d2target.Text{Label: "x", FontSize: 16, LabelWidth: 10, LabelHeight: 10}
			c.LabelPosition = "BORDER_TOP_CENTER"
		}, want: "labelPosition"},
		{name: "dimensions", edit: func(c *d2target.Connection) {
			c.Text = d2target.Text{Label: "x", FontSize: 16, LabelWidth: 0, LabelHeight: 10}
			c.LabelPosition = "INSIDE_MIDDLE_CENTER"
		}, want: "labelDimensions"},
		{name: "arrow dimensions", edit: func(c *d2target.Connection) {
			c.FontSize = 16
			c.SrcLabel = &d2target.Text{Label: "src", LabelHeight: 10}
		}, want: "srcLabelDimensions"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagram := validDiagram()
			test.edit(&diagram.Connections[0])
			_, err := Build(context.Background(), diagram, Options{})
			if err == nil || !strings.Contains(err.Error(), `connection "a-b"`) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Build() error = %v, want connection context and %q", err, test.want)
			}
		})
	}
}

func TestConnectionTraceKeepsLabelAndMaskTogether(t *testing.T) {
	diagram := validDiagram()
	e := &diagram.Connections[0]
	e.Text = d2target.Text{Label: "Request", FontSize: 16, LabelWidth: 46, LabelHeight: 20, FontFamily: "default"}
	e.LabelPosition = "INSIDE_MIDDLE_CENTER"
	e.Opacity = .6
	e.MotionLabel = &d2target.MotionLabel{Point: d2target.Point{X: 32, Y: 11}, Opacity: .35}
	document, err := Build(context.Background(), diagram, Options{})
	if err != nil {
		t.Fatal(err)
	}
	n := findSceneNode(t, document.Root, e.ID)
	if len(n.Children) != 2 || n.Children[1].Opacity != .35 {
		t.Fatalf("expected fading caption over a fully painted route: %+v", n)
	}
	hole := n.Children[0].Mask.Root.Children[1]
	box := hole.Primitive.(d2scene.Rect).Box
	if hole.Opacity != .35 || box != (d2scene.Box{X: 30, Y: 11, Width: 50, Height: 20}) {
		t.Fatalf("label mask %+v opacity %v", box, hole.Opacity)
	}
}

func TestHiddenIconAndTooltipLeaveNoConnectionMask(t *testing.T) {
	d := validDiagram()
	e := &d.Connections[0]
	e.Label, e.Tooltip = "", "Timing"
	e.Icon = &url.URL{Scheme: "https", Host: "example.com", Path: "/mark.svg"}
	e.IconPosition = "INSIDE_MIDDLE_CENTER"
	b := builder{diagram: d, ctx: t.Context()}
	e.Opacity = 0
	mask, err := b.buildConnectionMask(d2scene.Box{Width: 150, Height: 90})
	if err != nil || mask != nil {
		t.Fatalf("hidden icon and tooltip left a mask: %v, %v", mask, err)
	}
	e.Opacity = .4
	mask, err = b.buildConnectionMask(d2scene.Box{Width: 150, Height: 90})
	if err != nil || mask == nil || len(mask.Root.Children) != 2 || mask.Root.Children[1].Opacity != 1 {
		t.Fatalf("visible icon lost its authored mask: %v, %v", mask, err)
	}
}
