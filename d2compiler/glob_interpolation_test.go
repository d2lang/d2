package d2compiler_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

func TestInterpolatedGlobPatterns(t *testing.T) {
	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{`${stem}*`, []string{"prefix", "exact"}},
		{`*${stem}`, []string{"suffix", "exact"}},
		{`*${stem}*`, []string{"prefix", "infix", "suffix", "exact", "literal", "multiline"}},
		{`${pattern}`, []string{"prefix", "infix", "suffix", "exact", "literal", "multiline"}},
		{`\*${stem}*`, []string{"literal"}},
		{`${escaped}*`, []string{"literal"}},
		{`"${stem}*"`, nil},
		{`K*`, []string{"kelvin"}},
		{`${kelvin}*`, []string{"kelvin"}},
		{`${star}*`, []string{"literal"}},
		{`*${star}`, []string{"endingStar"}},
		{`*${empty}*api`, []string{"suffix", "exact"}},
		{`pre\n${stem}*`, []string{"multiline"}},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			source := `vars: {stem: api; pattern: *${stem}*; escaped: '*api'; kelvin: K; star: '*'; empty: ''}
prefix: api-server
infix: the API server
suffix: router-api
exact: api
literal: '*api-handler'
kelvin: k
endingStar: 'data*'
multiline: "pre\napi stream"
unrelated: database
*: {&label: ` + tc.filter + `; tooltip: selected}`
			g, _, err := d2compiler.Compile("patterns.d2", strings.NewReader(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, obj := range g.Objects {
				if obj.Tooltip != nil {
					got = append(got, obj.AbsID())
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("selected %v, want %v", got, tc.want)
			}
		})
	}
}
