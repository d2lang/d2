package d2svg

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// TestDrawGolden pins the markup drawShape and drawConnection produce over a
// broad matrix of shapes, connections and options. Regenerate with
// UPDATE_DRAW_GOLDEN=1 only for intended output changes.
func TestDrawGolden(t *testing.T) {
	got := map[string]string{}
	record := func(name string, draw func(w *objectWriter, appendix *bytes.Buffer) (string, error)) {
		buf := &bytes.Buffer{}
		appendix := &bytes.Buffer{}
		w := &objectWriter{buf: buf}
		mask, err := draw(w, appendix)
		w.finish()
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%v", buf.String(), appendix.String(), mask, err)))
		got[name] = fmt.Sprintf("%x", sum[:8])
	}
	icon, _ := url.Parse("https://icons.example/icon.svg")
	md := newMarkdownRenderer(nil, nil, nil)

	types := []string{""}
	for k := range d2target.DSL_SHAPE_TO_SHAPE_TYPE {
		types = append(types, k)
	}
	sort.Strings(types)
	type labelCase struct{ name, lang, label, pos string }
	labels := []labelCase{
		{"none", "", "", ""},
		{"plain", "", "hello", ""},
		{"outside", "", "hello", "OUTSIDE_TOP_CENTER"},
		{"border", "", "hello", "BORDER_TOP_CENTER"},
		{"inside", "", "hello", "INSIDE_BOTTOM_LEFT"},
		{"md", "markdown", "# hi\n\n**b**", ""},
		{"code", "go", "func f() {\n\treturn 1\n}", ""},
		{"latex", "latex", "x^2", ""},
	}
	for _, typ := range types {
		for flags := 0; flags < 16; flags++ {
			for li, lc := range labels {
				sketch := flags&8 != 0
				if sketch && li > 2 {
					continue
				}
				for variant := 0; variant < 3; variant++ {
					if variant > 0 && (sketch || li > 3) {
						continue
					}
					s := *d2target.BaseShape()
					s.ID = "a.b"
					s.Type = typ
					s.Pos = d2target.Point{X: 10, Y: 20}
					s.Width, s.Height = 140, 90
					s.Fill, s.Stroke = "#ffeedd", "#112233"
					s.Multiple = flags&1 != 0
					s.DoubleBorder = flags&2 != 0
					s.ThreeDee = flags&4 != 0
					s.Label, s.Language, s.LabelPosition = lc.label, lc.lang, lc.pos
					s.LabelWidth, s.LabelHeight, s.FontSize = 40, 21, 16
					s.Color = "#000000"
					if typ == d2target.ShapeImage || variant == 1 {
						s.Icon = icon
					}
					switch variant {
					case 1:
						s.IconPosition = "OUTSIDE_LEFT_TOP"
						s.IconBorderRadius = 4
						s.BorderRadius = 6
						s.Shadow, s.Blend = true, true
						s.Opacity = 0.5
						s.Link = "https://x.example"
						s.Tooltip = "tip"
						s.FontFamily = "mono"
						s.Bold, s.Underline = false, true
						s.LabelFill = "#abcdef"
						s.FillPattern = "dots"
						s.StrokeDash = 3
					case 2:
						s.Tooltip = "positioned"
						s.TooltipPosition = "top"
						s.Italic, s.Bold = true, false
						s.Animated = true
						s.Classes = []string{"c1", "c2"}
						s.FontSize = 20
					}
					name := fmt.Sprintf("shape/%s/f%d/%s/v%d", typ, flags, lc.name, variant)
					record(name, func(w *objectWriter, appendix *bytes.Buffer) (string, error) {
						return drawShape(w, appendix, "dh", "rh", s, sketch, nil, md)
					})
				}
			}
		}
	}

	arrows := []d2target.Arrowhead{d2target.NoArrowhead, d2target.ArrowArrowhead, d2target.TriangleArrowhead, d2target.UnfilledTriangleArrowhead, d2target.DiamondArrowhead, d2target.FilledDiamondArrowhead, d2target.CircleArrowhead, d2target.FilledCircleArrowhead, d2target.CrossArrowhead, d2target.BoxArrowhead, d2target.FilledBoxArrowhead, d2target.LineArrowhead, d2target.CfOne, d2target.CfMany, d2target.CfOneRequired, d2target.CfManyRequired}
	clabels := []labelCase{
		{"none", "", "", ""},
		{"plain", "", "edge", "OUTSIDE_TOP_CENTER"},
		{"onedge", "", "edge", "INSIDE_MIDDLE_CENTER"},
		{"md", "markdown", "*e*", "INSIDE_MIDDLE_CENTER"},
		{"code", "py", "x = 1\ny = 2", "OUTSIDE_BOTTOM_CENTER"},
		{"latex", "latex", "y^3", "INSIDE_MIDDLE_CENTER"},
	}
	for ai, src := range arrows {
		for bi, dst := range arrows {
			if ai != 0 && bi != 0 && ai != bi && (ai+bi)%5 != 0 {
				continue
			}
			for flags := 0; flags < 16; flags++ {
				for li, lc := range clabels {
					sketch := flags&8 != 0
					if (sketch || flags&4 != 0) && (li > 1 || (ai+bi)%3 != 0) {
						continue
					}
					c := *d2target.BaseConnection()
					c.ID = "(a -> b)[0]"
					c.Src, c.Dst = "a", "b"
					c.SrcArrow, c.DstArrow = src, dst
					c.Route = []*geo.Point{{X: 0, Y: 0}, {X: 50, Y: 10}, {X: 100, Y: 100}, {X: 200, Y: 120}}
					c.IsCurve = flags&2 != 0
					c.Animated = flags&1 != 0
					c.Stroke = "#223344"
					if flags&4 != 0 {
						c.Stroke = "linear-gradient(#ff0000, #0000ff)"
					}
					c.Label, c.Language, c.LabelPosition = lc.label, lc.lang, lc.pos
					c.LabelWidth, c.LabelHeight, c.FontSize = 30, 18, 16
					c.LabelPercentage = 0.5
					c.Color = "#101010"
					if li == 2 {
						c.Icon = icon
						c.IconBorderRadius = 3
						c.Fill = "#eeeeee"
						c.Link = "https://y.example"
						c.Opacity = 0.7
						c.Bold, c.Italic, c.Underline = true, false, true
						c.SrcLabel = &d2target.Text{Label: "1", LabelWidth: 8, LabelHeight: 12, Color: "#ff0000"}
						c.DstLabel = &d2target.Text{Label: "*", LabelWidth: 8, LabelHeight: 12}
						c.StrokeDash = 4
					}
					if li == 4 {
						c.Link = "https://z.example"
						c.FontFamily = "mono"
						c.FontSize = 12
					}
					if li == 0 && flags == 0 {
						c.Icon = icon
						c.IconPosition = "OUTSIDE_TOP_CENTER"
					}
					mask := ""
					if flags%3 == 0 {
						mask = "crossing-mask"
					}
					name := fmt.Sprintf("conn/%s/%s/f%d/%s", src, dst, flags, lc.name)
					record(name, func(w *objectWriter, _ *bytes.Buffer) (string, error) {
						return drawConnection(w, "dh", mask, c, map[string]struct{}{}, map[string]d2target.Shape{}, sketch, nil, md)
					})
				}
			}
		}
	}

	// One line per shape type or source arrowhead keeps the golden file small;
	// the line names the group whose output changed.
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	groups := map[string]*strings.Builder{}
	var order []string
	for _, name := range names {
		parts := strings.SplitN(name, "/", 3)
		group := parts[0] + "/" + parts[1]
		if groups[group] == nil {
			groups[group] = &strings.Builder{}
			order = append(order, group)
		}
		fmt.Fprintf(groups[group], "%s %s\n", name, got[name])
	}
	var b strings.Builder
	for _, group := range order {
		sum := sha256.Sum256([]byte(groups[group].String()))
		fmt.Fprintf(&b, "%s %x\n", group, sum[:8])
	}
	checkGolden(t, "testdata/draw_golden.txt", b.String())
}

// checkGolden compares got with the golden file at path, or rewrites the file
// when UPDATE_DRAW_GOLDEN is set.
func checkGolden(t *testing.T, path, got string) {
	if os.Getenv("UPDATE_DRAW_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(got, "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var wl, gl string
		if i < len(wantLines) {
			wl = wantLines[i]
		}
		if i < len(gotLines) {
			gl = gotLines[i]
		}
		if wl != gl {
			t.Errorf("want %q, got %q", wl, gl)
		}
	}
}
