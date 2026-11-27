package d2format_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/util-go/assert"
)

func TestFormatMarkdown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "isolated fences",
			in: "# Diagram  \n\n`d2` inline\n\n```d2 title=diagram\n" +
				"a  ->b;c\n```` \t\n\n```d2example\nx  -> y\n```\n\n" +
				"````text\n```d2\nx  -> y\n```\n````\n\n" +
				"<pre>\n```d2\nx  -> y\n```\n</pre>\n\n" +
				"    ```d2\n    x  -> y\n    ```\n\n~~~ d2\tx=y\nx  ->y\n~~~\n\nAfter",
			want: "# Diagram  \n\n`d2` inline\n\n```d2 title=diagram\n" +
				"a -> b\nc\n```` \t\n\n```d2example\nx  -> y\n```\n\n" +
				"````text\n```d2\nx  -> y\n```\n````\n\n" +
				"<pre>\n```d2\nx  -> y\n```\n</pre>\n\n" +
				"    ```d2\n    x  -> y\n    ```\n\n~~~ d2\tx=y\nx -> y\n~~~\n\nAfter",
		},
		{
			name: "nested containers",
			in: "> 1. Quote above  \n>    - ```d2\n>      a: {\n>             b\n>      }; c\n" +
				">      ```\n\nOutside\n\n  ~~~d2\np: {\n q\n}\n  ~~~",
			want: "> 1. Quote above  \n>    - ```d2\n>      a: {\n>        b\n>      }\n>      c\n" +
				">      ```\n\nOutside\n\n  ~~~d2\n  p: {\n    q\n  }\n  ~~~",
		},
		{
			name: "CRLF and comments",
			in:   "Before  \r\n\r\n```d2\r\na  -> b # note\r\nx: {\r\ny\r\n}\r\n```\r\nAfter\n",
			want: "Before  \r\n\r\n```d2\r\na -> b # note\r\nx: {\r\n  y\r\n}\r\n```\r\nAfter\n",
		},
		{
			name: "CR-only input",
			in:   "Before\r\r```d2\ra  -> b; c\r```\rAfter",
			want: "Before\r\r```d2\ra -> b\rc\r```\rAfter",
		},
		{
			name: "unclosed fence and no final newline",
			in:   "Before\n\n```d2\na  -> b;c",
			want: "Before\n\n```d2\na -> b\nc",
		},
		{
			name: "unclosed fence ended by block quote",
			in:   "> ```d2\n> a  -> b;c\n\nOutside\n",
			want: "> ```d2\n> a -> b\n> c\n\nOutside\n",
		},
		{
			name: "empty and canonical fence",
			in:   "> ```d2\r\n> ```  \r\n\r\n> ```d2\n>a -> b\n> ```\n",
			want: "> ```d2\r\n> ```  \r\n\r\n> ```d2\n>a -> b\n> ```\n",
		},
		{
			name: "compact block quote and adjacent fences",
			in:   "> ```d2\n>a  ->b; c\n> ```\n> ~~~d2\n>d  ->e\n> ~~~",
			want: "> ```d2\n>a -> b\n>c\n> ```\n> ~~~d2\n>d -> e\n> ~~~",
		},
		{
			name: "blank quote and list lines",
			in:   "> ```d2\n> a  ->b\n>\n> c\n> ```\n\n- ```d2\n  a  ->b\n\n  c\n  ```\n",
			want: "> ```d2\n> a -> b\n>\n> c\n> ```\n\n- ```d2\n  a -> b\n\n  c\n  ```\n",
		},
		{
			name: "tab after list marker",
			in:   "-\t```d2\n\tblock: {\n\tchild\n\t}\n\t```\n",
			want: "-\t```d2\n\tblock: {\n\t  child\n\t}\n\t```\n",
		},
		{
			name: "tab crossing nested Markdown indentation",
			in:   "> -\t```d2\n>   \ta  -> b;c\n>     ```\n",
			want: "> -\t```d2\n>     a -> b\n>     c\n>     ```\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := d2format.FormatMarkdown(context.Background(), "README.md", []byte(tc.in))
			assert.Success(t, err)
			assert.String(t, tc.want, string(got))
			again, err := d2format.FormatMarkdown(context.Background(), "README.md", got)
			assert.Success(t, err)
			assert.String(t, tc.want, string(again))
		})
	}
}

func TestFormatMarkdownErrorLocation(t *testing.T) {
	t.Parallel()
	const source = "Opening\r\n\r\n```d2\r\na  -> b\r\n```\r\n\r\n> ```d2\r\n> name:\r\n> ```\r\n"
	got, err := d2format.FormatMarkdown(context.Background(), "docs/guide.md", []byte(source))
	if got != nil {
		t.Fatalf("got partial document %q", got)
	}
	var parsed *d2parser.ParseError
	if !errors.As(err, &parsed) || len(parsed.Errors) != 1 {
		t.Fatalf("expected one D2 parse error, got %v", err)
	}
	problem := parsed.Errors[0]
	assert.String(t, "docs/guide.md:8:7: missing value after colon", problem.Message)
	assert.Equal(t, strings.Index(source, "name:")+len("name"), problem.Range.Start.Byte)
}

func TestFormatMarkdownFenceCollision(t *testing.T) {
	t.Parallel()
	const source = "```d2\nx: |md\n    ```\n    block contents\n|\n```\n"
	got, err := d2format.FormatMarkdown(context.Background(), "docs/guide.md", []byte(source))
	if got != nil || err == nil || !strings.Contains(err.Error(), "docs/guide.md:1:1: formatting would change the Markdown fence; try a longer enclosing fence") {
		t.Fatalf("expected a fence collision at the opening marker, got %q, %v", got, err)
	}
}

func TestFormatMarkdownCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := d2format.FormatMarkdown(ctx, "file.md", []byte("```d2\na  ->b\n```\n"))
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %q, %v", got, err)
	}
}
