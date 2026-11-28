package d2cli

import (
	"image/color"
	"math"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	d2color "github.com/d2lang/d2/lib/color"
)

// Extend opaque solid fills into the video's unused rows and columns.
// Gradients have no single surrounding color, and translucent fills need
// the same white backing during camera moves as in the settled raster.
func videoBoardMatte(theme d2themes.Theme, fill string) color.RGBA {
	solid, ok := d2color.ParseRGBA(d2themes.ResolveThemeColor(theme, fill))
	if !ok || solid.A != 255 {
		return color.RGBA{255, 255, 255, 255}
	}
	return color.RGBA(solid)
}

// Keep the artwork backing separate from the progress footer. Gradient stops
// can be translucent, so changing the camera or letterbox matte would also
// change the artwork. The SVG footer uses the theme's background for gradients.
type rasterMattes struct {
	board, footer color.RGBA
}

func boardRasterMattes(theme d2themes.Theme, fill string) rasterMattes {
	board := videoBoardMatte(theme, fill)
	footer := board
	if d2color.IsGradient(fill) {
		footer = videoBoardMatte(theme, d2target.BG_COLOR)
	}
	return rasterMattes{board: board, footer: footer}
}

func (from rasterMattes) blend(to rasterMattes, progress float64) rasterMattes {
	board := blendVideoMatte(from.board, to.board, progress)
	footer := board
	if from.board != from.footer || to.board != to.footer {
		footer = blendVideoMatte(from.footer, to.footer, progress)
	}
	return rasterMattes{board: board, footer: footer}
}

func blendVideoMatte(from, to color.RGBA, progress float64) color.RGBA {
	mix := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a) + (float64(b)-float64(a))*progress))
	}
	return color.RGBA{mix(from.R, to.R), mix(from.G, to.G), mix(from.B, to.B), 255}
}
