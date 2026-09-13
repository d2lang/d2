package d2ir

import (
	"errors"
	"path"
	"path/filepath"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2parser"
)

// ErrImportsDisabled is returned when D2 source requests an import but the
// caller did not provide CompileOptions.FS.
var ErrImportsDisabled = errors.New("d2ir: imports are disabled; provide CompileOptions.FS to enable them")

// MaxImportDepth is the maximum number of imported files permitted in one
// active import chain. The root source file is not counted.
const MaxImportDepth = 128

func (c *compiler) pushImportStack(imp *d2ast.Import) (string, bool) {
	impPath := imp.PathWithPre()
	if impPath == "" && imp.Range != (d2ast.Range{}) {
		c.errorf(imp, "imports must specify a path to import")
		return "", false
	}
	if len(c.importStack) > 0 {
		if path.Ext(impPath) != ".d2" {
			impPath += ".d2"
		}

		if !filepath.IsAbs(impPath) {
			impPath = path.Join(path.Dir(c.importStack[len(c.importStack)-1]), impPath)
		}
	}

	if len(c.importStack) > 0 && len(c.importStack)-1 >= MaxImportDepth {
		c.errorf(imp, "maximum import depth of %d exceeded", MaxImportDepth)
		return "", false
	}

	for i, p := range c.importStack {
		if impPath == p {
			c.errorf(imp, "detected cyclic import chain: %s", formatCyclicChain(c.importStack[i:]))
			return "", false
		}
	}

	c.importStack = append(c.importStack, impPath)
	return impPath, true
}

func (c *compiler) popImportStack() {
	c.importStack = c.importStack[:len(c.importStack)-1]
}

func formatCyclicChain(cyclicChain []string) string {
	var b strings.Builder
	for _, p := range cyclicChain {
		b.WriteString(p)
		b.WriteString(" -> ")
	}
	b.WriteString(cyclicChain[0])
	return b.String()
}

// Returns either *Map or *Field.
func (c *compiler) _import(imp *d2ast.Import, importer Node) (Node, bool) {
	_, fieldImport := importer.(*Field)
	return c.__import(imp, importTemplateSafeAt(importer), fieldImport)
}

func (c *compiler) selectImport(ir *Map, imp *d2ast.Import) (Node, bool) {
	nilScopeMap(ir)
	if ida := imp.IDA(); len(ida) > 0 {
		f := ir.getFieldIndexed(ida...)
		if f == nil {
			c.errorf(imp, "import key %q doesn't exist inside import", ida)
			return nil, false
		}
		return f, true
	}
	return ir, true
}

func (c *compiler) __import(imp *d2ast.Import, templateSafe, fieldImport bool) (Node, bool) {
	if c.stopped() {
		return nil, false
	}
	impPath, ok := c.pushImportStack(imp)
	if !ok {
		return nil, false
	}
	defer c.popImportStack()

	// Only get immediate imports.
	if len(c.importStack) == 2 {
		if _, ok := c.seenImports[impPath]; !ok {
			c.imports = append(c.imports, imp.PathWithPre())
		}
	}

	cacheable := templateSafe && len(c.globContexts()) == 0 && len(c.mapRefContextStack) == 0
	if cacheable {
		if template := c.importTemplates[impPath]; template != nil {
			c.seenImports[impPath] = struct{}{}
			if ida := imp.IDA(); fieldImport && !imp.Spread && len(ida) > 0 && template.canSelect() {
				selected := template.ir.getFieldIndexed(ida...)
				if selected == nil {
					c.errorf(imp, "import key %q doesn't exist inside import", ida)
					return nil, false
				}
				if !c.reserveVariableCopy(imp, selected) {
					return nil, false
				}
				field := cloneImportField(selected)
				nilScopeMap(field)
				return field, true
			}
			if !c.reserveVariableCopy(imp, template.ir) {
				return nil, false
			}
			return c.selectImport(cloneImportMap(template.ir), imp)
		}
	}

	errCount := len(c.err.Errors)
	ast, openErr, ok := c.loadImportAST(impPath, imp, c.err)
	if !ok {
		if openErr != nil {
			c.errorf(imp, "failed to import %q: %v", impPath, openErr)
		}
		return nil, false
	}

	ir := &Map{}
	ir.initRoot()
	ir.parent.(*Field).References[0].Context_.Scope = ast

	c.compileMap(ir, ast, ast)
	if c.stopped() {
		return nil, false
	}

	// We attempt to resolve variables in the imported file scope first
	// But ignore errors, in case the variable is meant to be resolved at the
	// importer
	savedErrors := make([]d2ast.Error, len(c.err.Errors))
	copy(savedErrors, c.err.Errors)
	c.compileSubstitutions(ir, nil)
	c.err.Errors = savedErrors
	if c.stopped() {
		return nil, false
	}

	c.seenImports[impPath] = struct{}{}
	if cacheable && len(c.err.Errors) == errCount {
		if !c.reserveVariableCopy(imp, ir) {
			return nil, false
		}
		c.importTemplates[impPath] = &importTemplate{ir: cloneImportMap(ir)}
	}

	return c.selectImport(ir, imp)
}

