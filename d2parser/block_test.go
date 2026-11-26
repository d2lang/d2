package d2parser_test

import (
	"strings"
	"testing"

	"github.com/d2lang/util-go/assert"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
)

func TestBlockFences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, fence, body string }{
		{"mixed", "!#", "the old ##| spelling stays in the body"},
		{"multibyte", "§§", "before §|"},
		{"mixed-rune-widths", "§!🞧", "text 🞧|"},
		{"overlapping", "!?!", "before !?"},
		{"pipe-in-fence", "!|!", "before !|?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "x: |" + tc.fence + "txt " + tc.body + tc.fence + "|; next\n"
			ast, err := d2parser.Parse("test.d2", strings.NewReader(source), nil)
			assert.Success(t, err)
			assert.Equal(t, 2, len(ast.Nodes))
			block := ast.Nodes[0].MapKey.Value.BlockString
			assert.String(t, tc.fence, block.Quote)
			assert.String(t, "txt", block.Tag)
			assert.String(t, tc.body, block.Value)
			assert.Equal(t, strings.Index(source, "; next"), block.Range.End.Byte)
			formatted := d2format.Format(ast)
			again, err := d2parser.Parse("test.d2", strings.NewReader(formatted), nil)
			assert.Success(t, err)
			assert.String(t, tc.body, again.Nodes[0].MapKey.Value.BlockString.Value)
			assert.String(t, formatted, d2format.Format(again))
		})
	}
}

func TestUnclosedBlockDelimiters(t *testing.T) {
	t.Parallel()
	for _, tail := range []string{"##|", "!", "!#"} {
		ast, err := d2parser.Parse("test.d2", strings.NewReader("x: |!#txt body "+tail), nil)
		assert.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "terminated with !#|"))
		assert.String(t, "body "+tail, ast.Nodes[0].MapKey.Value.BlockString.Value)
	}
	ast, err := d2parser.Parse("test.d2", strings.NewReader(`""" one " and two ""`), nil)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), `terminated with """`))
	assert.String(t, `one " and two ""`, ast.Nodes[0].BlockComment.Value)
}

func TestBlockIndentation(t *testing.T) {
	t.Parallel()
	const input = "\u2003λ\n\tsecond\n\u2003\u3000third\n\u2003\n\u2003last"
	const want = "λ\n second\n\u3000third\n\nlast"
	source := "x: |txt\n" + input + "\n|\n\"\"\"\n" + input + "\n\"\"\"next\n"
	for range 2 {
		ast, err := d2parser.Parse("test.d2", strings.NewReader(source), nil)
		assert.Success(t, err)
		assert.Equal(t, 3, len(ast.Nodes))
		assert.String(t, want, ast.Nodes[0].MapKey.Value.BlockString.Value)
		assert.String(t, want, ast.Nodes[1].BlockComment.Value)
		assert.String(t, "next", ast.Nodes[2].MapKey.Key.Path[0].ScalarString())
		source = d2format.Format(ast)
	}
}

func TestFormatWhitespaceOnlyBlocks(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"  \n\t", "\n\n", "\u2003\n\n \t"} {
		source := "outer: {\n  x: |txt\n" + body + "\n  |\n  \"\"\"\n" + body + "\n  \"\"\"\n}\n"
		for range 2 {
			ast, err := d2parser.Parse("test.d2", strings.NewReader(source), nil)
			assert.Success(t, err)
			nodes := ast.Nodes[0].MapKey.Value.Map.Nodes
			assert.String(t, body, nodes[0].MapKey.Value.BlockString.Value)
			assert.String(t, body, nodes[1].BlockComment.Value)
			formatted := d2format.Format(ast)
			assert.String(t, source, formatted)
			source = formatted
		}
	}
}

func TestFormatCollidingUnicodeFence(t *testing.T) {
	t.Parallel()
	block := &d2ast.BlockString{Quote: "!§", Tag: "txt", Value: "one !§| two |!§§ three"}
	formatted := d2format.Format(block)
	assert.String(t, "|!§§§txt one !§| two |!§§ three !§§§|", formatted)
	value, err := d2parser.ParseValue(formatted)
	assert.Success(t, err)
	assert.String(t, block.Value, value.(*d2ast.BlockString).Value)
	assert.String(t, formatted, d2format.Format(value))
}
