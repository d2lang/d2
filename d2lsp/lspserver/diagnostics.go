package lspserver

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/lib/localfile"
)

func toLSPRange(r d2ast.Range) lspRange {
	return lspRange{
		Start: lspPosition{r.Start.Line, r.Start.Column},
		End:   lspPosition{r.End.Line, r.End.Column},
	}
}

func comparePosition(a, b lspPosition) int {
	if n := cmp.Compare(a.Line, b.Line); n != 0 {
		return n
	}
	return cmp.Compare(a.Character, b.Character)
}

func lineZeroDiagnostic(severity int, message string) []diagnostic {
	return []diagnostic{{Range: lspRange{End: lspPosition{Character: 1}}, Severity: severity, Source: "d2", Message: message}}
}

func (s *Server) computeDiagnostics(ctx context.Context, doc document) (out []diagnostic) {
	defer func() {
		if v := recover(); v != nil {
			s.log.Printf("diagnosing %s: %v\n%s", doc.uri, v, debug.Stack())
			out = lineZeroDiagnostic(1, fmt.Sprintf("internal error: %v", v))
		}
	}()
	if len(doc.text) > maxDocumentBytes {
		return lineZeroDiagnostic(3, "file is too large for D2 language features")
	}
	_, _, err := d2compiler.Compile(doc.path, strings.NewReader(doc.text), &d2compiler.CompileOptions{
		Context: ctx, UTF16Pos: true, FS: s.fileSystem(doc),
	})
	if err == nil || ctx.Err() != nil {
		return []diagnostic{}
	}
	var pe *d2parser.ParseError
	if !errors.As(err, &pe) {
		return lineZeroDiagnostic(1, err.Error())
	}

	var imports map[string]lspRange
	var fallback lspRange
	out = make([]diagnostic, 0, len(pe.Errors))
	for _, e := range pe.Errors {
		r := toLSPRange(e.Range)
		message := strings.TrimPrefix(e.Message, e.Range.String()+": ")
		d := diagnostic{Range: r, Source: "d2", Message: message, Severity: 1}
		if e.Range.Path != doc.path {
			if imports == nil {
				imports, fallback = importRanges(ctx, doc)
			}
			d.Range = fallback
			if source, ok := imports[e.Range.Path]; ok {
				d.Range = source
			}
			d.Message = fmt.Sprintf("%s:%s: %s", filepath.Base(e.Range.Path), e.Range.Start, message)
			d.RelatedInformation = []relatedInformation{{Location: location{URI: s.uriForPath(doc, e.Range.Path), Range: r}, Message: message}}
		}
		if comparePosition(d.Range.End, d.Range.Start) <= 0 {
			d.Range.End = d.Range.Start
			d.Range.End.Character++
		}
		out = append(out, d)
	}
	slices.SortStableFunc(out, func(a, b diagnostic) int { return comparePosition(a.Range.Start, b.Range.Start) })
	return out
}

// Resolve imports the same way as the IR compiler, including implicit .d2 extensions.
func importPath(parent string, imp *d2ast.Import) string {
	name := imp.PathWithPre()
	if name == "" || parent == "" {
		return ""
	}
	if filepath.Ext(name) != ".d2" {
		name += ".d2"
	}
	return localfile.Unrestricted().ResolveImportPath(parent, name)
}

func importRanges(ctx context.Context, doc document) (map[string]lspRange, lspRange) {
	root, _ := d2parser.ParseContext(ctx, doc.path, strings.NewReader(doc.text), &d2parser.ParseOptions{UTF16Pos: true})
	first := lspRange{End: lspPosition{Character: 1}}
	ranges := make(map[string]lspRange)
	d2ast.Walk(root, func(n d2ast.Node) bool {
		if ctx.Err() != nil {
			return false
		}
		if imp, ok := n.(*d2ast.Import); ok {
			path := importPath(doc.path, imp)
			if _, seen := ranges[path]; !seen {
				r := toLSPRange(imp.GetRange())
				if len(ranges) == 0 {
					first = r
				}
				ranges[path] = r
			}
		}
		return true
	})
	return ranges, first
}
