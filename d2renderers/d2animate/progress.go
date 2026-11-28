package d2animate

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
)

// ProgressColor resolves the opaque B1 accent used by animation overlays.
// A transparent diagram accent leaves the catalog's default line visible.
func ProgressColor(themeID *int64, overrides *d2target.ThemeOverrides) string {
	if overrides != nil && overrides.B1 != nil && !strings.EqualFold(*overrides.B1, "transparent") {
		return *overrides.B1
	}
	if themeID == nil {
		return d2themescatalog.NeutralDefault.Colors.B1
	}
	return d2themescatalog.Find(*themeID).Colors.B1
}

func writeProgressStyles(buf *bytes.Buffer, hash string, opts d2svg.RenderOpts, links bool) {
	line := ".d2-progress-" + hash
	fmt.Fprintf(buf, "@keyframes d2Progress-%s{from{width:0}to{width:100%%}}%s{fill:%s}", hash, line, ProgressColor(opts.ThemeID, opts.ThemeOverrides))
	if opts.DarkThemeID != nil {
		fmt.Fprintf(buf, "@media screen and (prefers-color-scheme:dark){%s{fill:%s}}", line, ProgressColor(opts.DarkThemeID, opts.DarkThemeOverrides))
	}
	if links {
		// The global clock pauses while a visitor explores a linked board.
		fmt.Fprintf(buf, ".d2-layer-link-%s:target~%s{animation-play-state:paused!important}", hash, line)
	}
	fmt.Fprintf(buf, "@media(prefers-reduced-motion:reduce){%s{visibility:hidden}}", line)
}
