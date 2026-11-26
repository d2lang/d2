package d2svgimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStructureGolden pins ImportNode's result (a digest of the imported
// scene and metrics, or the exact error) over a deterministic set of
// generated SVG documents that combine the supported and unsupported
// elements, resources, references, stylesheets and metadata in valid and
// misplaced positions. Regenerate with D2SVGIMPORT_UPDATE_STRUCTURE_GOLDEN=1
// only for an intended behavior change.
func TestStructureGolden(t *testing.T) {
	const cases = 6000
	path := filepath.Join("testdata", "structure_golden.txt")
	var got strings.Builder
	for seed := 0; seed < cases; seed++ {
		fmt.Fprintf(&got, "%d %s\n", seed, structureGoldenResult(int64(seed)))
	}
	if os.Getenv("D2SVGIMPORT_UPDATE_STRUCTURE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() == string(want) {
		return
	}
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(got.String(), "\n")
	mismatches := 0
	for i := range gotLines {
		if i >= len(wantLines) || gotLines[i] != wantLines[i] {
			mismatches++
			if mismatches <= 10 {
				_, detail := structureGoldenImport(int64(i))
				t.Errorf("seed %d: got %q, want %q (%s)", i, gotLines[i], wantLines[min(i, len(wantLines)-1)], detail)
			}
		}
	}
	t.Fatalf("%d of %d generated documents changed", mismatches, cases)
}

func structureGoldenImport(seed int64) (string, string) {
	g := &svgGen{rng: rand.New(rand.NewSource(seed)), strict: seed%2 == 0}
	source := g.document()
	result, err := ImportNode(context.Background(), "generated.svg", []byte(source), generousImportLimits())
	if err != nil {
		return "error: " + err.Error(), source
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "unencodable: " + err.Error(), source
	}
	return "result: " + string(encoded), source
}

func structureGoldenResult(seed int64) string {
	outcome, _ := structureGoldenImport(seed)
	sum := sha256.Sum256([]byte(outcome))
	return outcome[:1] + hex.EncodeToString(sum[:6])
}

type svgGen struct {
	rng    *rand.Rand
	strict bool
	ids    []string
	b      strings.Builder
}

func (g *svgGen) chance(n int) bool { return g.rng.Intn(n) == 0 }

func (g *svgGen) bad(n int) bool { return !g.strict && g.chance(n) }

func (g *svgGen) one(values ...string) string { return values[g.rng.Intn(len(values))] }

// oneOK picks one of good, or, unless strict, one of good and bad together.
func (g *svgGen) oneOK(good []string, bad ...string) string {
	if g.strict {
		return g.one(good...)
	}
	return g.one(append(append([]string(nil), good...), bad...)...)
}

func (g *svgGen) document() string {
	g.ids = []string{"g0", "c0", "p0", "m0", "missing"}
	g.b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"`)
	namespaces := g.chance(3)
	if namespaces {
		g.b.WriteString(` xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:cc="http://creativecommons.org/ns#" xmlns:dc="http://purl.org/dc/elements/1.1/"`)
	}
	g.b.WriteString(` viewBox="0 0 ` + g.oneOK([]string{"40 30", "10 10"}, "0 5") + `"`)
	if g.chance(2) {
		g.b.WriteString(` width="` + g.oneOK([]string{"80", "20px"}, "50%") + `" height="60"`)
	}
	if g.chance(4) {
		g.b.WriteString(` preserveAspectRatio="` + g.one("none", "xMidYMid slice", "xMinYMax meet") + `"`)
	}
	g.b.WriteString(">")
	if g.chance(3) {
		g.metadata(namespaces)
	}
	if g.strict || g.chance(2) {
		g.defs()
	}
	if g.chance(4) {
		g.b.WriteString(`<style>` + g.oneOK([]string{".a{fill:red}", ".b{opacity:.5}", ".a{stroke:blue;stroke-width:2}"}, "rect{stroke:blue}", ".b{opacity:.5} #x{fill:url(#g0)}", "@import url(x.css);", ".a{fill:") + `</style>`)
	}
	for n := g.rng.Intn(5); n > 0; n-- {
		g.content(0)
	}
	if g.bad(10) {
		g.b.WriteString(g.one("<stop/>", "<clipPath><rect/></clipPath>", "<cc:Work/>", "<dc:format>x</dc:format>", "<unknown/>"))
	}
	g.b.WriteString("</svg>")
	return g.b.String()
}

