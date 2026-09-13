package d2graph

import (
	"math"
	"strings"
	"testing"
)

func TestGridCapacityLimits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		rows    int
		columns int
		want    int
		wantErr string
	}{
		{name: "exact_dimension_limit", rows: MaxGridDimension, columns: 1, want: MaxGridDimension},
		{name: "over_dimension_limit", rows: MaxGridDimension + 1, columns: 1, wantErr: "exceed the maximum"},
		{name: "exact_cell_limit", rows: 1_000, columns: 1_000, want: MaxGridCells},
		{name: "over_cell_limit", rows: 1_000, columns: 1_001, wantErr: "exceed the limit"},
		{name: "integer_overflow", rows: math.MaxInt, columns: math.MaxInt, wantErr: "exceed the maximum"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := GridCapacity(tc.rows, tc.columns)
			if tc.wantErr == "" {
				if err != nil || got != tc.want {
					t.Fatalf("GridCapacity(%d, %d) = %d, %v; want %d, nil", tc.rows, tc.columns, got, err, tc.want)
				}
				return
			}
			if err == nil || got != 0 || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("GridCapacity(%d, %d) = %d, %v; want 0 and error containing %q", tc.rows, tc.columns, got, err, tc.wantErr)
			}
		})
	}
}
