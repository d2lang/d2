package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

func TestWholeKeyInNearAndBoardLink(t *testing.T) {
	for _, tc := range []struct{ source, diagnostic string }{
		{"a; b.near: a -> missing", "bad near key"},
		{`layers: {overview: {y}}; b.link: "layers.overview; other"`, "invalid board link"},
		{`a -> b: {link: "root.layers.overview; other"}`, "invalid board link"},
		{`b.link: "layers.overview; second/path"`, "invalid board link"},
		{`b.link: "layers.overview: wrong"`, "invalid board link"},
		{`vars: {dest: "layers.overview; other"}; b.link: ${dest}`, "invalid board link"},
	} {
		_, _, err := d2compiler.Compile("test.d2", strings.NewReader(tc.source), nil)
		if err == nil || !strings.Contains(err.Error(), tc.diagnostic) || !strings.Contains(err.Error(), "after key") {
			t.Errorf("%q: expected %q, got %v", tc.source, tc.diagnostic, err)
		}
	}

	const source = `layers: {overview: {y}; "release;preview": {z}}
a
b.near: a
overview.link: layers.overview
quoted.link: 'layers."release;preview"'
missing.link: layers.absent
https.link: "https://example.org/a?q=1"
mail.link: "mailto:person@example.org"
scheme.link: "layers.example:open"
rooted.link: "/docs/guide.html#intro"
relative.link: "layers.html#intro"`
	g, _, err := d2compiler.Compile("test.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	if near := g.Root.Children["b"].NearKey; near == nil || near.StringIDA()[0] != "a" {
		t.Errorf("lost valid near target: %+v", near)
	}
	for id, want := range map[string]string{
		"overview": "root.layers.overview",
		"quoted":   `root.layers."release;preview"`,
		"https":    "https://example.org/a?q=1",
		"mail":     "mailto:person@example.org",
		"scheme":   "layers.example:open",
		"rooted":   "/docs/guide.html#intro",
		"relative": "layers.html#intro",
	} {
		if link := g.Root.Children[id].Link; link == nil || link.Value != want {
			t.Errorf("%s: wanted %q, got %+v", id, want, link)
		}
	}
	if g.Root.Children["missing"].Link != nil {
		t.Error("unresolved board link was retained")
	}
}
