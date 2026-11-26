package xsort_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TinyGo instantiates the generic sorts of package slices once per element
// type and does not share them between pointer types, so in the D2.js module
// every direct call can add another copy of pdqsort. D2's packages that D2.js
// links sort through lib/xsort instead, which TinyGo builds with one shared
// implementation (sort.Slice, sort.SliceStable and sort.Strings already share
// one). This test fails on a direct call in any of those packages.
func TestD2JSSortsThroughXsort(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-json=ImportPath,Dir,GoFiles",
		"-tags=tinygo,scheduler.none,goroutines.inert", "github.com/d2lang/d2/d2js")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	generic := map[string]bool{"Sort": true, "SortFunc": true, "SortStableFunc": true}
	var found []string
	checked := 0
	fset := token.NewFileSet()
	for dec := json.NewDecoder(bytes.NewReader(out)); dec.More(); {
		var pkg struct {
			ImportPath string
			Dir        string
			GoFiles    []string
		}
		if err := dec.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(pkg.ImportPath, "github.com/d2lang/d2/") || pkg.ImportPath == "github.com/d2lang/d2/lib/xsort" {
			continue
		}
		checked++
		for _, name := range pkg.GoFiles {
			path := filepath.Join(pkg.Dir, name)
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			local := ""
			for _, imp := range f.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); p == "slices" {
					local = "slices"
					if imp.Name != nil {
						local = imp.Name.Name
					}
				}
			}
			if local == "" {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fun := call.Fun
				if index, ok := fun.(*ast.IndexExpr); ok { // slices.SortFunc[T](...)
					fun = index.X
				}
				if sel, ok := fun.(*ast.SelectorExpr); ok && generic[sel.Sel.Name] {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == local {
						found = append(found, fset.Position(call.Pos()).String()+": slices."+sel.Sel.Name)
					}
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("go list found none of D2's packages; the check would pass vacuously")
	}
	if len(found) > 0 {
		t.Errorf("D2.js links these direct generic sorts; use lib/xsort instead (xsort.Func, xsort.StableFunc, xsort.Ordered),"+
			"or the module gains a copy of pdqsort per element type:\n  %s", strings.Join(found, "\n  "))
	}
}
