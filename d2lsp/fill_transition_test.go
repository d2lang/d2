package d2lsp

import (
	"strings"
	"testing"
)

func TestFillTransitionValueContext(t *testing.T) {
	for _, tc := range []struct {
		source   string
		property bool
	}{
		{"a.style.fill-transition: |", true},
		{"a.style: {\n  fill-transition: |\n}", true},
		{"a: {\n  style: {\n    fill-transition: |\n  }\n}", true},
		{"fill-transition: |", false},
		{"a.fill-transition: |", false},
		{"registry: {\n  shape: sql_table\n  fill-transition: |\n}", false},
	} {
		src, rest, _ := strings.Cut(tc.source, "|")
		lines := strings.Split(src, "\n")
		items, err := GetCompletionItems(src+rest, len(lines)-1, len(lines[len(lines)-1]))
		if err != nil {
			t.Fatal(err)
		}
		wipe := false
		for _, it := range items {
			if it.Label == "wipe-right" {
				wipe = true
				break
			}
		}
		if wipe != tc.property {
			t.Errorf("wipe completion for %q = %v, want %v", tc.source, wipe, tc.property)
		}
	}
}
