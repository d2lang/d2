package d2svg

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// These remaining format strings interpolate renderer-generated numeric or
// pre-serialized fragments. New dynamic SVG markup must use lib/svg instead of
// extending this allowlist.
var rawSVGFormatAllowlist = map[string]int{
	"d2renderers/d2svg/appendix/appendix.go:\"<circle cx=\\\"%d\\\" cy=\\\"%d\\\" r=\\\"%d\\\" fill=\\\"white\\\" stroke=\\\"#DEE1EB\\\" />\"":                                                                                            1,
	"d2renderers/d2svg/appendix/appendix.go:\"<g class=\\\"appendix\\\" x=\\\"%d\\\" y=\\\"%d\\\" width=\\\"%d\\\" height=\\\"100%%\\\">%s</g>\\n\"":                                                                                      1,
	"d2renderers/d2svg/appendix/appendix.go:\"<g transform=\\\"translate(%d %d)\\\" class=\\\"appendix-icon\\\">%s</g>\"":                                                                                                                 1,
	"d2renderers/d2svg/appendix/appendix.go:\"<style type=\\\"text/css\\\"><![CDATA[\\n.text {\\n\\tfont-family: \\\"font-regular\\\";\\n}\\n@font-face {\\n\\tfont-family: font-regular;\\n\\tsrc: url(\\\"%s\\\");\\n}\\n]]></style>\"": 1,
	"d2renderers/d2svg/appendix/appendix.go:\"<style type=\\\"text/css\\\"><![CDATA[\\n.text-bold {\\n\\tfont-family: \\\"font-bold\\\";\\n}\\n@font-face {\\n\\tfont-family: font-bold;\\n\\tsrc: url(\\\"%s\\\");\\n}\\n]]></style>\"":  1,
	"d2renderers/d2svg/appendix/appendix.go:\"<text class=\\\"text-bold\\\" x=\\\"%d\\\" y=\\\"%d\\\" style=\\\"font-size: %dpx;text-anchor:middle;\\\">%d</text>\"":                                                                      1,
	"d2renderers/d2svg/appendix/appendix.go:\"<text class=\\\"text\\\" x=\\\"%d\\\" y=\\\"%d\\\" style=\\\"font-size: %dpx;\\\">%s</text>\"":                                                                                              1,
	"d2renderers/d2svg/appendix/appendix.go:\"height=\\\"%s\\\"\"":                                                                                                                                      1,
	"d2renderers/d2svg/appendix/appendix.go:\"viewBox=\\\"%s %s %s %s\\\"\"":                                                                                                                            1,
	"d2renderers/d2svg/appendix/appendix.go:\"viewBox=\\\"0 0 %d %d\\\"\"":                                                                                                                              1,
	"d2renderers/d2svg/appendix/appendix.go:\"width=\\\"%s\\\"\"":                                                                                                                                       1,
	"d2renderers/d2svg/d2svg.go:\" style='opacity:%s'\"":                                                                                                                                                2,
	"d2renderers/d2svg/d2svg.go:\" style=\\\"font-size:%v\\\"\"":                                                                                                                                        2,
	"d2renderers/d2svg/d2svg.go:\" width=\\\"%d\\\" height=\\\"%d\\\"\"":                                                                                                                                1,
	"d2renderers/d2svg/d2svg.go:\"%s%s<%s class=\\\"%s\\\" width=\\\"%d\\\" height=\\\"%d\\\" viewBox=\\\"%d %d %d %d\\\">%s%s%s%s</%s>%s\"":                                                            1,
	"d2renderers/d2svg/d2svg.go:\"<defs>%s</defs>\"":                                                                                                                                                    1,
	"d2renderers/d2svg/d2svg.go:\"<g class=\\\"positioned-tooltip\\\">%s%s%s</g>\"":                                                                                                                     1,
	"d2renderers/d2svg/d2svg.go:\"<g class=\\\"shape%s\\\" %s>\"":                                                                                                                                       1,
	"d2renderers/d2svg/d2svg.go:\"<g transform=\\\"translate(%d %d)\\\" class=\\\"appendix-icon\\\">%s</g>\"":                                                                                           1,
	"d2renderers/d2svg/d2svg.go:\"<g transform=\\\"translate(%d %d)\\\" class=\\\"appendix-icon\\\"><title>%s</title>%s</g>\"":                                                                          1,
	"d2renderers/d2svg/d2svg.go:\"<g transform=\\\"translate(%d, %d) scale(%s)\\\">\"":                                                                                                                  2,
	"d2renderers/d2svg/d2svg.go:\"<g transform=\\\"translate(%s %s)\\\" class=\\\"%s\\\"%s>\"":                                                                                                          2,
	"d2renderers/d2svg/d2svg.go:\"<g transform=\\\"translate(%s %s)\\\">\"":                                                                                                                             1,
	"d2renderers/d2svg/d2svg.go:\"<marker id=\\\"%s\\\" markerWidth=\\\"%s\\\" markerHeight=\\\"%s\\\" refX=\\\"%s\\\" refY=\\\"%s\\\"\"":                                                               1,
	"d2renderers/d2svg/d2svg.go:\"<mask id=\\\"%s\\\" maskUnits=\\\"userSpaceOnUse\\\" x=\\\"%d\\\" y=\\\"%d\\\" width=\\\"%d\\\" height=\\\"%d\\\">\"":                                                 1,
	"d2renderers/d2svg/d2svg.go:\"<path d=\\\"M %s %s L %s %s S %s %s %s %s \"":                                                                                                                         1,
	"d2renderers/d2svg/d2svg.go:\"<rect x=\\\"%d\\\" y=\\\"%d\\\" width=\\\"%d\\\" height=\\\"%d\\\" fill=\\\"white\\\"></rect>\"":                                                                      1,
	"d2renderers/d2svg/d2svg.go:\"<rect x=\\\"%s\\\" y=\\\"%s\\\" width=\\\"%d\\\" height=\\\"%d\\\" fill=\\\"%s\\\"></rect>\"":                                                                         1,
	"d2renderers/d2svg/d2svg.go:\"<rect x=\\\"%s\\\" y=\\\"%s\\\" width=\\\"%s\\\" height=\\\"%s\\\" fill=\\\"%s\\\"></rect>\"":                                                                         1,
	"d2renderers/d2svg/d2svg.go:\"<style type=\\\"text/css\\\"><![CDATA[%s%s]]></style>\"":                                                                                                              1,
	"d2renderers/d2svg/d2svg.go:\"<svg xmlns=\\\"http://www.w3.org/2000/svg\\\" xmlns:xlink=\\\"http://www.w3.org/1999/xlink\\\" %s preserveAspectRatio=\\\"%s meet\\\" viewBox=\\\"0 0 %d %d\\\"%s>\"": 1,
	"d2renderers/d2svg/d2svg.go:\"<text class=\\\"text-bold\\\" x=\\\"%d\\\" y=\\\"%d\\\" style=\\\"font-size: %dpx;\\\">%s</text>\"":                                                                   1,
	"d2renderers/d2svg/d2svg.go:\"<text class=\\\"text-mono\\\" x=\\\"0\\\" y=\\\"%sem\\\">\"":                                                                                                          2,
	"d2renderers/d2svg/d2svg.go:\"<text class=\\\"text\\\" x=\\\"%d\\\" y=\\\"%d\\\" style=\\\"font-size: %dpx;\\\">%s</text>\"":                                                                        2,
	"d2renderers/d2svg/d2svg.go:\"<title>%s</title>\"":                                                                                                                                                  1,
	"d2renderers/d2svg/d2svg.go:\"<tspan %s>%s</tspan>\"":                                                                                                                                               2,
	"d2renderers/d2svg/d2svg.go:\"<tspan x=\\\"%s\\\" dy=\\\"%s\\\">%s</tspan>\"":                                                                                                                       1,
	"d2renderers/d2svg/d2svg.go:\"data-d2-version=\\\"%s\\\"\"":                                                                                                                                         1,
	"d2renderers/d2svg/d2svg.go:\"viewBox=\\\"%s %s %s %s\\\"\"":                                                                                                                                        1,
	"d2renderers/d2svg/markdown.go:\"<svg x=\\\"%s\\\" y=\\\"%s\\\" width=\\\"%d\\\" height=\\\"%d\\\" viewBox=\\\"0 0 %d %d\\\" overflow=\\\"hidden\\\">\"":                                            1,
	"d2renderers/d2svg/table.go:\"<path d=\\\"M %s %s L %s %s S %s %s %s %s \"":                                                                                                                         1,
}

