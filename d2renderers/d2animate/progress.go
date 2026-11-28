package d2animate

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/color"
)

// ProgressHeight is the maximum playback progress line height in output pixels.
const ProgressHeight = 6

// ProgressGap is the clear space between the artwork canvas and the line, in output pixels.
const ProgressGap = 8

// ProgressLineHeight is 1% of the shorter canvas side, rounded to whole pixels.
func ProgressLineHeight(width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	return min(height, ProgressHeight, max(2, int(math.Round(float64(min(width, height))/100))))
}

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
func svgProgress(buf *bytes.Buffer, hash string, footer image.Rectangle, totalMS int, opts d2svg.RenderOpts, links bool) (string, error) {
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
		fmt.Fprintf(buf, "svg:has(>.d2-layer-link-%s:target)~%s{animation-play-state:paused!important}", hash, line)
	}
	fmt.Fprintf(buf, "@media(prefers-reduced-motion:reduce){%s{visibility:hidden}}", line)
	return fmt.Sprintf(`<rect class="d2-progress-%s" x="0" y="%d" width="%d" height="%d" transform="scale(0 1)" pointer-events="none" aria-hidden="true" style="%sanimation:d2Progress-%s %dms linear infinite"/>`,
		hash, footer.Min.Y+ProgressGap, footer.Dx(), footer.Dy()-ProgressGap, paint, hash, totalMS), nil
}

// Match solid board backgrounds in the footer. Gradient and patterned boards
// keep their artwork intact and use a solid base for the extra margin.
func footerMatte(css *bytes.Buffer, hash string, footer image.Rectangle, totalMS int, opts d2svg.RenderOpts, root *d2target.Diagram, frames []Frame, links []layerLink) string {
	fill := root.Root.Fill
	if len(frames) > 0 {
		fill = frames[0].Diagram.Root.Fill
	}
	uniform := true
	for _, frame := range frames {
		if frame.Diagram.Root.Fill != fill {
			uniform = false
			break
		}
	}
	var theme *d2themes.Theme
	if opts.DarkThemeID == nil {
		light := d2themescatalog.NeutralDefault
		if opts.ThemeID != nil {
			light = d2themescatalog.Find(*opts.ThemeID)
		}
		light.ApplyOverrides(opts.ThemeOverrides)
		theme = &light
	}
	matte := d2themes.NewThemableElement("rect", theme)
	matte.X, matte.Y = 0, float64(footer.Min.Y)
	matte.Width, matte.Height = float64(footer.Dx()), float64(footer.Dy())
	paint := func(fill string) string {
		if fill == "" || fill == "none" || strings.EqualFold(fill, "transparent") {
			return ""
		}
		if !color.IsThemeColor(fill) {
			if _, ok := color.ParseRGBA(fill); !ok {
				fill = d2target.BG_COLOR
			}
		}
		matte.Fill = fill
		return matte.Render()
	}
	var body strings.Builder
	if uniform {
		body.WriteString(paint(fill))
	} else {
		// Share the artwork's visibility/crossfade clocks, without moving the margin.
		for i, frame := range frames {
			matte.Style = fmt.Sprintf("transform:none!important;animation:d2Transition-%s-%d %dms steps(1,end) infinite", hash, i, totalMS)
			matte.ClassName = "d2-matte-" + strconv.Itoa(i)
			body.WriteString(paint(frame.Diagram.Root.Fill))
		}
		if len(links) != 0 {
			fmt.Fprintf(css, "svg:has(>.d2-layer-link-%[1]s:target)~.d2-footer-%[1]s>rect{animation-play-state:paused!important;opacity:0!important;clip-path:none!important}", hash)
			for _, link := range links {
				fmt.Fprintf(css, "svg:has(>#%s:target)~.d2-footer-%s>.d2-matte-%d{opacity:1!important}", link.id, hash, link.to)
			}
		}
	}
	if body.Len() == 0 {
		return ""
	}
	return `<g class="` + hash + ` d2-footer-` + hash + `" aria-hidden="true" pointer-events="none">` + body.String() + `</g>`
}
