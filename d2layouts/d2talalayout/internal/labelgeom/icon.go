package labelgeom

import (
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// IconSize uses the exported diagram's integer icon size for a proposed box.
func IconSize(box *geo.Box, position string) float64 {
	return float64(d2target.GetIconSize(box, position))
}