func TestSVGSerializationRatchet(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	directories := []string{
		filepath.Join(repositoryRoot, "d2renderers", "d2svg"),
		filepath.Join(repositoryRoot, "d2renderers", "d2sketch"),
		filepath.Join(repositoryRoot, "d2themes"),
	}

	observed := make(map[string]int)
	var forbiddenFields []string
	forbiddenFieldNames := map[string]struct{}{
		"Attributes": {},
		"ClipPath":   {},
		"Content":    {},
		"Mask":       {},
	}
	files := token.NewFileSet()
	for _, directory := range directories {
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if path != directory && (entry.Name() == "testdata" || entry.Name() == "vendor") {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			parsed, err := parser.ParseFile(files, path, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			relative, err := filepath.Rel(repositoryRoot, path)
			if err != nil {
				return err
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.AssignStmt:
					for _, expression := range node.Lhs {
						selector, ok := expression.(*ast.SelectorExpr)
						if !ok {
							continue
						}
						if _, forbidden := forbiddenFieldNames[selector.Sel.Name]; forbidden {
							position := files.Position(selector.Pos())
							forbiddenFields = append(forbiddenFields, fmt.Sprintf("%s:%d uses raw ThemableElement.%s", relative, position.Line, selector.Sel.Name))
						}
					}
				case *ast.CallExpr:
					if selector, ok := node.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "SetMaskUrl" {
						position := files.Position(selector.Pos())
						forbiddenFields = append(forbiddenFields, fmt.Sprintf("%s:%d uses deprecated ThemableElement.SetMaskUrl", relative, position.Line))
					}
					formatIndex, ok := fmtFormatIndex(node.Fun)
					if !ok || len(node.Args) <= formatIndex {
						return true
					}
					literal, ok := node.Args[formatIndex].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						return true
					}
					formatString, err := strconv.Unquote(literal.Value)
					if err != nil || !isDynamicRawSVGFormat(formatString) {
						return true
					}
					observed[relative+":"+strconv.Quote(formatString)]++
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(forbiddenFields) > 0 {
		sort.Strings(forbiddenFields)
		t.Fatalf("raw ThemableElement escape hatch used outside compatibility boundary:\n%s", strings.Join(forbiddenFields, "\n"))
	}
	if diff := diffFormatCounts(rawSVGFormatAllowlist, observed); diff != "" {
		t.Fatalf("raw SVG formatting changed; migrate new sites through lib/svg:\n%s", diff)
	}
}

func fmtFormatIndex(function ast.Expr) (int, bool) {
	selector, ok := function.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}
	packageName, ok := selector.X.(*ast.Ident)
	if !ok || packageName.Name != "fmt" {
		return 0, false
	}
	switch selector.Sel.Name {
	case "Sprintf":
		return 0, true
	case "Fprintf":
		return 1, true
	default:
		return 0, false
	}
}

func isDynamicRawSVGFormat(formatString string) bool {
	if !strings.Contains(formatString, "%") {
		return false
	}
	if strings.Contains(formatString, "<") {
		return true
	}
	for _, quote := range []string{`="`, `='`} {
		if strings.Contains(formatString, quote) {
			return true
		}
	}
	return false
}

func diffFormatCounts(want, got map[string]int) string {
	keys := make(map[string]struct{}, len(want)+len(got))
	for key := range want {
		keys[key] = struct{}{}
	}
	for key := range got {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	var differences []string
	for _, key := range ordered {
		if want[key] != got[key] {
			differences = append(differences, fmt.Sprintf("%q: allowlisted %d, observed %d", key, want[key], got[key]))
		}
	}
	return strings.Join(differences, "\n")
}
