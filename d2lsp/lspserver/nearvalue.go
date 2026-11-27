package lspserver

import (
	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2ir"
)

type nearValue struct {
	scalar   d2ast.Scalar
	resolved string
	computed bool
	source   d2ast.Range
}

// The parser attaches the raw spelling to literal string fragments. The IR
// compiler leaves standalone substitutions in the key AST and coalesces mixed
// interpolations into a new string without a source spelling. Both forms can
// resolve to a shape, but a shape rename cannot rewrite their value as a literal.
func nearValueAt(f *d2ir.Field, ref *d2ir.FieldReference) (nearValue, bool) {
	key := ref.Context().Key
	scalar := key.Primary.Unbox()
	if scalar == nil {
		scalar = key.Value.ScalarBox().Unbox()
	}
	if scalar == nil {
		return nearValue{}, false
	}
	var parts []d2ast.InterpolationBox
	switch value := scalar.(type) {
	case *d2ast.UnquotedString:
		parts = value.Value
	case *d2ast.DoubleQuotedString:
		parts = value.Value
	}
	computed := len(parts) > 0 && (len(parts) != 1 || parts[0].StringRaw == nil)
	if !computed {
		return nearValue{scalar: scalar, resolved: scalar.ScalarString(), source: scalar.GetRange()}, true
	}
	if f.Primary() == nil || f.LastPrimaryRef() != ref {
		return nearValue{}, false
	}
	source := scalar.GetRange()
	// A string wrapper can end before the substitution it contains. For a
	// scalar assignment the enclosing key records the end of the expression.
	if key.Value.ScalarBox().Unbox() == scalar {
		source.End = key.Range.End
	}
	return nearValue{scalar: scalar, resolved: f.Primary().Value.ScalarString(), computed: true, source: source}, true
}
