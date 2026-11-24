package d2animate

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
)

func TestShareFrameGraphics(t *testing.T) {
	const hash = "d2-100"
	path := "M0 0" + strings.Repeat(" L1 1", 80)
	gradient := "grad-" + strings.Repeat("a", 40)
	label := "<text>#" + gradient + "</text>"
	mask := `<mask id="d2-100" maskUnits="userSpaceOnUse"><rect fill="white" width="500" height="400"/></mask>`
	resources := `<defs><linearGradient id="` + gradient + `"><stop offset="0" stop-color="red"/></linearGradient></defs>` + mask
	var frames [][]byte
	for i := 0; i < 8; i++ {
		stroke := fmt.Sprintf(`<path d="%s" fill="url('#%s')" mask="url(#d2-100)" stroke-width="%d"/>`, path, gradient, i)
		root := `<g class="d2-100 " viewBox="0 0 500 400">`
		frames = append(frames, []byte(root+resources+`<use xlink:href = "#`+gradient+`"/><g class="authored">`+label+stroke+label+`</g></g>`))
	}
	defs, reduced := shareFrameGraphics(frames, hash)
	if len(defs) == 0 || bytes.Count(bytes.Join(reduced, nil), []byte("<linearGradient ")) != 1 || bytes.Count(bytes.Join(reduced, nil), []byte("<mask ")) != 1 || bytes.Count(defs, []byte(path)) != 1 || bytes.Contains(defs, []byte("<text")) {
		t.Fatalf("expected shared paint and path geometry, got %s", defs)
	}
	doc := bytes.NewBufferString(`<svg xmlns:xlink="http://www.w3.org/1999/xlink">`)
	doc.Write(defs)
	for i, fr := range reduced {
		if bytes.Count(fr, []byte(label)) != 2 || !bytes.Contains(fr, []byte(`<g class="authored">`+label+`<use `)) || !bytes.Contains(fr, []byte(fmt.Sprintf(`stroke-width="%d"/>%s`, i, label))) {
			t.Fatalf("frame %d changed its labels, order or per-frame stroke", i)
		}
		if bytes.Contains(fr, []byte(`href = "#`+gradient)) || bytes.Contains(fr, []byte("url(#d2-100)")) || bytes.Contains(fr, []byte("url('#"+gradient+"')")) {
			t.Errorf("original paint reference in frame %d", i)
		}
		doc.Write(fr)
	}
	doc.WriteString("</svg>")
	d := xml.NewDecoder(doc)
	var refs []string
	ids := map[string]bool{}
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if el, ok := token.(xml.StartElement); ok {
			for _, a := range el.Attr {
				switch a.Name.Local {
				case "id":
					if ids[a.Value] {
						t.Fatalf("duplicate ID %s", a.Value)
					}
					ids[a.Value] = true
				case "href", "fill", "mask":
					if _, id, ok := strings.Cut(a.Value, "#"); ok {
						refs = append(refs, strings.TrimRight(id, "')"))
					}
				}
			}
		}
	}
	for _, r := range refs {
		if !ids[r] {
			t.Errorf("missing reference %s", r)
		}
	}
	defs2, reduced2 := shareFrameGraphics(frames, hash)
	if !bytes.Equal(defs, defs2) || !bytes.Equal(bytes.Join(reduced, nil), bytes.Join(reduced2, nil)) {
		t.Error("nondeterministic graphics sharing")
	}
}