// Imported IR templates are context independent for ordinary maps. Board
// imports are different: scenario and step inheritance is established while
// the import is attached to its destination board. Keep the parsed-AST cache
// there, but compile a fresh IR instance so board order and inheritance retain
// their source semantics.
func importTemplateSafeAt(n Node) bool {
	for n != nil {
		if kind := NodeBoardKind(n); kind != "" {
			root := false
			switch n := n.(type) {
			case *Field:
				root = n.Root()
			case *Map:
				root = n.Root()
			}
			if !root {
				return false
			}
		}
		n = n.Parent()
	}
	return true
}

func (c *compiler) peekImport(imp *d2ast.Import) (*Map, bool) {
	impPath := imp.PathWithPre()
	if impPath == "" && imp.Range != (d2ast.Range{}) {
		return nil, false
	}

	if len(c.importStack) > 0 {
		if path.Ext(impPath) != ".d2" {
			impPath += ".d2"
		}

		if !filepath.IsAbs(impPath) {
			impPath = path.Join(path.Dir(c.importStack[len(c.importStack)-1]), impPath)
		}
	}

	// Use a separate parse error to avoid polluting the main one
	localErr := &d2parser.ParseError{}
	ast, _, ok := c.loadImportAST(impPath, imp, localErr)
	if !ok {
		return nil, false
	}

	ir := &Map{}
	ir.initRoot()
	ir.parent.(*Field).References[0].Context_.Scope = ast

	c.compileMap(ir, ast, ast)
	if c.stopped() {
		return nil, false
	}

	return ir, true
}

func (c *compiler) loadImportAST(impPath string, source d2ast.Node, parseErr *d2parser.ParseError) (*d2ast.Map, error, bool) {
	if ast := c.parsedImports[impPath]; ast != nil {
		if !c.reserveVariableASTCopy(source, ast) {
			return nil, nil, false
		}
		cloned, err := cloneASTMapContext(c.ctx, ast)
		if err != nil {
			c.handleVariableExpansionWalkError(err)
			return nil, nil, false
		}
		return cloned, nil, true
	}

	if c.fs == nil {
		return nil, ErrImportsDisabled, false
	}
	f, err := c.fs.Open(impPath)
	if err != nil {
		return nil, err, false
	}
	defer f.Close()

	ast, err := d2parser.ParseContext(c.ctx, impPath, f, &d2parser.ParseOptions{
		UTF16Pos:   c.utf16Pos,
		ParseError: parseErr,
	})
	if err != nil {
		return nil, nil, false
	}
	c.parsedImports[impPath] = ast
	if !c.reserveVariableASTCopy(source, ast) {
		return nil, nil, false
	}
	cloned, err := cloneASTMapContext(c.ctx, ast)
	if err != nil {
		c.handleVariableExpansionWalkError(err)
		return nil, nil, false
	}
	return cloned, nil, true
}

func nilScopeMap(n Node) {
	switch n := n.(type) {
	case *Map:
		for _, f := range n.Fields {
			nilScopeMap(f)
		}
		for _, e := range n.Edges {
			nilScopeMap(e)
		}
	case *Edge:
		for _, r := range n.References {
			r.Context_.ScopeMap = nil
		}
		if n.Map() != nil {
			nilScopeMap(n.Map())
		}
	case *Field:
		for _, r := range n.References {
			r.Context_.ScopeMap = nil
		}
		if n.Map() != nil {
			nilScopeMap(n.Map())
		}
	}
}
