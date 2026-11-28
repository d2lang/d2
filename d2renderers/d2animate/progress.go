package d2animate

import (
	"bytes"
	"fmt"
	"image"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
)

// ProgressHeight is the line height in SVG user units or raster output pixels.
const ProgressHeight = 6

// ProgressColor resolves the opaque B1 color used by animation overlays.
// When the override supplies no solid paint, use the original catalog color.
func ProgressColor(themeID *int64, overrides *d2target.ThemeOverrides) string {
	if overrides != nil && overrides.B1 != nil && *overrides.B1 != "" && d2target.ValidateAnimateProgressColor(*overrides.B1) == nil {
		return *overrides.B1
	}
	if themeID != nil {
		if theme := d2themescatalog.Find(*themeID); theme.Colors.B1 != "" {
			return theme.Colors.B1
		}
	}
	return d2themescatalog.NeutralDefault.Colors.B1
}

// ResolveProgressColor selects the explicit line color or the theme’s B1 color.
func ResolveProgressColor(themeID *int64, overrides *d2target.ThemeOverrides, explicit string) (string, error) {
	if explicit == "" {
		return ProgressColor(themeID, overrides), nil
	}
	if err := d2target.ValidateAnimateProgressColor(explicit); err != nil {
		return "", err
	}
	return explicit, nil
}

// svgProgress adds CSS to the shared clock and returns the overlay that paints
// last in the tour viewport, outside the moving camera.
func svgProgress(buf *bytes.Buffer, hash string, viewport image.Rectangle, totalMS int, opts d2svg.RenderOpts, links bool) (string, error) {
	ink, err := ResolveProgressColor(opts.ThemeID, opts.ThemeOverrides, opts.AnimateProgressColor)
	if err != nil {
		return "", err
	}
	line := ".d2-progress-" + hash
	// Transform fixed geometry so static board holds do not need per-tick SVG layout.
	fmt.Fprintf(buf, "@keyframes d2Progress-%s{from{transform:scaleX(0)}to{transform:scaleX(1)}}%s{transform-box:fill-box;transform-origin:left}", hash, line)
	paint := "fill:" + ink + ";"
	if opts.AnimateProgressColor == "" && opts.DarkThemeID != nil {
		// Keep both colors on the element. Multiple inline renders of the same
		// diagram share a hash and may each use different theme overrides.
		dark := ProgressColor(opts.DarkThemeID, opts.DarkThemeOverrides)
		paint = "--d2-progress-light:" + ink + ";--d2-progress-dark:" + dark + ";"
		fmt.Fprintf(buf, "%s{fill:var(--d2-progress-light)}@media screen and (prefers-color-scheme:dark){%s{fill:var(--d2-progress-dark)}}", line, line)
	}
	if links {
		// The global clock pauses while a visitor explores a linked board.
		fmt.Fprintf(buf, ".d2-layer-link-%s:target~%s{animation-play-state:paused!important}", hash, line)
	}
	fmt.Fprintf(buf, "@media(prefers-reduced-motion:reduce){%s{visibility:hidden;animation:none!important}}", line)
	h := min(ProgressHeight, viewport.Dy())
	return fmt.Sprintf(`<rect class="d2-progress-%s" x="%d" y="%d" width="%d" height="%d" transform="scale(0 1)" pointer-events="none" aria-hidden="true" style="%sanimation:d2Progress-%s %dms linear infinite"/>`,
		hash, viewport.Min.X, viewport.Max.Y-h, viewport.Dx(), h, paint, hash, totalMS), nil
}
