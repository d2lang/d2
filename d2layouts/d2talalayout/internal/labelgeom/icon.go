package labelgeom

import (
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// IconSize uses the exported diagram's integer icon size for a proposed box.
func IconSize(box *geo.Box, position string) float64 {
	return float64(d2target.GetIconSize(box, position))
}

// MovingIconGeometry carries the painted footprint and centerline style into
// caption placement before routes and label positions are finalized.
type MovingIconGeometry struct {
	Footprint    d2target.MovingIconFootprint `json:"footprint"`
	StrokeWidth  float64                      `json:"strokeWidth"`
	BorderRadius float64                      `json:"borderRadius"`
}

func (icon MovingIconGeometry) LabelTopLeft(route geo.Route, curve bool, pos label.Position, fraction, width, height float64, charge func(int64) bool) *geo.Point {
	c := &d2target.Connection{Route: route, IsCurve: curve, BorderRadius: icon.BorderRadius, StrokeWidthOffset: icon.StrokeWidth}
	return icon.Footprint.LabelTopLeft(c, pos, fraction, width, height, charge)
}
