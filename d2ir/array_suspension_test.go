package d2ir_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/util-go/assert"
)

func TestArraySuspensionValues(t *testing.T) {
	t.Parallel()

	const source = "values: [suspend; ['suspend'; unsuspend]; null]\n"
	ast, err := d2parser.Parse("array.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	assert.String(t, source, d2format.Format(ast))

	encoded, err := json.Marshal(ast)
	assert.Success(t, err)
	var restored d2ast.Map
	assert.Success(t, json.Unmarshal(encoded, &restored))
	assert.String(t, source, d2format.Format(&restored))

	m, _, err := d2ir.Compile(&restored, nil)
	assert.Success(t, err)
	assert.String(t, source, d2format.Format(m.AST()))
}
