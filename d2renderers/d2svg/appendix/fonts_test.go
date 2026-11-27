package appendix_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/d2svg/appendix"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestAppendixFallbackFonts(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	opts := &d2svg.RenderOpts{}
	script := `api: API {tooltip: "Verify office traffic: Ж é 漢"}
api -> db`
	diagram, _, err := d2lib.Compile(context.Background(), script, &d2lib.CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2dagrelayout.DefaultLayout, nil
		},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	svg, err := d2svg.Render(diagram, opts)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := appendix.AppendRendered(diagram, opts, ruler, svg)
	if err != nil {
		t.Fatal(err)
	}
	regular := []byte(`.text{font-family:font-regular}`)
	bold := []byte(`.text-bold{font-family:font-bold}`)
	if !bytes.Contains(rendered, regular) || bytes.Contains(rendered, bold) {
		t.Fatal("expected only the full regular font for the tooltip")
	}

	if bytes.LastIndex(rendered, []byte("</text>")) > bytes.LastIndex(rendered, []byte("<style>")) {
		t.Fatal("standalone appendix fallback must follow the footnote text for ImageIO")
	}

	// A registered font URL can contain a literal </style> inside CDATA.
	// In this case fallbacks must follow the entire original stylesheet.
	const uri = "https://example.org/a (regular)</style>.woff?a=1&b=2"
	withURL := bytes.Replace(svg, []byte("<style>"), []byte(`<style><![CDATA[@font-face{font-family:external;src:url("`+uri+`")}`), 1)
	withURL = bytes.Replace(withURL, []byte("}</style>"), []byte("}]]></style>"), 1)
	withFallback, err := appendix.AppendRendered(diagram, opts, ruler, withURL)
	if err != nil {
		t.Fatal(err)
	}
	d := xml.NewDecoder(bytes.NewReader(withFallback))
	var sheets []string
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if el, ok := token.(xml.StartElement); ok && el.Name.Local == "style" {
			var css string
			if err := d.DecodeElement(&css, &el); err != nil {
				t.Fatal(err)
			}
			sheets = append(sheets, css)
		}
	}
	if len(sheets) != 2 || !strings.Contains(sheets[0], `url("`+uri+`")`) || !strings.Contains(sheets[1], string(regular)) {
		t.Fatal("appendix altered the original font URL or misplaced the fallback face")
	}

	// Consumers can pass SVG with their own styles, even ones referring to
	// the fallback family by name. Both global aliases remain available.
	custom := bytes.Replace(svg, []byte("</svg></svg>"), []byte(`<style>.x {font-family:font-bold}</style><text class="x">Custom</text></svg></svg>`), 1)
	preserved, err := appendix.AppendWithError(diagram, opts, ruler, custom)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(preserved, []byte(`font-family: "font-regular"`)) || !bytes.Contains(preserved, []byte(`font-family: "font-bold"`)) {
		t.Fatal("custom SVG lost a full font")
	}
}
