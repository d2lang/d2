package e2etests

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/util-go/assert"
)

// Both compilations must start with the fixture's options, before Compile
// fills in defaults and applies configuration from the source.
func checkFormattedDiagram(t *testing.T, ctx context.Context, source string, original *d2target.Diagram, compileOpts d2lib.CompileOptions, renderOpts d2svg.RenderOpts) {
	t.Helper()
	t.Run("format", func(t *testing.T) {
		ast, err := d2lib.Parse(ctx, source, &compileOpts)
		assert.Success(t, err)
		formatted := d2format.Format(ast)
		if formatted == source {
			return
		}
		got, _, err := d2lib.Compile(ctx, formatted, &compileOpts, &renderOpts)
		assert.Success(t, err)
		assert.JSON(t, original, got)
	})
}
