package d2target

import "github.com/d2lang/d2/lib/geo"

// The centered square reserved for the triangle and diamond includes their tips.
const connectionTallIconSize = 16

// IconPolygon returns the upright vertices of a built-in polygon with its
// filled centroid at center.
// Circles and image icons have no polygon.
func (c *Connection) IconPolygon(center geo.Point) []geo.Point {
	if c.Icon != nil {
		return nil
	}
	x, y := center.X, center.Y
	const tip = connectionTallIconSize / 2
	switch c.IconShape {
	case ShapeSquare:
		const r = connectionIconShapeSize / 2
		return []geo.Point{{X: x - r, Y: y - r}, {X: x + r, Y: y - r}, {X: x + r, Y: y + r}, {X: x - r, Y: y + r}}
	case ConnectionIconTriangle:
		// A triangle's centroid sits one third of its height above its base.
		return []geo.Point{{X: x, Y: y - tip}, {X: x + 7, Y: y + tip/2}, {X: x - 7, Y: y + tip/2}}
	case ShapeDiamond:
		return []geo.Point{{X: x, Y: y - tip}, {X: x + 6, Y: y}, {X: x, Y: y + tip}, {X: x - 6, Y: y}}
	}
	return nil
}
