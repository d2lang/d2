package d2talalayout

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestNoEvaluationOrderHazards scans TALA's source for statements that read a
// slice header (the operand of an index, slice, len, cap or append, or the
// base of an indexed assignment) and, in the same statement, call a method on
// the same receiver variable, or a local closure, that assigns that field or
// variable. Go leaves the order of the read and the call unspecified: gc runs
// the call first, TinyGo (d2's wasm build) reads the slice first, so
// m.entries[m.entryFor(c)] indexed a stale slice under TinyGo and set.near[i] = append(set.near[i], intern(n)) lost pairs. The scan is syntactic and one call deep; the
// fixes put the call in its own statement.
//
// TALA_EVAL_ORDER_DIR scans another tree (to check the scan finds the
// original hazards).
func TestNoEvaluationOrderHazards(t *testing.T) {
	root := "."
	if dir := os.Getenv("TALA_EVAL_ORDER_DIR"); dir != "" {
		root = dir
	}
	fset := token.NewFileSet()
	var files []*ast.File
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == "testdata" || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 100 {
		t.Fatalf("scanned only %d files under %s", len(files), root)
	}
	hazards := evalOrderHazards(fset, files)
	sort.Strings(hazards)
	for _, hazard := range hazards {
		t.Error(hazard)
	}
}

// evalOrderHazards returns one line per hazard found.
func evalOrderHazards(fset *token.FileSet, files []*ast.File) []string {
	// Fields each method assigns through its receiver, by method name.
	methodWrites := map[string]map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
				continue
			}
			recv := fd.Recv.List[0].Names[0].Name
			for target := range assignedTargets(fd.Body) {
				if field, ok := strings.CutPrefix(target, recv+"."); ok && !strings.Contains(field, ".") {
					if methodWrites[fd.Name.Name] == nil {
						methodWrites[fd.Name.Name] = map[string]bool{}
					}
					methodWrites[fd.Name.Name][field] = true
				}
			}
		}
	}
	var out []string
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			var body *ast.BlockStmt
			switch fn := n.(type) {
			case *ast.FuncDecl:
				body = fn.Body
			case *ast.FuncLit:
				body = fn.Body
			}
			if body == nil {
				return true
			}
			// Local closures and what they assign.
			closures := map[string]map[string]bool{}
			ast.Inspect(body, func(m ast.Node) bool {
				assign, ok := m.(*ast.AssignStmt)
				if !ok || len(assign.Lhs) != len(assign.Rhs) {
					return true
				}
				for i, lhs := range assign.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok {
						continue
					}
					if lit, ok := assign.Rhs[i].(*ast.FuncLit); ok {
						closures[id.Name] = assignedTargets(lit.Body)
					}
				}
				return true
			})
			ast.Inspect(body, func(m ast.Node) bool {
				switch stmt := m.(type) {
				case *ast.FuncLit:
					return m == n
				case *ast.AssignStmt, *ast.ExprStmt, *ast.ReturnStmt, *ast.DeclStmt, *ast.IncDecStmt, *ast.SendStmt:
					out = append(out, statementHazards(fset, stmt, methodWrites, closures)...)
				case *ast.IfStmt:
					out = append(out, statementHazards(fset, stmt.Cond, methodWrites, closures)...)
				case *ast.ForStmt:
					if stmt.Cond != nil {
						out = append(out, statementHazards(fset, stmt.Cond, methodWrites, closures)...)
					}
				case *ast.SwitchStmt:
					if stmt.Tag != nil {
						out = append(out, statementHazards(fset, stmt.Tag, methodWrites, closures)...)
					}
				case *ast.RangeStmt:
					out = append(out, statementHazards(fset, stmt.X, methodWrites, closures)...)
				}
				return true
			})
			return false
		})
	}
	return out
}

// assignedTargets lists the variables and selectors (as source text) a body
// assigns, closures inside it included.
func assignedTargets(body ast.Node) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			if stmt.Tok == token.DEFINE {
				return true
			}
			for _, lhs := range stmt.Lhs {
				if text := exprText(lhs); text != "" {
					out[text] = true
				}
			}
		case *ast.IncDecStmt:
			if text := exprText(stmt.X); text != "" {
				out[text] = true
			}
		}
		return true
	})
	return out
}

// exprText is the source text of an identifier or a chain of selectors.
func exprText(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if base := exprText(x.X); base != "" {
			return base + "." + x.Sel.Name
		}
	case *ast.ParenExpr:
		return exprText(x.X)
	}
	return ""
}

func statementHazards(fset *token.FileSet, stmt ast.Node, methodWrites map[string]map[string]bool, closures map[string]map[string]bool) []string {
	type read struct {
		node ast.Expr
		text string
	}
	var reads []read
	var calls []*ast.CallExpr
	addRead := func(e ast.Expr) {
		if text := exprText(e); text != "" {
			reads = append(reads, read{e, text})
		}
	}
	ast.Inspect(stmt, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.IndexExpr:
			addRead(x.X)
		case *ast.SliceExpr:
			addRead(x.X)
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "len" || id.Name == "cap" || id.Name == "append") {
				if len(x.Args) > 0 {
					addRead(x.Args[0])
				}
				return true
			}
			calls = append(calls, x)
		}
		return true
	})
	var out []string
	for _, call := range calls {
		var writes map[string]bool
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			recv := exprText(fun.X)
			if recv == "" {
				continue
			}
			for field := range methodWrites[fun.Sel.Name] {
				if writes == nil {
					writes = map[string]bool{}
				}
				writes[recv+"."+field] = true
			}
		case *ast.Ident:
			writes = closures[fun.Name]
		}
		for _, r := range reads {
			if !writes[r.text] {
				continue
			}
			inside := func(inner, outer ast.Node) bool {
				return inner.Pos() >= outer.Pos() && inner.End() <= outer.End()
			}
			if inside(r.node, call) || inside(call, r.node) {
				continue
			}
			out = append(out, fset.Position(r.node.Pos()).String()+": "+r.text+" is read in the same statement as "+exprText(call.Fun)+"(...), which assigns it")
		}
	}
	return out
}
