package d2cli

import (
	"bytes"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/d2lang/d2/internal/testutil"
	"github.com/d2lang/d2/internal/testutil/imagediff"
	"github.com/d2lang/d2/lib/pptx"
)

const explodedCLISource = `layers: {
  studio: {
    press: {link: https://example.com/press}
    proof: {link: layers.edition}
    press -> proof: {label: ""; link: layers.edition}
    layers: {edition: {folio: {link: https://example.com/folio}}}
  }
  warehouse: {crate: {link: https://example.com/crate}}
}`

func TestIsometricExplodedCLISelectsOneScene(t *testing.T) {
	const fromSource = "vars: {d2-config: {isometric-exploded: true}}\n" + explodedCLISource
	flag, dir, err := isometricCLI(t, explodedCLISource, nil, "--isometric-exploded", "--scale=.2", "stack.svg")
	if err != nil {
		t.Fatal(err)
	}
	if len(flag) != 0 {
		t.Fatal("file export also wrote to stdout")
	}
	file, err := os.ReadFile(filepath.Join(dir, "stack.svg"))
	if err != nil {
		t.Fatal(err)
	}
	_, links := inspectIsometricSVG(t, file)
	for _, url := range []string{"https://example.com/press", "https://example.com/folio", "https://example.com/crate"} {
		if !containsIsometricString(links, url) {
			t.Fatalf("combined SVG lost %s: %v", url, links)
		}
	}
	for _, tc := range []struct {
		name, source string
		env          []string
	}{
		{"source", fromSource, nil}, {"environment", explodedCLISource, []string{"D2_ISOMETRIC_EXPLODED=true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := isometricCLI(t, tc.source, tc.env, "--scale=.2", "--stdout-format=svg", "-")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, file) {
				t.Fatal("different option sources produced different stacks")
			}
		})
	}
	selected, _, err := isometricCLI(t, fromSource, nil, "--target=studio.*", "--scale=.2", "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := isometricCLI(t, fromSource, nil, "--target=layers.studio.*", "--scale=.2", "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	_, links = inspectIsometricSVG(t, selected)
	if !bytes.Equal(selected, canonical) || !containsIsometricString(links, "https://example.com/folio") || containsIsometricString(links, "https://example.com/crate") {
		t.Fatalf("unexpected selected subtree: %v", links)
	}
	// A relative board link in studio must point at an actual floor ID
	// after the subtree becomes a standalone SVG.
	var resolved bool
	for _, href := range links {
		if id, ok := strings.CutPrefix(href, "#"); ok && bytes.Contains(selected, []byte(`id="`+id+`"`)) {
			resolved = true
		}
	}
	if !resolved {
		t.Fatalf("selected stack lost its internal destination: %v", links)
	}
	flat, _, err := isometricCLI(t, fromSource, nil, "--isometric=false", "--target=studio", "--stdout-format=svg", "-")
	if err != nil || bytes.Contains(flat, []byte("D2 isometric diagram")) || !bytes.Contains(flat, []byte("</svg>")) {
		t.Fatalf("explicit --isometric=false did not disable the exploded source setting: %v", err)
	}
	_, _, err = isometricCLI(t, fromSource, nil, "--isometric", "--isometric-exploded=false", "--stdout-format=svg", "-")
	if err == nil || !strings.Contains(err.Error(), "requires one board") {
		t.Fatalf("expected standard per-board SVG behavior, got %v", err)
	}
}

func TestIsometricExplodedCLIPageAndSlide(t *testing.T) {
	render := func(format string) []byte {
		t.Helper()
		out, _, err := isometricCLI(t, explodedCLISource, nil, "--isometric-exploded", "--scale=.15", "--stdout-format="+format, "-")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	pixels := render("png")
	if _, err := png.DecodeConfig(bytes.NewReader(pixels)); err != nil {
		t.Fatal(err)
	}
	pdf := render("pdf")
	if _, err := testutil.ComparePDFImages(pdf, [][]byte{pixels}, imagediff.Options{}); err != nil {
		t.Fatal(err)
	}
	info, err := testutil.InspectD2PDF(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Pages) != 1 || !containsIsometricString(info.ExternalLinks, "https://example.com/folio") {
		t.Fatalf("unexpected PDF pages/links: %+v", info)
	}
	slides := render("pptx")
	if err := pptx.Validate(slides, 1); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pixels, readZipMember(t, slides, "ppt/media/slide1Image.png")) {
		t.Fatal("slide must embed the single stack PNG")
	}
	animation, err := gif.DecodeAll(bytes.NewReader(render("gif")))
	if err != nil || len(animation.Image) == 0 {
		t.Fatalf("invalid single-scene GIF: %v", err)
	}
	_, _, err = isometricCLI(t, explodedCLISource, nil, "--isometric-exploded", "--animate-interval=200", "--stdout-format=gif", "-")
	if err == nil || !strings.Contains(err.Error(), "slideshow") {
		t.Fatalf("unexpected interval result: %v", err)
	}
}
