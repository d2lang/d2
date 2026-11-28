package d2animate

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"image"
	stdcolor "image/color"
	"math"
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

func progressTheme(id *int64, overrides *d2target.ThemeOverrides) d2themes.Theme {
	theme := d2themescatalog.NeutralDefault
	if id != nil {
		if t := d2themescatalog.Find(*id); t.Name != "" {
			theme = t
		}
	}
	theme.ApplyOverrides(overrides)
	return theme
}

// A transparent SVG can be embedded over any page. Use the current theme's
// canvas as its reference, or white if the canvas itself has no opaque paint.
func progressCanvas(theme d2themes.Theme) stdcolor.RGBA {
	c, ok := color.ParseRGBA(theme.Colors.Neutrals.N7)
	if !ok || c.A != 255 {
		return stdcolor.RGBA{255, 255, 255, 255}
	}
	return stdcolor.RGBA(c)
}

func hexColor(c stdcolor.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func progressInk(theme d2themes.Theme, fill string, bg stdcolor.RGBA) string {
	if c, ok := color.ParseRGBA(d2themes.ResolveThemeColor(theme, fill)); ok {
		mix := func(c, b uint8, a uint32) uint8 {
			return uint8((uint32(c)*a + uint32(b)*(255-a) + 127) / 255)
		}
		a := uint32(c.A)
		bg.R, bg.G, bg.B = mix(c.R, bg.R, a), mix(c.G, bg.G, a), mix(c.B, bg.B, a)
	}
	return hexColor(color.InverseContrast(bg))
}

// ProgressColor returns the contrast-checked inverse of the theme's canvas.
func ProgressColor(themeID *int64, overrides *d2target.ThemeOverrides) string {
	return hexColor(color.InverseContrast(progressCanvas(progressTheme(themeID, overrides))))
}

// ResolveProgressColor selects explicit paint or the theme canvas's automatic ink.
func ResolveProgressColor(themeID *int64, overrides *d2target.ThemeOverrides, explicit string) (string, error) {
	if explicit == "" {
		return ProgressColor(themeID, overrides), nil
	}
	if err := d2target.ValidateAnimateProgressColor(explicit); err != nil {
		return "", err
	}
	return explicit, nil
}

// Step and tween frames already hold a solid footer until their next boundary.
// Animate just its fill on the existing progress clock, coalescing equal holds.
func stepInk(css *bytes.Buffer, totalMS int, frames []Frame, light d2themes.Theme, dark *d2themes.Theme, links []layerLink) string {
	track := func(theme d2themes.Theme) []byte {
		var stops bytes.Buffer
		start, previous, lastFill := 0, "", ""
		canvas := progressCanvas(theme)
		for i, f := range frames {
			at := start
			start += f.DurationMS
			fill := f.Diagram.Root.Fill
			if i > 0 && fill == lastFill {
				continue
			}
			lastFill = fill
			ink := progressInk(theme, footerFill(fill), canvas)
			if ink != previous {
				fmt.Fprintf(&stops, "%s{fill:%s}", percentage(100*float64(at)/float64(totalMS), 9), ink)
				previous = ink
			}
		}
		return stops.Bytes()
	}
	day := track(light)
	var night []byte
	if dark != nil {
		night = track(*dark)
	}
	// Render options can differ across inline exports of the same source.
	h := fnv.New32a()
	h.Write(day)
	h.Write(night)
	name := fmt.Sprintf("d2Ink-%x", h.Sum32())
	fmt.Fprintf(css, "@keyframes %s{%s}", name, day)
	if len(night) > 0 && !bytes.Equal(day, night) {
		fmt.Fprintf(css, "@media screen and (prefers-color-scheme:dark){@keyframes %s{%s}}", name, night)
	}
	for _, link := range links {
		// A link with no camera jumps directly to its board. The global
		// progress width stays paused while that board supplies the ink.
		fill := footerFill(frames[link.to].Diagram.Root.Fill)
		ink := progressInk(light, fill, progressCanvas(light))
		selector := "#" + link.id + ":target~." + name
		fmt.Fprintf(css, "%s{fill:%s!important}", selector, ink)
		if dark != nil {
			other := progressInk(*dark, fill, progressCanvas(*dark))
			if ink != other {
				fmt.Fprintf(css, "@media screen and (prefers-color-scheme:dark){%s{fill:%s!important}}", selector, other)
			}
		}
	}
	return name
}

// svgProgress adds CSS to the shared clock and returns the overlay that paints
// last in the tour viewport, outside the moving camera.
func svgProgress(buf *bytes.Buffer, hash string, footer image.Rectangle, totalMS int, light, dark, track string, links, filtered bool) string {
	line := ".d2-progress-" + hash
	// Transform fixed geometry so static board holds do not need per-tick SVG layout.
	fmt.Fprintf(buf, "@keyframes d2Progress-%s{from{transform:scaleX(0)}to{transform:scaleX(1)}}%s{transform-box:fill-box;transform-origin:left}", hash, line)
	paint := "fill:" + light + ";"
	if dark != "" && light != dark {
		// Keep both colors on the element. Multiple inline renders of the same
		// diagram share a hash and may each use different theme overrides.
		paint = "--d2-progress-light:" + light + ";--d2-progress-dark:" + dark + ";"
		fmt.Fprintf(buf, "%s{fill:var(--d2-progress-light)}@media screen and (prefers-color-scheme:dark){%s{fill:var(--d2-progress-dark)}}", line, line)
	}
	if links {
		// The global clock pauses while a visitor explores a linked board.
		sibling := ""
		if filtered {
			sibling = "g>"
		}
		fmt.Fprintf(buf, ".d2-layer-link-%s:target~%s%s{animation-play-state:paused!important}", hash, sibling, line)
	}
	fmt.Fprintf(buf, "@media(prefers-reduced-motion:reduce){%s{visibility:hidden}}", line)
	var class, clock string
	if track != "" {
		clock = fmt.Sprintf(",%s %dms steps(1,end) infinite", track, totalMS)
		if links {
			class = " " + track
		}
	}
	return fmt.Sprintf(`<rect class="d2-progress-%s%s" x="0" y="%d" width="%d" height="%d" transform="scale(0 1)" pointer-events="none" aria-hidden="true" style="%sanimation:d2Progress-%s %dms linear infinite%s"/>`,
		hash, class, footer.Min.Y+ProgressGap, footer.Dx(), footer.Dy()-ProgressGap, paint, hash, totalMS, clock)
}

// Gradients have no single footer color; the gap uses the theme canvas.
func footerFill(fill string) string {
	if fill == "" || fill == "none" || strings.EqualFold(fill, "transparent") {
		return ""
	}
	if !color.IsThemeColor(fill) {
		if _, ok := color.ParseRGBA(fill); !ok {
			return d2target.BG_COLOR
		}
	}
	return fill
}

func svgFooter(css *bytes.Buffer, hash string, footer image.Rectangle, totalMS int, opts d2svg.RenderOpts, root *d2target.Diagram, frames []Frame, links []layerLink, slots []layerSlot, camera bool) (string, bool, error) {
	explicit := opts.AnimateProgressColor
	if err := d2target.ValidateAnimateProgressColor(explicit); err != nil {
		return "", false, err
	}
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
	fill = footerFill(fill)
	filtered := !uniform && explicit == "" && camera
	light := progressTheme(opts.ThemeID, opts.ThemeOverrides)
	var single *d2themes.Theme
	var dark d2themes.Theme
	if opts.DarkThemeID == nil {
		single = &light
	} else {
		dark = progressTheme(opts.DarkThemeID, opts.DarkThemeOverrides)
	}
	lightInk, darkInk, clock := explicit, "", ""
	if filtered {
		// The width marker is the filter's alpha mask; only its geometry is used.
		lightInk = "white"
	} else if explicit == "" {
		lightInk = progressInk(light, fill, progressCanvas(light))
		var night *d2themes.Theme
		if single == nil {
			darkInk = progressInk(dark, fill, progressCanvas(dark))
			night = &dark
		}
		if !uniform {
			clock = stepInk(css, totalMS, frames, light, night, links)
		}
	}
	line := svgProgress(css, hash, footer, totalMS, lightInk, darkInk, clock, len(links) > 0, filtered)
	matte := d2themes.NewThemableElement("rect", single)
	matte.X, matte.Y = 0, float64(footer.Min.Y)
	matte.Width, matte.Height = float64(footer.Dx()), float64(footer.Dy())
	paint := func(fill string) string {
		if fill == "" {
			return ""
		}
		matte.Fill = fill
		return matte.Render()
	}
	var body bytes.Buffer
	linkedMatte := !uniform && len(links) > 0
	if uniform {
		body.WriteString(paint(fill))
	} else {
		if linkedMatte {
			fmt.Fprintf(css, ".d2-footer-%s g{transform:none!important;pointer-events:none!important}", hash)
		}
		for i, frame := range frames {
			if linkedMatte {
				if rect := paint(footerFill(frame.Diagram.Root.Fill)); rect != "" {
					writeLayerFrame(&body, hash, i, totalMS, slots, false, true)
					body.WriteString(rect)
					body.WriteString("</g></g></g>")
				}
			} else {
				matte.ClassName = fmt.Sprintf("d2-matte-%d", i)
				matte.Style = fmt.Sprintf("transform:none!important;animation:d2Transition-%s-%d %dms steps(1,end) infinite", hash, i, totalMS)
				body.WriteString(paint(footerFill(frame.Diagram.Root.Fill)))
			}
		}
	}
	group := `<g class="` + hash + ` d2-footer-` + hash
	matteGroup := group + `" aria-hidden="true" pointer-events="none">` + body.String() + "</g>"
	if filtered {
		canvas := hexColor(progressCanvas(light))
		night := canvas
		if single == nil {
			night = hexColor(progressCanvas(dark))
		}
		// Include the filter's user-space geometry and reference colors so two
		// inline exports of the same diagram can choose independent settings.
		id := fmt.Sprintf("d2-ink-%s-%d-%d-%d-%s%s", hash, footer.Dx(), footer.Min.Y, footer.Dy(), canvas[1:], night[1:])
		if night != canvas {
			fmt.Fprintf(css, "@media screen and (prefers-color-scheme:dark){#%s>feFlood{flood-color:%s}}", id, night)
		}
		filter := fmt.Sprintf(progressFilter, id, footer.Min.Y, footer.Dx(), footer.Dy(), ProgressGap, canvas, footer.Min.Y+ProgressGap, footer.Dy()-ProgressGap)
		// Keep the ordinary matte outside the filter. WebKit can cull a
		// zero-width marker's filtered group at a navigation boundary.
		fmt.Fprintf(css, ".d2-ink-source-%s rect:not([aria-hidden]){height:%dpx}", hash, ProgressGap)
		return matteGroup + filter + group + ` d2-ink-source-` + hash + `" aria-hidden="true" pointer-events="none" filter="url(#` + id + `)">` + body.String() + line + "</g>", linkedMatte, nil
	}
	if body.Len() == 0 {
		return line, false, nil
	}
	return matteGroup + line, linkedMatte, nil
}

// Use the composed gap as the input to the ink calculation. Work is restricted
// to the footer; its SourceAlpha in the last few rows holds the line's width.
// Blend the matte with the reference canvas in sRGB before computing its ink.
const progressFilter = `<defs><filter id="%[1]s" filterUnits="userSpaceOnUse" color-interpolation-filters="sRGB" x="0" y="%[2]d" width="%[3]d" height="%[4]d">` +
	`<feOffset in="SourceGraphic" dy="%[5]d" result="matte"/><feFlood flood-color="%[6]s"/><feComposite in="matte" operator="over" result="bg"/>` +
	`<feComponentTransfer in="bg" result="inv"><feFuncR type="linear" slope="-1" intercept="1"/><feFuncG type="linear" slope="-1" intercept="1"/><feFuncB type="linear" slope="-1" intercept="1"/></feComponentTransfer>` +
	// Use a 3.1:1 guard on the composited strip: browsers can quantize
	// linear-RGB channels to 8 bits during a fade. This margin protects
	// 3:1 output contrast. L-3.1I or I-3.1L must be >= .105. Scaling by
	// 255 and thresholding alpha keeps the decision binary.
	`<feColorMatrix in="bg" type="luminanceToAlpha" color-interpolation-filters="linearRGB" result="L"/>` +
	`<feColorMatrix in="inv" type="luminanceToAlpha" color-interpolation-filters="linearRGB" result="I"/>` +
	`<feComposite in="L" in2="I" operator="arithmetic" k2="255" k3="-790.5" k4="-26.775" result="a"/>` +
	`<feComposite in="I" in2="L" operator="arithmetic" k2="255" k3="-790.5" k4="-26.775"/><feComposite in2="a" operator="over"/>` +
	`<feComponentTransfer><feFuncA type="discrete" tableValues="0 1"/></feComponentTransfer><feComposite in="inv" operator="in" result="good"/>` +
	// When the inverse fails, use white below sqrt(.05*1.05)-.05 (~.17913)
	// and black above it. The discrete transfer thresholds alpha at .5.
	`<feComponentTransfer in="L"><feFuncA type="linear" slope="-1" intercept=".67913"/></feComponentTransfer>` +
	`<feComponentTransfer><feFuncA type="discrete" tableValues="0 1"/></feComponentTransfer>` +
	`<feColorMatrix values="0 0 0 1 0 0 0 0 1 0 0 0 0 1 0 0 0 0 0 1"/><feComposite in="good" operator="over"/>` +
	`<feComposite in2="SourceAlpha" operator="in" y="%[7]d" height="%[8]d"/></filter></defs>`
