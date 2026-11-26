package lspserver

import (
	"math"
	"testing"

	"github.com/d2lang/util-go/assert"
)

func TestL73_DocumentColor(t *testing.T) {
	const uri = "untitled:colors"
	c := newTestClient(t)
	c.open(uri, "a.style.fill: red\nb.style.stroke: \"#3B6FD9\"\nc.style.fill: \"linear-gradient(red, blue)\"\nd: {style: {font-color: blue}}\ne: {fill: green}")
	got := decode[[]colorInformation](t, c.request("textDocument/documentColor", docParams(uri)))
	assert.Equal(t, 3, len(got))
	for i, want := range []colorInformation{
		{lineRange(0, 14, 17), lspColor{1, 0, 0, 1}},
		{lineRange(1, 16, 25), lspColor{59.0 / 255, 111.0 / 255, 217.0 / 255, 1}},
		{lineRange(3, 24, 28), lspColor{0, 0, 1, 1}},
	} {
		assert.Equal(t, want.Range, got[i].Range)
		a, b := got[i].Color, want.Color
		for _, diff := range []float64{a.Red - b.Red, a.Green - b.Green, a.Blue - b.Blue, a.Alpha - b.Alpha} {
			if math.Abs(diff) > 1e-6 {
				t.Errorf("at %+v got %+v, want %+v", want.Range, a, b)
			}
		}
	}
}

func TestL74_ColorPresentation(t *testing.T) {
	const uri = "untitled:colors"
	c := newTestClient(t)
	c.open(uri, "style.fill: red")
	r := lineRange(0, 12, 15)
	got := decode[[]colorPresentation](t, c.request("textDocument/colorPresentation", colorPresentationParams{
		TextDocument: textDocumentIdentifier{URI: uri}, Range: r, Color: lspColor{Red: 1, Alpha: 0.5},
	}))
	assert.JSON(t, []colorPresentation{{Label: `"#ff0000"`, TextEdit: &textEdit{Range: r, NewText: `"#ff0000"`}}}, got)
}

func TestTransparentColorPresentation(t *testing.T) {
	const uri = "untitled:colors"
	c := newTestClient(t)
	c.open(uri, "style.fill: transparent")
	color := decode[[]colorInformation](t, c.request("textDocument/documentColor", docParams(uri)))[0]
	got := decode[[]colorPresentation](t, c.request("textDocument/colorPresentation", colorPresentationParams{
		TextDocument: textDocumentIdentifier{URI: uri}, Range: color.Range, Color: color.Color,
	}))
	assert.JSON(t, []colorPresentation{{Label: "transparent", TextEdit: &textEdit{Range: color.Range, NewText: "transparent"}}}, got)
}
