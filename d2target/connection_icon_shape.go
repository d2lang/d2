package d2target

import "github.com/d2lang/d2/lib/geo"

// IconPolygon returns the upright vertices of a built-in polygon at center.
// Circles and image icons have no polygon.
func (c *Connection) IconPolygon(center geo.Point) []geo.Point {
	if c.Icon != nil {
		return nil
	}
	const r = connectionIconShapeSize / 2.
	left, right := center.X-r, center.X+r
	top, bottom := center.Y-r, center.Y+r
	switch c.IconShape {
	case ShapeSquare:
		return []geo.Point{{X: left, Y: top}, {X: right, Y: top}, {X: right, Y: bottom}, {X: left, Y: bottom}}
	case ConnectionIconTriangle:
		return []geo.Point{{X: center.X, Y: top}, {X: right, Y: bottom}, {X: left, Y: bottom}}
	case ShapeDiamond:
		return []geo.Point{{X: center.X, Y: top}, {X: right, Y: center.Y}, {X: center.X, Y: bottom}, {X: left, Y: center.Y}}
	}
	return nil
}
