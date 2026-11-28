package d2animate

import (
	"bytes"
	"fmt"

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

func writeProgressStyles(buf *bytes.Buffer, hash string, opts d2svg.RenderOpts, links, themeColor bool) {
	line := ".d2-progress-" + hash
	fmt.Fprintf(buf, "@keyframes d2Progress-%s{from{width:0}to{width:100%%}}", hash)
	if themeColor {
		fmt.Fprintf(buf, "%s{fill:%s}", line, ProgressColor(opts.ThemeID, opts.ThemeOverrides))
		if opts.DarkThemeID != nil {
			fmt.Fprintf(buf, "@media screen and (prefers-color-scheme:dark){%s{fill:%s}}", line, ProgressColor(opts.DarkThemeID, opts.DarkThemeOverrides))
		}
	}
	if links {
		// The global clock pauses while a visitor explores a linked board.
		fmt.Fprintf(buf, ".d2-layer-link-%s:target~%s{animation-play-state:paused!important}", hash, line)
	}
	fmt.Fprintf(buf, "@media(prefers-reduced-motion:reduce){%s{visibility:hidden}}", line)
}