func (g *svgGen) metadata(namespaces bool) {
	format := "image/svg+xml"
	if g.bad(4) {
		format = "text/plain"
	}
	kind := g.rng.Intn(4)
	if g.strict && !namespaces && kind != 1 {
		kind = 1
	}
	switch kind {
	case 0:
		g.b.WriteString(`<metadata><rdf:RDF><cc:Work><dc:format>` + format + `</dc:format><dc:type rdf:resource="http://purl.org/dc/dcmitype/StillImage"/><dc:title>t</dc:title></cc:Work></rdf:RDF></metadata>`)
	case 1:
		g.b.WriteString(`<title>x</title><desc>y</desc>`)
	case 2:
		if g.strict {
			g.b.WriteString(`<metadata><rdf:RDF><cc:Work/></rdf:RDF></metadata>`)
		} else {
			g.b.WriteString(g.one(`<metadata/>`, `<metadata><rdf:RDF/><rdf:RDF/></metadata>`, `<title><g/></title>`, `<g><metadata/></g>`, `<metadata><rdf:RDF><cc:Work><dc:type/><dc:type/></cc:Work></rdf:RDF></metadata>`))
		}
	default:
		g.b.WriteString(`<sodipodi:namedview xmlns:sodipodi="http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd"/>`)
	}
}

func (g *svgGen) stops() {
	for n := g.rng.Intn(4); n > 0; n-- {
		g.b.WriteString(`<stop offset="` + g.oneOK([]string{"0", "0.5", "1"}, "30%", "x") + `" stop-color="` + g.oneOK([]string{"red", "#00f", "rgb(1,2,3)"}, "currentColor") + `"`)
		if g.chance(3) {
			g.b.WriteString(` stop-opacity="` + g.oneOK([]string{"0.5", "0"}, "2", "-1") + `"`)
		}
		if g.bad(4) {
			g.b.WriteString(` style="stop-color:green"`)
		}
		g.b.WriteString("/>")
	}
	if g.bad(12) {
		g.b.WriteString("<rect/>")
	}
}

func (g *svgGen) defs() {
	g.b.WriteString("<defs>")
	if g.strict || g.chance(2) {
		g.b.WriteString(`<linearGradient id="g0" x1="0" y1="0" x2="` + g.one("1", "100%", "0") + `" y2="0" gradientUnits="` + g.oneOK([]string{"objectBoundingBox", "userSpaceOnUse"}, "bogus") + `" spreadMethod="` + g.one("pad", "reflect", "repeat") + `"`)
		if g.chance(3) {
			g.b.WriteString(` gradientTransform="` + g.oneOK([]string{"rotate(30)", "translate(1 2) skewX(10)"}, "scale(0)") + `"`)
		}
		if g.bad(5) {
			g.b.WriteString(` xlink:href="#g1"`)
		}
		g.b.WriteString(">")
		g.stops()
		g.b.WriteString("</linearGradient>")
	}
	if g.strict || g.chance(2) {
		g.b.WriteString(`<radialGradient id="g1" cx="0.5" cy="0.5" r="` + g.oneOK([]string{"0.5", "0"}, "-1") + `" fx="0.4" fy="0.5">`)
		g.stops()
		g.b.WriteString("</radialGradient>")
	}
	if g.strict || g.chance(2) {
		g.b.WriteString(`<clipPath id="c0"`)
		if g.chance(4) {
			g.b.WriteString(` clipPathUnits="` + g.oneOK([]string{"userSpaceOnUse"}, "objectBoundingBox") + `"`)
		}
		g.b.WriteString(">")
		if !g.bad(6) {
			g.shape(true)
		}
		if g.bad(8) {
			g.shape(true)
		}
		g.b.WriteString("</clipPath>")
	}
	if g.strict || g.chance(3) {
		g.b.WriteString(`<g id="p0">`)
		g.shape(false)
		g.b.WriteString(`</g>`)
	}
	if g.strict || g.chance(2) {
		g.b.WriteString(`<rect id="s0" x="1" y="1" width="4" height="` + g.one("4", "0") + `"/>`)
	}
	if g.strict || g.chance(4) {
		g.b.WriteString(`<mask id="m0" x="0" y="0" width="1" height="1"><rect x="0" y="0" width="10" height="10" fill="white"/></mask>`)
	}
	if g.strict || g.chance(5) {
		g.b.WriteString(`<filter id="f0"><feGaussianBlur stdDeviation="` + g.one("1", "0", "2 1") + `"/></filter>`)
	}
	g.b.WriteString("</defs>")
}

