package svg

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ScaledDimensions returns the outer SVG's explicit size. An absent scale
// leaves sizing to the containing document.
func ScaledDimensions(width, height int, scale *float64) (string, error) {
	if scale == nil {
		return "", nil
	}
	w, h := math.Ceil(*scale*float64(width)), math.Ceil(*scale*float64(height))
	limit := -float64(math.MinInt)
	if !(*scale > 0 && w >= 0 && w < limit && h >= 0 && h < limit) {
		return "", fmt.Errorf("SVG scale %g produces invalid dimensions", *scale)
	}
	return fmt.Sprintf(` width="%d" height="%d"`, int(w), int(h)), nil
}

// FormatFloat serializes an SVG coordinate with the same six decimal places
// as %f, but omits redundant trailing zeros. Keeping the original rounding is
// important: formatting the full float or reducing precision changes geometry.
func FormatFloat(value float64) string {
	// Most laid-out coordinates are integers. Avoid allocating and formatting
	// a fractional part only to remove it again. The upper bound is exclusive
	// because float64(math.MaxInt64) rounds up to 2^63.
	if value >= -0x1p63 && value < 0x1p63 {
		integer := int64(value)
		if value == float64(integer) {
			if integer == 0 && math.Signbit(value) {
				return "-0"
			}
			return strconv.FormatInt(integer, 10)
		}
	}
	s := strconv.FormatFloat(value, 'f', 6, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return s
}
