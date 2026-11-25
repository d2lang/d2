package d2svg

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2themes"
)

func TestObjectMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, fragment string
		fallback       bool
	}{
		{"plain", "", false},
		{"inherited spans", `<text class="text"><tspan>x</tspan><tspan class="text-italic fill-N2">y</tspan></text>`, false},
		{"stylesheet", `<style>.other {font-family: serif}</style>`, true},
		{"custom whitespace", "<g class=\"fill-B5\u00a0stroke-B2\"></g>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &objectWriter{buf: new(bytes.Buffer), usage: &svgUsage{classes: map[string]bool{}}}
			label := func(class, text string) {
				el := d2themes.NewThemableElement("text", nil)
				el.ClassName, el.Fill = class, "N1"
				w.label(el, text, 40)
			}
			w.openGroup([]string{"example", "stroke-AA4"}, "")
			shape := d2themes.NewThemableElement("rect", nil)
			shape.Fill, shape.Stroke, shape.FillPattern = "B4", "B1", "dots"
			w.element(shape)
			label("text text-underline", "A & <B>\n\nCé中")
			w.buf.WriteString("</g>")
			if w.raw {
				t.Fatal("plain geometry and multiline text unexpectedly required XML parsing")
			}
			w.finish()

			w.openGroup([]string{"mixed"}, "")
			label("text-mono", "first")
			cell := d2themes.NewThemableElement("text", nil)
			cell.ClassName = "text text-mono"
			w.text(cell, "row\nwith &")
			if tc.fragment != "" {
				fmt.Fprint(w, tc.fragment)
			}
			label("text-mono", "last")
			w.buf.WriteString("</g>")
			w.finish()
			label("text", "after")
			w.finish()

			if (w.usage == nil) != tc.fallback {
				t.Fatalf("whole-board fallback = %t, want %t", w.usage == nil, tc.fallback)
			}
			source := w.buf.String()
			fonts, classes := w.metadata(source)
			wantFonts, wantClasses, _ := collectFontCorporaAndClasses(source)
			if !reflect.DeepEqual(fonts, wantFonts) || !reflect.DeepEqual(classes, wantClasses) {
				t.Fatalf("metadata = %v %v, want %v %v", fonts, classes, wantFonts, wantClasses)
			}
		})
	}
}

func TestConsecutiveRawObjects(t *testing.T) {
	for _, tc := range []struct {
		name      string
		lastLabel bool
	}{
		{"trigger in a later object", true},
		{"no regular-face trigger", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &objectWriter{buf: new(bytes.Buffer), usage: &svgUsage{classes: map[string]bool{}}}
			writeRaw := func(s string) {
				fmt.Fprint(w, s)
				w.finish()
			}
			writeRaw(`<g class="text-bold"><text class="other text fill-N1">first</text></g>`)
			writeRaw(`<text class="text-mono">code</text>`)
			if tc.lastLabel {
				el := d2themes.NewThemableElement("text", nil)
				el.ClassName = "text"
				w.label(el, "second", 12)
				w.finish()
			}
			writeRaw(`<g class="stroke-N2"><text class="other text">last</text></g>`)

			source := w.buf.String()
			fonts, classes := w.metadata(source)
			wantFonts, wantClasses, _ := collectFontCorporaAndClasses(source)
			if !reflect.DeepEqual(fonts, wantFonts) || !reflect.DeepEqual(classes, wantClasses) {
				t.Fatalf("metadata = %v %v, want %v %v", fonts, classes, wantFonts, wantClasses)
			}
		})
	}
}

func TestPartialUnderlineUsesEachLineWidth(t *testing.T) {
	w := &objectWriter{buf: new(bytes.Buffer)}
	el := d2themes.NewThemableElement("text", nil)
	el.ClassName, el.X, el.Y, el.Fill = "text", 55, 20, "N1"
	el.Style = "text-anchor:middle;font-size:16px"
	fraction := .3
	w.animatedUnderline(el, "gyp\nx", 36, &fraction)
	out := w.buf.String()
	if strings.Contains(out, "pointer-events:none") {
		t.Fatal("disabling hit testing on either half makes part of a linked label unclickable")
	}
	for _, fragment := range []string{`x="55" y="20"`, `x="55" y="38"`, ">gyp</text>", ">x</text>", "inset(-100% 70% -100% -100%) fill-box", "inset(-100% -100% -100% 30%) fill-box", `aria-hidden="true"`} {
		if strings.Count(out, fragment) != 2 {
			t.Errorf("expected two occurrences of %q in %s", fragment, out)
		}
	}
}