func (g *svgGen) attrs() {
	if g.chance(3) {
		g.b.WriteString(` fill="` + g.oneOK([]string{"red", "none", "url(#g0)", "url(#g1)", "currentColor", "#abc"}, "url(#missing)") + `"`)
	}
	if g.chance(3) {
		g.b.WriteString(` stroke="` + g.one("blue", "url(#g0)", "none") + `" stroke-width="` + g.oneOK([]string{"1", "2.5", "0"}, "-1") + `"`)
		if g.chance(3) {
			g.b.WriteString(` stroke-dasharray="` + g.one("2 1", "0 0", "3") + `" stroke-linecap="` + g.one("round", "square", "butt") + `"`)
		}
	}
	if g.chance(4) {
		g.b.WriteString(` transform="` + g.one("translate(2,3)", "scale(2)", "rotate(45 5 5)", "matrix(1 0 0 1 2 2)", "scale(0)") + `"`)
	}
	if g.chance(5) {
		g.b.WriteString(` opacity="` + g.one("0.5", "1", "0") + `"`)
	}
	if g.chance(6) {
		g.b.WriteString(` clip-path="url(#` + g.oneOK([]string{"c0"}, "missing", "g0") + `)"`)
	}
	if g.chance(8) {
		g.b.WriteString(` mask="url(#m0)"`)
	}
	if g.chance(8) {
		g.b.WriteString(` filter="url(#f0)"`)
	}
	if g.chance(5) {
		g.b.WriteString(` class="` + g.one("a", "b", "a b") + `"`)
	}
	if g.chance(6) {
		g.b.WriteString(` style="` + g.one("fill:green", "stroke:red;stroke-width:3", "display:none", "visibility:hidden", "fill-rule:evenodd") + `"`)
	}
}

func (g *svgGen) shape(clip bool) {
	switch g.rng.Intn(6) {
	case 0:
		g.b.WriteString(`<rect x="1" y="2" width="` + g.oneOK([]string{"10", "0"}, "50%", "-3") + `" height="8"`)
		if g.chance(3) {
			g.b.WriteString(` rx="2"`)
		}
	case 1:
		g.b.WriteString(`<circle cx="10" cy="10" r="` + g.one("5", "0") + `"`)
	case 2:
		g.b.WriteString(`<ellipse cx="10" cy="10" rx="5" ry="3"`)
	case 3:
		g.b.WriteString(`<path d="` + g.oneOK([]string{"M1 1 L10 10 Z", "M0 0 C 5 5 10 0 15 5", "M2 2 h5 v5 a2 2 0 0 1 -3 3 z", "M1 1 Q 3 3 5 1 T 9 1"}, "L1 1") + `"`)
	case 4:
		g.b.WriteString(`<line x1="0" y1="0" x2="` + g.one("10", "0") + `" y2="5"`)
	default:
		if clip {
			g.b.WriteString(`<use xlink:href="#` + g.oneOK([]string{"s0"}, "p0", "c0", "missing") + `"`)
		} else {
			g.b.WriteString(`<polygon points="` + g.oneOK([]string{"1,1 5,1 3,4"}, "1,1", "1 1 5 1 3") + `"`)
		}
	}
	if !clip {
		g.attrs()
	} else if g.bad(5) {
		g.b.WriteString(` fill="red"`)
	}
	g.b.WriteString("/>")
}

func (g *svgGen) content(depth int) {
	switch g.rng.Intn(6) {
	case 0, 1, 2:
		g.shape(false)
	case 3:
		if depth > 3 {
			g.shape(false)
			return
		}
		g.b.WriteString("<g")
		if g.bad(4) {
			g.b.WriteString(` id="x"`)
		}
		g.attrs()
		g.b.WriteString(">")
		for n := g.rng.Intn(3); n > 0; n-- {
			g.content(depth + 1)
		}
		if g.chance(8) {
			g.defs()
		}
		g.b.WriteString("</g>")
	case 4:
		g.b.WriteString(`<use xlink:href="#` + g.oneOK([]string{"p0"}, g.ids...) + `" x="` + g.one("0", "3") + `"`)
		g.attrs()
		g.b.WriteString("/>")
	default:
		if depth > 2 {
			return
		}
		g.b.WriteString(`<svg x="2" y="2" width="` + g.oneOK([]string{"10"}, "0") + `" height="10" viewBox="0 0 5 5">`)
		g.content(depth + 1)
		g.b.WriteString("</svg>")
	}
}
