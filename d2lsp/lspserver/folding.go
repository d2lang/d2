package lspserver

import "github.com/d2lang/d2/d2ast"

func folds(root *d2ast.Map) []foldingRange {
	out := []foldingRange{}
	if root == nil {
		return out
	}
	d2ast.Walk(root, func(n d2ast.Node) bool {
		r := n.GetRange()
		end := r.End.Line
		var kind string
		switch n := n.(type) {
		case *d2ast.Map:
			// The parser stores consecutive # lines in a single Comment node.
			for _, node := range n.Nodes {
				if c := node.Comment; c != nil && !c.Range.OneLine() {
					out = append(out, foldingRange{StartLine: c.Range.Start.Line, EndLine: c.Range.End.Line, Kind: "comment"})
				}
			}
			if n == root {
				return true
			}
			end--
		case *d2ast.Array, *d2ast.BlockString:
			end-- // leave the closing delimiter visible
		case *d2ast.BlockComment:
			kind = "comment"
		default:
			return true
		}
		if end > r.Start.Line {
			out = append(out, foldingRange{StartLine: r.Start.Line, EndLine: end, Kind: kind})
		}
		return true
	})
	return out
}
