package fontface

import (
	"fmt"
	"math"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/d2lang/d2/d2renderers/d2scene"
)

// GlyphPath returns the font's closed outline in scene coordinates.
func GlyphPath(face *ParsedFace, id uint32, ppem fixed.Int26_6, origin d2scene.Point) (d2scene.Path, error) {
	if id == 0 || id > math.MaxUint16 || int(id) >= face.Outline.NumGlyphs() {
		return d2scene.Path{}, fmt.Errorf("native SVG has invalid glyph ID %d", id)
	}
	var buffer sfnt.Buffer
	segments, err := face.Outline.LoadGlyph(&buffer, sfnt.GlyphIndex(id), ppem, nil)
	if err != nil {
		return d2scene.Path{}, fmt.Errorf("native SVG glyph %d outline: %w", id, err)
	}
	path := d2scene.Path{}
	point := func(p fixed.Point26_6) d2scene.Point {
		return d2scene.Point{X: origin.X + float64(p.X)/64, Y: origin.Y + float64(p.Y)/64}
	}
	open := false
	for _, s := range segments {
		a, b, c := point(s.Args[0]), point(s.Args[1]), point(s.Args[2])
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			if open {
				path.Commands = append(path.Commands, d2scene.ClosePath())
			}
			path.Commands = append(path.Commands, d2scene.MoveTo(a.X, a.Y))
			open = true
		case sfnt.SegmentOpLineTo:
			path.Commands = append(path.Commands, d2scene.LineTo(a.X, a.Y))
		case sfnt.SegmentOpQuadTo:
			path.Commands = append(path.Commands, d2scene.QuadraticTo(a.X, a.Y, b.X, b.Y))
		case sfnt.SegmentOpCubeTo:
			path.Commands = append(path.Commands, d2scene.CubicTo(a.X, a.Y, b.X, b.Y, c.X, c.Y))
		}
	}
	if open {
		path.Commands = append(path.Commands, d2scene.ClosePath())
	}
	return path, nil
}
