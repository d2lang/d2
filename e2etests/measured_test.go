package e2etests

import (
	_ "embed"
	"testing"

	"github.com/d2lang/d2/d2target"
)

// measuredCases supplies pre-measured text to the renderer.
func measuredCases(t *testing.T) []testCase {
	return []testCase{
		{
			name:   "empty-shape",
			mtexts: []*d2target.MText{},
			script: `a: ""
`,
		},
		{
			name:   "empty-class",
			mtexts: []*d2target.MText{},
			script: `a: "" { shape: class }
`,
		},
		{
			name:   "empty-sql_table",
			mtexts: []*d2target.MText{},
			script: `a: "" { shape: sql_table }
`,
		},
	}
}
