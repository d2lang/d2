package d2svg

import (
	"bytes"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestPruneThemeCSS(t *testing.T) {
	dark := int64(0)
	full, err := ThemeCSS("d2-test", nil, &dark, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Include classes in separate body/background fragments, with XML entity
	// decoding, either quoting style, and multiple CSS whitespace separators.
	got := pruneThemeCSS(full,
		`<g class='shape fill-N1 stroke&#45;B3'><text class="text color-AB5"/></g>`,
		`<rect class="fill-N7&#9;sketch-overlay-AA2"/>`,
	)
	for _, class := range []string{"fill-N1", "stroke-B3", "color-AB5", "fill-N7", "sketch-overlay-AA2", "stroke-B2"} {
		if count := strings.Count(got, "."+class+"{"); count != 2 {
			t.Fatalf("%s retained %d rules, want both light and dark rules", class, count)
		}
	}
	for _, class := range []string{"fill-N2", "background-color-B1", "sketch-overlay-N1", "md", "light-code", "dark-code"} {
		if strings.Contains(got, "."+class+"{") {
			t.Fatalf("unused %s was retained", class)
		}
	}
	if !strings.Contains(got, "@media screen and (prefers-color-scheme:dark){") {
		t.Fatal("dark media scope was changed")
	}
	withContent := pruneThemeCSS(full, `<g class="md"/><g class="light-code"/><g class="dark-code"/>`)
	for _, selector := range []string{".appendix text.text{", ".md{", ".light-code{", ".dark-code{"} {
		if strings.Count(withContent, selector) != strings.Count(full, selector) {
			t.Fatalf("used selector %s was changed", selector)
		}
	}
	if len(got) >= len(full)/2 {
		t.Fatalf("unused palette rules were not removed: %d -> %d bytes", len(full), len(got))
	}
	again, err := ThemeCSS("d2-test", nil, &dark, nil, nil)
	if err != nil || full != again {
		t.Fatal("pruning changed the public full stylesheet")
	}
}

func TestPruneThemeCSSRetainsExactUsedRules(t *testing.T) {
	const stylesheet = ".d2-test .fill-N1{fill:#123456;}\n" +
		".d2-test .fill-N2{fill:#abcdef;}\n" +
		".d2-test .stroke-B2{stroke:blue;}\n" +
		".sketch-overlay-B3{fill:url(#streaks-dark-d2-test);mix-blend-mode:overlay}\n" +
		".shape{stroke-linejoin:round}.d2-test .fill-CUSTOM{fill:red}.custom .other-N1{color:red}"
	const source = `<!-- <rect class="fill-N2"/> --><defs><g id="shape" class="fill-N1"/></defs><use href="#shape"/><g class="sketch-overlay-B3"/>`
	const want = ".d2-test .fill-N1{fill:#123456;}\n" +
		".d2-test .stroke-B2{stroke:blue;}\n" +
		".sketch-overlay-B3{fill:url(#streaks-dark-d2-test);mix-blend-mode:overlay}\n" +
		".shape{stroke-linejoin:round}.d2-test .fill-CUSTOM{fill:red}.custom .other-N1{color:red}"
	if got := pruneThemeCSS(stylesheet, source); got != want {
		t.Fatalf("used or unrelated CSS changed:\n%s\nwant:\n%s", got, want)
	}
}

func TestPruneThemeCSSConservativeFallback(t *testing.T) {
	const stylesheet = ".d2-test .fill-N1{fill:red;} .d2-test .fill-N2{fill:blue;}"
	for _, source := range []string{
		`<g`,
		`<g class="&unknown;"/>`,
		`<script>document.documentElement.setAttribute('class','fill-N1')</script>`,
		`<g onload="this.setAttribute('class','fill-N1')"/>`,
		`<g><set attributeName="class" to="fill-N1"/></g>`,
		`<g><animate attributeName="class" values="fill-N1;fill-N2"/></g>`,
		`<use href="other.svg#shape"/>`,
		`<a href="javascript:changeClasses()"/>`,
	} {
		t.Run(source, func(t *testing.T) {
			if got := pruneThemeCSS(stylesheet, `<rect/>`, source); got != stylesheet {
				t.Fatalf("unsafe fragment was pruned: %q", got)
			}
		})
	}
}

func TestPruneThemeCSSPaletteAlphabet(t *testing.T) {
	const source = `<g class="fill-N10 stroke-N0 color-AA3 sketch-overlay-bright FILL-N1"/>`
	const stylesheet = ".d2-test .fill-N1{fill:red;} .d2-test .fill-N10{fill:blue;} .sketch-overlay-bright{fill:yellow;}"
	const want = " .d2-test .fill-N10{fill:blue;} .sketch-overlay-bright{fill:yellow;}"
	if got := pruneThemeCSS(stylesheet, source); got != want {
		t.Fatalf("non-palette classes affected pruning: %q, want %q", got, want)
	}
}

func TestCustomStylesheetKeepsGeneratedResources(t *testing.T) {
	original := BaseStylesheet
	t.Cleanup(func() { BaseStylesheet = original })
	diagram := d2target.NewDiagram()
	hash, err := diagram.HashID(nil)
	if err != nil {
		t.Fatal(err)
	}
	BaseStylesheet += ".shape {mask:url(#" + hash + ")}"
	markup, err := Render(diagram, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(markup, []byte(`<mask id="`+hash+`"`)) || !bytes.Contains(markup, []byte(`.light-code{`)) {
		t.Fatal("custom styles lost the generated label mask or code stylesheet")
	}
	var styles bytes.Buffer
	if err := WriteAnimationStyles(&styles, hash, "<rect/>", diagram, RenderOpts{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(styles.String(), ".fill-B1{") {
		t.Fatal("custom animation stylesheet lost its palette")
	}
}

func TestRootFillPatternDeclaresXLink(t *testing.T) {
	t.Parallel()
	diagram := d2target.NewDiagram()
	diagram.Root.FillPattern = "grain"
	out, err := Render(diagram, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(` xlink:href=`)) || !bytes.Contains(out, []byte(` xmlns:xlink="http://www.w3.org/1999/xlink"`)) {
		t.Fatal("grain pattern needs an XLink namespace for its embedded image")
	}
}
