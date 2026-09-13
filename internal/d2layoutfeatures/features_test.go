package d2layoutfeatures

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
)

func TestFeatureSupport(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		dagreOK bool
		elkOK   bool
	}{
		{"ordinary edges", "a -> b", true, true},
		{"locked positions", "a: {top: 10; left: 20}", false, false},
		{"container dimensions", "a: {width: 300; b}", false, true},
		{"object near", "a\nb: {near: a}", false, false},
		{"constant near", "a: {near: top-center}", true, true},
		{"descendant edges", "a.b\na -> a.b", false, true},
		{"container loop", "a.b\na -> a", false, true},
		{"grid dimensions", "a: {grid-columns: 1; width: 300; b}", true, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph, _, err := d2compiler.Compile("test.d2", strings.NewReader(test.script), nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"dagre", "DAGRE", "elk", "ElK", "tala", "TaLa"} {
				canonicalName := strings.ToLower(name)
				wantOK := canonicalName == "tala" || (canonicalName == "dagre" && test.dagreOK) || (canonicalName == "elk" && test.elkOK)
				err := Check(name, graph)
				if (err == nil) != wantOK {
					t.Errorf("%s feature support = %v, want success %v", name, err, wantOK)
				}
				if err != nil && !strings.Contains(err.Error(), `layout engine "`+canonicalName+`"`) {
					t.Errorf("feature error does not identify the engine: %v", err)
				}
			}
		})
	}
}

func TestUnknownLayout(t *testing.T) {
	for _, name := range []string{"", "custom", "d2plugin-tala", "../tala"} {
		if err := Check(name, d2graph.NewGraph()); err == nil {
			t.Errorf("Check(%q) accepted an unknown layout", name)
		}
	}
}
