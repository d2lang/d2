package d2format_test

import (
	"strings"
	"testing"

	"github.com/d2lang/util-go/assert"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
)

func TestEscapeSingleQuoted(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		str  string
		exp  string
	}{
		{
			name: "simple",
			str:  `Things will be bright in P.M. Love is a snowmobile racing across the tundra, which suddenly flips.`,
			exp:  `'Things will be bright in P.M. Love is a snowmobile racing across the tundra, which suddenly flips.'`,
		},
		{
			name: "single_quotes",
			str:  `'rawr'`,
			exp:  `'''rawr'''`,
		},
		{
			name: "newlines",
			str: `


`,
			exp: `'\n\n\n'`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.String(t, tc.exp, d2format.Format(&d2ast.SingleQuotedString{
				Value: tc.str,
			}))
		})
	}
}

func TestEscapeDoubleQuoted(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		str   string
		exp   string
		inKey bool
	}{
		{
			name: "simple",
			str:  `Things will be bright in P.M. Love is a snowmobile racing across the tundra, which suddenly flips.`,
			exp:  `"Things will be bright in P.M. Love is a snowmobile racing across the tundra, which suddenly flips."`,
		},
		{
			name: "specials_1",
			str:  `"\x`,
			exp:  `"\"\\x"`,
		},
		{
			name: "specials_2",
			str:  `$$3es`,
			exp:  `"\$\$3es"`,
		},
		{
			name: "newlines",
			str: `


`,
			exp: `"\n\n\n"`,
		},
		{
			name:  "specials_key",
			str:   `$$3es`,
			exp:   `"$$3es"`,
			inKey: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var n d2ast.Node
			if tc.inKey {
				n = &d2ast.KeyPath{
					Path: []*d2ast.StringBox{
						d2ast.MakeValueBox(d2ast.FlatDoubleQuotedString(tc.str)).StringBox(),
					},
				}
			} else {
				n = d2ast.FlatDoubleQuotedString(tc.str)
			}
			assert.String(t, tc.exp, d2format.Format(n))
		})
	}
}

func TestEscapeUnquoted(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		str   string
		exp   string
		inKey bool
	}{
		{
			name: "simple",
			str:  `Change your thoughts and you change your world.`,
			exp:  `Change your thoughts and you change your world.`,
		},
		{
			name: "specials_1",
			str:  `meow;{};meow`,
			exp:  `meow\;\{\}\;meow`,
		},
		{
			name: "specials_2",
			str:  `'meow-#'`,
			exp:  `\'meow-\#'`,
		},
		{
			name: "specials_3",
			str:  `#meow|`,
			exp:  `\#meow|`,
		},
		{
			name:  "specials_key_1",
			str:   `<---->`,
			exp:   `\<\-\-\--\>`,
			inKey: true,
		},
		{
			name:  "specials_key_2",
			str:   `:::::`,
			exp:   `\:\:\:\:\:`,
			inKey: true,
		},
		{
			name:  "specials_key_3",
			str:   `&&OKAY!!  Turn on the sound ONLY for TRYNEL CARPETING, FULLY-EQUIPPED`,
			exp:   `\&&OKAY!!  Turn on the sound ONLY for TRYNEL CARPETING, FULLY-EQUIPPED`,
			inKey: true,
		},
		{
			name:  "specials_key_4",
			str:   `*-->`,
			exp:   `\*\--\>`,
			inKey: true,
		},
		{
			name: "specials_key_4_notkey",
			str:  `*-->`,
			exp:  `*-->`,
		},
		{
			name: "null",
			str:  `null`,
			exp:  `'null'`,
		},
		{
			name: "empty",
			str:  ``,
			exp:  `""`,
		},
		{
			name: "newlines",
			str: `


`,
			exp: `\n\n\n`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var n d2ast.Node
			if tc.inKey {
				n = &d2ast.KeyPath{
					Path: []*d2ast.StringBox{
						d2ast.MakeValueBox(d2ast.FlatUnquotedString(tc.str)).StringBox(),
					},
				}
			} else {
				n = d2ast.FlatUnquotedString(tc.str)
			}

			assert.String(t, tc.exp, d2format.Format(n))
		})
	}
}

