package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
)

func TestConnectionIconCadence(t *testing.T) {
	const input = `
vars.cadence: 1.5/s
classes.emit: {icon.style.animated: ${cadence}}
a -> b: {class: emit}
a -> c: {icon: "circle"; icon.style.animated: true; style.animated: true}
a -> d: {icon.shape: circle}
a -> e: {icon.style.animated: false}
a -> f: {icon: "./my icon.svg" {style.border-radius: 8; near: center-left}}
steps: {
  stop: {(a -> b)[0].icon.style.animated: 0/s}
  resume: {(a -> b)[0].icon.style.animated: .25/s}
  clear: {(a -> c)[0].icon.style.animated: null}
}
`
	g, _, err := d2compiler.Compile("source.d2", strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	connection := func(board *d2graph.Graph, index int) d2target.Connection {
		return d2exporter.ToConnection(board.Edges[index], nil)
	}
	for _, item := range []struct {
		board *d2graph.Graph
		rate  float64
	}{{g, 1.5}, {g.Steps[0], 0}, {g.Steps[1], .25}} {
		c := connection(item.board, 0)
		if c.IconAnimation == nil || c.IconAnimation.Rate == nil || *c.IconAnimation.Rate != item.rate || c.IconShape != d2target.ShapeCircle || c.Icon != nil || c.HasStaticIcon() || c.Animated {
			t.Fatalf("rate %g exported as %+v", item.rate, c)
		}
	}
	loop := connection(g, 1)
	if loop.IconAnimation == nil || loop.IconAnimation.Rate != nil || loop.Icon == nil || loop.Icon.Path != "circle" || loop.IconShape != "" || !loop.Animated {
		t.Fatalf("expected one looping image and animated stroke: %+v", loop)
	}
	circle := connection(g, 2)
	if !circle.HasStaticIcon() || circle.IconShape != d2target.ShapeCircle || circle.IconSize() >= d2target.DEFAULT_ICON_SIZE {
		t.Fatalf("expected a small static circle: %+v", circle)
	}
	off := connection(g, 3)
	if off.HasStaticIcon() || off.IconAnimation != nil {
		t.Fatalf("false emitted an icon: %+v", off)
	}
	image := connection(g, 4)
	if !image.HasStaticIcon() || image.Icon.Path != "./my icon.svg" || image.IconBorderRadius != 8 {
		t.Fatalf("static image lost its URL or style: %+v", image)
	}
	cleared := connection(g.Steps[2], 1)
	if cleared.IconAnimation != nil || !cleared.HasStaticIcon() {
		t.Fatalf("clearing icon motion should restore a static image: %+v", cleared)
	}
}

func TestInheritedImageStyleWithConnectionIconMotion(t *testing.T) {
	const input = `classes.art: {icon: ./image.svg {style.border-radius: 3}}
a: {class: art; icon.style.border-radius: 15}
a -> b: {class: art; icon.style.border-radius: 15; icon.style.animated: 2/s}`
	g, _, err := d2compiler.Compile("source.d2", strings.NewReader(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if radius := g.Objects[0].IconStyle.BorderRadius; radius == nil || radius.Value != "3" {
		t.Errorf("inherited shape image lost its original border radius: %+v", radius)
	}
	icon := d2exporter.ToConnection(g.Edges[0], nil)
	if icon.Icon == nil || icon.IconBorderRadius != 3 || icon.IconAnimation == nil || icon.IconAnimation.Rate == nil || *icon.IconAnimation.Rate != 2 {
		t.Errorf("inherited edge image lost its style or cadence: %+v", icon)
	}
}

func TestInvalidConnectionIconCadence(t *testing.T) {
	for _, value := range []string{"-1/s", "2", "1/ms", "1/ns", "1e9/s", "NaN/s", "Inf/s", "1.2.3/s", "1_/s", "./s", "[2/s]", "{foo: 3}", "2/s {foo: 3}", strings.Repeat("9", 400) + "/s"} {
		src := "a -> b: {icon.style.animated: " + value + "}"
		if _, _, err := d2compiler.Compile("source.d2", strings.NewReader(src), nil); err == nil {
			t.Errorf("compiled invalid icon cadence %q", value)
		}
	}
	for _, src := range []string{
		"a -> b: {style.animated: 1/s}",
		"a: {style.animated: 1/s}",
		"a: {icon: ./test.svg; icon.style.animated: 1/s}",
		"a: {icon.style.animated: 1/s}",
		"a -> b: {icon.shape: rectangle}",
		"a -> b: {icon: ./test.svg; icon.shape: circle}",
	} {
		if _, _, err := d2compiler.Compile("source.d2", strings.NewReader(src), nil); err == nil {
			t.Errorf("compiled invalid icon declaration %q", src)
		}
	}
	if _, err := d2graph.ParseConnectionIconAnimation("1" + strings.Repeat("0", 300) + "/s"); err != nil {
		t.Errorf("finite rates should compile: %v", err)
	}
}
