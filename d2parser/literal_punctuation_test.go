package d2parser_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
)

func TestLiteralPunctuationInKeys(t *testing.T) {
	for _, name := range []string{"R&D", "[canary]", "queue[1]", "]", "<worker>", "load > 90%", "trailing <", "count < (limit)", "rank < @limit", "$5"} {
		key, err := d2parser.ParseMapKey(name + ": ready")
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if key.Key == nil || len(key.Key.Path) != 1 || key.Key.StringIDA()[0] != name || key.Ampersand || len(key.Edges) != 0 {
			t.Errorf("expected one ordinary shape named %q, got %+v", name, key)
		}
	}

	const source = "svc.&ready -> <sink>[1]; (svc.&ready -> <sink>[1])[0].label: ack [OK]"
	m, err := d2parser.Parse("test.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range m.Nodes {
		edge := n.MapKey.Edges[0]
		if edge.Src.StringIDA()[1] != "&ready" || edge.Dst.StringIDA()[0] != "<sink>[1]" {
			t.Fatalf("lost punctuation in connection endpoints: %+v", edge)
		}
	}
	k := m.Nodes[1].MapKey
	if k.EdgeIndex == nil || *k.EdgeIndex.Int != 0 || k.Value.UnquotedString.ScalarString() != "ack [OK]" {
		t.Fatalf("expected indexed edge and bracketed label, got %+v", k)
	}
	for source, name := range map[string]string{`<\#tag`: "<#tag", `a<\#b`: "a<#b"} {
		k, err := d2parser.ParseMapKey(source)
		if err != nil || k.Key == nil || k.Key.StringIDA()[0] != name {
			t.Errorf("%q: expected a key named %q, got %+v, %v", source, name, k, err)
		}
	}
	k, err = d2parser.ParseMapKey("(a -> b<)[0].label: literal")
	if err != nil || len(k.Edges) != 1 || k.Edges[0].Dst.StringIDA()[0] != "b<" || k.EdgeIndex == nil || *k.EdgeIndex.Int != 0 {
		t.Errorf("expected indexed connection to the endpoint b<, got %+v, %v", k, err)
	}
	for _, source := range []string{"a < b", "a <- b", "a <-> b", "a <= b"} {
		k, err := d2parser.ParseMapKey(source)
		if err != nil || len(k.Edges) != 1 || k.Edges[0].SrcArrow != "<" {
			t.Errorf("%q: expected a left arrow, got %+v, %v", source, k, err)
		}
	}
}

func TestLiteralDollar(t *testing.T) {
	for _, text := range []string{"$20", "$", "cash$", "$HOME", "$$", "in $ and £"} {
		for _, source := range []string{text, `"` + text + `"`} {
			v, err := d2parser.ParseValue(source)
			if err != nil {
				t.Fatalf("%q: %v", source, err)
			}
			if v.(d2ast.String).ScalarString() != text || d2format.Format(v) != source {
				t.Errorf("lost literal text or spelling in %q", source)
			}
		}
	}
	for _, source := range []string{`$$ {amount} USD`, `"$$ {amount} USD"`} {
		v, err := d2parser.ParseValue(source)
		if err != nil {
			t.Fatal(err)
		}
		var parts []d2ast.InterpolationBox
		switch s := v.(type) {
		case *d2ast.DoubleQuotedString:
			parts = s.Value
		case *d2ast.UnquotedString:
			parts = s.Value
		}
		if len(parts) != 3 || *parts[0].String != "$" || parts[1].Substitution.IDA()[0] != "amount" || *parts[2].String != " USD" {
			t.Errorf("%q: expected a literal dollar, variable, and currency", source)
		}
	}
}

func TestPunctuationInArrayElements(t *testing.T) {
	v, err := d2parser.ParseValue(`[...$10; before[; {arr[0]: value]}; [$5]; ...${vars}; @file]`)
	if err != nil {
		t.Fatal(err)
	}
	nodes := v.(*d2ast.Array).Nodes
	if len(nodes) != 6 || nodes[0].UnquotedString.ScalarString() != "...$10" ||
		nodes[1].UnquotedString.ScalarString() != "before[" ||
		nodes[3].Array.Nodes[0].UnquotedString.ScalarString() != "$5" ||
		!nodes[4].Substitution.Spread || nodes[5].Import.Path[0].ScalarString() != "file" {
		t.Fatalf("lost array element or delimiter: %+v", nodes)
	}
	k := nodes[2].Map.Nodes[0].MapKey
	if k.Key.StringIDA()[0] != "arr[0]" || k.Value.UnquotedString.ScalarString() != "value]" {
		t.Fatal("the enclosing array consumed a bracket in a nested map")
	}
}