func TestEscapeBlockString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		tag   string
		quote string
		value string

		exp string
	}{
		{
			name:  "oneline",
			value: `Change your thoughts and you change your world.`,
			exp:   `| Change your thoughts and you change your world. |`,
		},
		{
			name: "multiline",
			value: `Change your thoughts and you change your world.
`,
			exp: `|
  Change your thoughts and you change your world.
|`,
		},
		{
			name:  "empty",
			value: ``,
			exp:   `| |`,
		},
		{
			name:  "quote_1",
			value: `|%%% %%%|`,
			quote: "%%%",

			exp: `|%%%% |%%% %%%| %%%%|`,
		},
		{
			name:  "quote_2",
			value: `|%%% %%%%|`,
			quote: "%%%",

			exp: `|%%%%% |%%% %%%%| %%%%%|`,
		},
		{
			name:  "quote_3",
			value: `||`,
			quote: "",

			exp: `||| || |||`,
		},
		{
			name:  "tag",
			value: `This must be morning.  I never could get the hang of mornings.`,
			tag:   "html",

			exp: `|html This must be morning.  I never could get the hang of mornings. |`,
		},
		{
			name:  "bad_tag",
			value: `This must be morning.  I never could get the hang of mornings.`,
			tag:   "ok ok",

			exp: `|ok ok This must be morning.  I never could get the hang of mornings. |`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			n := &d2ast.BlockString{
				Quote: tc.quote,
				Tag:   tc.tag,
				Value: tc.value,
			}

			assert.String(t, tc.exp, d2format.Format(n))
		})
	}
}

// TODO: chaos test each

func TestParsedInterpolation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, value, label string }{
		{"unquoted-delimiters", `before${v}\#tag\;end`, `beforeX#tag;end`},
		{"unquoted-spelling", `before${v}\*literal`, `beforeX*literal`},
		{"adjacent-substitutions", `STYLE${v}${v}FILL`, `STYLEXXFILL`},
		{"quoted-escapes", `"\"${v}\"mid${v}\\tail\nend"`, "\"X\"midX\\tail\nend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := d2parser.Parse("test.d2", strings.NewReader("x: "+tc.value), nil)
			assert.Success(t, err)
			formatted := d2format.Format(ast.Nodes[0].MapKey.Value.Unbox())
			assert.String(t, tc.value, formatted)
			checkFormattedInterpolation(t, formatted, tc.label)
		})
	}
}

func TestConstructedInterpolation(t *testing.T) {
	t.Parallel()

	literal := func(s string) d2ast.InterpolationBox { return d2ast.InterpolationBox{String: &s} }
	variable := d2ast.InterpolationBox{Substitution: &d2ast.Substitution{Path: d2ast.MakeKeyPath([]string{"v"}).Path}}
	for _, tc := range []struct {
		name  string
		value d2ast.Node
		want  string
		label string
	}{
		{
			name: "unquoted",
			value: &d2ast.UnquotedString{Value: []d2ast.InterpolationBox{
				literal("@start"), variable, literal("#;"), literal("${literal}"), variable, literal("null"), literal(""),
			}},
			want: `\@start${v}\#\;\$\{literal\}${v}null`, label: `@startX#;${literal}Xnull`,
		},
		{
			name: "quoted",
			value: &d2ast.DoubleQuotedString{Value: []d2ast.InterpolationBox{
				literal(`"`), variable, literal("\n"), variable, literal(`\${literal}"`),
			}},
			want: `"\"${v}\n${v}\\\${literal}\""`, label: "\"X\nX\\${literal}\"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			formatted := d2format.Format(tc.value)
			assert.String(t, tc.want, formatted)
			checkFormattedInterpolation(t, formatted, tc.label)
		})
	}
}

func checkFormattedInterpolation(t *testing.T, value, label string) {
	t.Helper()
	source := "vars: {v: X}\nx: " + value + "\n"
	ast, err := d2parser.Parse("test.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	assert.String(t, source, d2format.Format(ast))
	g, _, err := d2compiler.Compile("test.d2", strings.NewReader(source), nil)
	assert.Success(t, err)
	assert.Equal(t, 1, len(g.Objects))
	assert.String(t, label, g.Objects[0].Label.Value)
}
