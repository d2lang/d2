package lspserver

import (
	"math"
	"strconv"

	"github.com/mazznoer/csscolorparser"

	"github.com/d2lang/d2/d2ast"
)

func colors(root *d2ast.Map) []colorInformation {
	out := []colorInformation{}
	var visit func(d2ast.Node, bool)
	visit = func(n d2ast.Node, inStyleMap bool) {
		switch n := n.(type) {
		case *d2ast.Map, *d2ast.Array:
			for _, child := range n.Children() {
				visit(child, inStyleMap)
			}
		case *d2ast.Key:
			path := attributePath(n)
			last := ""
			inStyle := inStyleMap
			if path != nil && len(path.Path) > 0 {
				i := len(path.Path) - 1
				last = d2ast.Keyword(path.Path[i].Unbox())
				if i > 0 {
					inStyle = d2ast.Keyword(path.Path[i-1].Unbox()) == "style"
				}
			}
			if inStyle && (last == "fill" || last == "stroke" || last == "font-color") {
				if c := colorValue(n); c != nil {
					out = append(out, *c)
				}
			}
			visit(n.Value.Unbox(), last == "style" && n.Value.Map != nil)
		}
	}
	if root != nil {
		visit(root, false)
	}
	return out
}

func colorValue(key *d2ast.Key) *colorInformation {
	value := stringValue(key)
	if value == nil {
		return nil
	}
	c, err := csscolorparser.Parse(value.ScalarString())
	if err != nil {
		return nil
	}
	c = c.Clamp()
	// Relative CSS color functions can compute NaN, which JSON cannot represent.
	if math.IsNaN(c.R + c.G + c.B + c.A) {
		return nil
	}
	return &colorInformation{
		Range: toLSPRange(value.GetRange()),
		Color: lspColor{Red: c.R, Green: c.G, Blue: c.B, Alpha: c.A},
	}
}

func presentColor(p colorPresentationParams) []colorPresentation {
	rgb := csscolorparser.Color{R: p.Color.Red, G: p.Color.Green, B: p.Color.Blue, A: 1}
	label := strconv.Quote(rgb.HexString())
	return []colorPresentation{{Label: label, TextEdit: &textEdit{Range: p.Range, NewText: label}}}
}
