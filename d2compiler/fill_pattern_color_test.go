package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2parser"
)

func TestCompileFillPatternColor(t *testing.T) {
	t.Parallel()

	t.Run("valid_dot_syntax", func(t *testing.T) {
		t.Parallel()
		g, _, err := d2compiler.Compile("", strings.NewReader(`
style.fill-pattern: dots
style.fill-pattern.color: "#ffffff"
x: {
  style.fill: "black"
  style.fill-pattern: dots
  style.fill-pattern.color: white
}
y: {
  style.fill-pattern: lines {
    color: "#ff00aa"
  }
}
`), nil)
		if err != nil {
			t.Fatalf("unexpected compile error: %v", err)
		}
		if g.Root.Style.FillPattern == nil || g.Root.Style.FillPattern.Value != "dots" {
			t.Fatalf("root FillPattern = %+v, want dots", g.Root.Style.FillPattern)
		}
		if g.Root.Style.FillPatternColor == nil || g.Root.Style.FillPatternColor.Value != "#ffffff" {
			t.Fatalf("root FillPatternColor = %+v, want #ffffff", g.Root.Style.FillPatternColor)
		}
		x, ok := g.Root.HasChild([]string{"x"})
		if !ok {
			t.Fatal("missing shape x")
		}
		if x.Style.FillPatternColor == nil || x.Style.FillPatternColor.Value != "white" {
			t.Fatalf("x FillPatternColor = %+v, want white", x.Style.FillPatternColor)
		}
		y, ok := g.Root.HasChild([]string{"y"})
		if !ok {
			t.Fatal("missing shape y")
		}
		if y.Style.FillPattern == nil || y.Style.FillPattern.Value != "lines" {
			t.Fatalf("y FillPattern = %+v, want lines", y.Style.FillPattern)
		}
		if y.Style.FillPatternColor == nil || y.Style.FillPatternColor.Value != "#ff00aa" {
			t.Fatalf("y FillPatternColor = %+v, want #ff00aa", y.Style.FillPatternColor)
		}
	})

	t.Run("color_only_subfield", func(t *testing.T) {
		t.Parallel()
		g, _, err := d2compiler.Compile("", strings.NewReader(`
classes: {
  dotted: {
    style.fill-pattern: dots
  }
}
x: {
  class: dotted
  style.fill-pattern.color: orange
}
`), nil)
		if err != nil {
			t.Fatalf("unexpected compile error: %v", err)
		}
		x, ok := g.Root.HasChild([]string{"x"})
		if !ok {
			t.Fatal("missing shape x")
		}
		if x.Style.FillPattern == nil || x.Style.FillPattern.Value != "dots" {
			t.Fatalf("x FillPattern = %+v, want dots", x.Style.FillPattern)
		}
		if x.Style.FillPatternColor == nil || x.Style.FillPatternColor.Value != "orange" {
			t.Fatalf("x FillPatternColor = %+v, want orange", x.Style.FillPatternColor)
		}
	})

	t.Run("invalid_color", func(t *testing.T) {
		t.Parallel()
		_, _, err := d2compiler.Compile("", strings.NewReader(`
x.style.fill-pattern: dots
x.style.fill-pattern.color: not-a-color
`), nil)
		if err == nil {
			t.Fatal("expected compile error for invalid fill-pattern.color")
		}
		parseErr, ok := err.(*d2parser.ParseError)
		if !ok || len(parseErr.Errors) == 0 || !strings.Contains(parseErr.Errors[0].Message, `fill-pattern.color`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid_subfield", func(t *testing.T) {
		t.Parallel()
		_, _, err := d2compiler.Compile("", strings.NewReader(`
x.style.fill-pattern: dots
x.style.fill-pattern.size: 10
`), nil)
		if err == nil {
			t.Fatal("expected compile error for invalid fill-pattern subfield")
		}
	})
}
