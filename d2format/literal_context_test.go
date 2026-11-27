package d2format_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2oracle"
	"github.com/d2lang/d2/d2parser"
)

func TestLiteralDollarBeforeStyleMap(t *testing.T) {
	for _, tc := range []struct{ source, primary, label string }{
		{"costs $", `costs \$`, "costs $"},
		{`costs \$`, `costs \$`, "costs $"},
		{`two \\$`, `two \\\$`, `two \$`},
		{`$${v} costs $`, `$${v} costs \$`, "$5 costs $"},
		{`"costs $"`, `"costs $"`, "costs $"},
	} {
		key, err := d2parser.ParseMapKey("x: " + tc.source)
		if err != nil {
			t.Fatal(err)
		}
		key.Primary = key.Value.ScalarBox()
		key.Value = d2ast.MakeValueBox(&d2ast.Map{Range: d2ast.MakeRange(",1:0:0-1:0:0")})
		if got, want := d2format.Format(key), "x: "+tc.primary+" {}"; got != want {
			t.Errorf("formatting %q as a primary: got %q, want %q", tc.source, got, want)
		}
		g, _, err := d2compiler.Compile("test.d2", strings.NewReader("vars: {v: 5}\nx: "+tc.source), nil)
		if err != nil {
			t.Fatal(err)
		}
		fill := "blue"
		g, err = d2oracle.Set(g, nil, "x.style.fill", nil, &fill)
		if err != nil {
			t.Fatal(err)
		}
		if x := d2oracle.GetObj(g, nil, "x"); x == nil || x.Label.Value != tc.label || x.Style.Fill.Value != fill {
			t.Errorf("lost the original label or new fill for %q", tc.source)
		}
	}
}

func TestMoveParsedValueIntoArray(t *testing.T) {
	for _, tc := range []struct{ source, element string }{
		{"#AbC", `\#AbC`},
		{`already\] and bare]`, `already\] and bare\]`},
		{`backslash\\]`, `backslash\\\]`},
		{`before]${v}after]`, `before\]${v}after\]`},
		{`@file.field]name`, `@file.field\]name`},
		{`...$5`, `...$5`},
		{"costs $", "costs $"},
	} {
		v, err := d2parser.ParseValue(tc.source)
		if err != nil {
			t.Fatal(err)
		}
		array := &d2ast.Array{Nodes: []d2ast.ArrayNodeBox{
			d2ast.MakeArrayNodeBox(v.(d2ast.ArrayNode)),
			d2ast.MakeArrayNodeBox(d2ast.FlatUnquotedString("next")),
		}}
		got := d2format.Format(array)
		if want := "[" + tc.element + "; next]"; got != want {
			t.Errorf("moved %q into array: got %q, want %q", tc.source, got, want)
		}
		again, err := d2parser.ParseValue(got)
		if err != nil {
			t.Fatal(err)
		}
		if a := again.(*d2ast.Array); len(a.Nodes) != 2 || d2format.Format(a) != got {
			t.Fatalf("unstable elements after formatting %q", got)
		}
	}
}

func TestHexColorMovedIntoKey(t *testing.T) {
	color, err := d2parser.ParseValue("#abc")
	if err != nil {
		t.Fatal(err)
	}
	key := &d2ast.KeyPath{Path: []*d2ast.StringBox{d2ast.MakeValueBox(color).StringBox()}}
	formatted := d2format.Format(&d2ast.Key{Key: key})
	m, err := d2parser.Parse("test.d2", strings.NewReader(formatted), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Nodes) != 1 || m.Nodes[0].MapKey == nil || m.Nodes[0].MapKey.Key.StringIDA()[0] != "#abc" {
		t.Fatalf("expected a shape named #abc after formatting %q", formatted)
	}
}

func TestFormatFoldedTokens(t *testing.T) {
	for _, tc := range []struct{ prefix, suffix, want string }{
		{"API-", ">Worker", `API\->Worker`},
		{"API-", "-Worker -> db", `API\--Worker -> db`},
		{"API-", "*.style.fill: blue", `API\-*.style.fill: blue`},
		{"!", "&flag", `\!&flag`},
		{"box.", "(port", `box.\(port`},
		{"status: ", `"On call"`, `status: \"On call"`},
		{"status: ", `'${literal}'`, `status: \'${literal}'`},
		{"status: ", "@file", `status: \@file`},
		{"status: ...", "@file", `status: \...@file`},
		{"status: [", "[open]", "status: [\n  \\[open\n]"},
	} {
		s := tc.prefix + "\\\n  " + tc.suffix
		for range 2 {
			m, err := d2parser.Parse("test.d2", strings.NewReader(s), nil)
			if err != nil {
				t.Fatal(err)
			}
			s = d2format.Format(m)
			if strings.TrimSpace(s) != tc.want {
				t.Errorf("formatting %q + %q: got %q, want %q", tc.prefix, tc.suffix, s, tc.want)
				break
			}
		}
	}
}