func TestFrameGraphicsFallback(t *testing.T) {
	const hash = "d2-100"
	static := `<g class="shape"><path d="M0 0` + strings.Repeat(" L1 1", 50) + `"/></g>`
	makeFrames := func(extra string) [][]byte {
		base := `<g class="d2-100">` + static
		return [][]byte{[]byte(base + "</g>"), []byte(base + "</g>"), []byte(base + extra + "</g>")}
	}
	for name, extra := range map[string]string{
		"native link":                   `<a href="https://example.org/"><text>Documentation</text></a>`,
		"named fragment":                `<path id="public-target" d="M0 0"/>`,
		"connection with its own clock": `<path class="animated-connection connection" d="M0 0"/>`,
		"inline animation":              `<g style="animation: slide 1s infinite"/>`,
		"custom css":                    `<style>g:nth-child(2){fill:red}</style>`,
		"entity fragment":               `<use href="&#35;target"/>`,
		"noncanonical url":              `<rect fill="url( #target )"/>`,
		"external use":                  `<use href="external.svg#node"/>`,
		"uppercase CSS URL":             `<rect fill="URL(#d2-10)"/>`,
		"percent-encoded reference":     `<rect fill="url(#d2-%31%30)"/>`,
		"percent-encoded href":          `<use href="#d2-%31%30"/>`,
		"CSS escape":                    `<rect fill="u\72 l(#d2-10)"/>`,
		"CDATA label":                   `<text><![CDATA[<use href="#d2-10"/>]]></text>`,
		"retained resource before pool": `<g fill="red"><mask id="d2-10"><rect fill="white"/></mask></g><mask id="d2-10"><rect fill="white"/></mask>`,
		"aria label":                    `<rect aria-label="status"/>`,
		"id conflict":                   `<g class="d2x2su0"/>`,
		"script":                        `<script/>`,
		"foreign content":               `<foreignObject/>`,
		"conflicting resources":         `<mask id="d2-10"><rect fill="black"/></mask><mask id="d2-10"><rect fill="white"/></mask>`,
	} {
		t.Run(name, func(t *testing.T) {
			input := makeFrames(extra)
			defs, out := shareFrameGraphics(input, hash)
			if len(defs) > 0 || !bytes.Equal(bytes.Join(input, nil), bytes.Join(out, nil)) {
				t.Fatal("expected original frame sequence")
			}
		})
	}
	t.Run("custom base stylesheet", func(t *testing.T) {
		saved := d2svg.BaseStylesheet
		d2svg.BaseStylesheet += "\ng:nth-child(2) {fill:red}"
		t.Cleanup(func() { d2svg.BaseStylesheet = saved })
		input := makeFrames("")
		if defs, _ := shareFrameGraphics(input, hash); defs != nil {
			t.Fatal("unexpected sharing with custom CSS")
		}
	})
}

func TestTaggedObjectKeepsLiveGeometry(t *testing.T) {
	const hash = "d2-100"
	path := `<path d="M0 0` + strings.Repeat(" L1 1", 120) + `" class="shape" fill="red"/>`
	tagged := `<g class="YQ== my-tag"><g class="shape">` + path + `</g><text>Label</text></g>`
	untagged := `<g class="Yg=="><g class="shape">` + path + `</g></g>`
	raw := []byte(`<g class="d2-100">` + tagged + untagged + `</g>`)
	defs, frames := shareFrameGraphics([][]byte{raw, raw, raw, raw}, hash)
	if len(defs) == 0 {
		t.Fatal("expected stationary geometry to be shared")
	}
	for _, frame := range frames {
		if !bytes.Contains(frame, []byte(tagged)) || !bytes.Contains(frame, []byte(`<g class="Yg=="><g class="shape"><use `)) {
			t.Fatal("lost selectors or event targets")
		}
	}
}

func TestConnectionMarkerScopes(t *testing.T) {
	const hash = "d2-100"
	marker := func(id int, extra string) string {
		return fmt.Sprintf(`<marker id="mk-d2-%d"><path d="M0 0L10 5 0 10Z" fill="red"%s/></marker><path marker-end="url(#mk-d2-%d)" d="M0 0L100 100"/>`, id, extra, id)
	}
	build := func(connection int, width int) []byte {
		return []byte(fmt.Sprintf(`<g class="%s" width="%d"><g class="edge">%s</g><g class="edge my-tag">%s</g><g fill="blue"><g class="edge">%s</g></g><g class="edge">%s</g></g>`,
			hash, width, marker(connection, ""), marker(connection+1, ""), marker(connection+2, ""), marker(connection+3, ` id="pencil-d2-named"`)))
	}
	frames := [][]byte{build(10, 100), build(20, 100), build(30, 200)}
	_, reduced := shareFrameGraphics(frames, hash)
	all := bytes.Join(reduced, nil)
	// Only the first two plain markers share both ancestors; distinct tagged
	// parents, inherited paint, child IDs or viewport dimensions stay scoped.
	if got := bytes.Count(all, []byte("<marker ")); got != 11 {
		t.Fatalf("got %d markers, want 11", got)
	}
}
