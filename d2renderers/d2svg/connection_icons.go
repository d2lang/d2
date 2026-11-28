package d2svg

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

// drawConnectionIcon paints upright artwork around a center in local coordinates.
func drawConnectionIcon(w *objectWriter, c d2target.Connection, center geo.Point, fill string, theme *d2themes.Theme) {
	size := c.IconSize()
	radius := float64(size) / 2
	if c.Icon == nil {
		dot := d2themes.NewThemableElement("circle", theme)
		dot.Cx, dot.Cy, dot.R, dot.Fill = center.X, center.Y, radius, fill
		w.element(dot)
		return
	}
	attrs := []svg.Attribute{
		svg.Attr("href", c.Icon.String()),
		svg.FloatAttr("x", center.X-radius), svg.FloatAttr("y", center.Y-radius),
		svg.IntAttr("width", size), svg.IntAttr("height", size),
	}
	if c.IconBorderRadius != 0 {
		attrs = append(attrs, svg.Attr("clip-path", "inset(0 round "+svg.FormatFloat(c.IconBorderRadius)+"px)"))
	}
	fmt.Fprint(w, svg.EmptyElement("image", attrs...))
}

func drawMovingConnectionIcons(w *objectWriter, scope string, c d2target.Connection, shapes map[string]d2target.Shape, fill string, theme *d2themes.Theme) {
	if c.IconPositions != nil && len(c.IconPositions) == 0 {
		return
	}
	path := c.IconMotionPath(shapes[c.Src].RenderedStrokeWidth(), shapes[c.Dst].RenderedStrokeWidth())
	period := path.LaunchPeriod(c.IconAnimation)
	if c.IconSchedule != nil {
		if !c.IconSchedule.FixedLayer && len(c.IconSchedule.Carried) == 0 && c.IconSchedule.First >= float64(c.IconSchedule.DurationMS)/1000 {
			return
		}
		scope += "-t" + strconv.Itoa(c.IconSchedule.StartMS)
	}
	if path.Length() <= 0 || c.IconSchedule == nil && c.IconPositions == nil && (math.IsInf(period, 1) || !(period > 0)) {
		return
	}
	// Define the image only once, including when the CLI bundles it as a data URI.
	id := svg.ScopedID(scope+"-flow-", c.ID, "-icon")
	fmt.Fprint(w, "<defs>", svg.OpenElement("g", svg.Attr("id", id.String())))
	drawConnectionIcon(w, c, geo.Point{}, fill, theme)
	fmt.Fprint(w, "</g>")
	routeID := svg.ScopedID(scope+"-flow-", c.ID, "-path")
	var line, cssShape strings.Builder
	if c.IconPositions == nil {
		for i, pt := range path.Points {
			x, y := svg.FormatFloat(pt.X), svg.FormatFloat(pt.Y)
			if i == 0 {
				line.WriteByte('M')
			} else {
				line.WriteByte('L')
			}
			line.WriteString(x)
			line.WriteByte(' ')
			line.WriteString(y)
			if c.IconSchedule != nil {
				if i == 0 {
					cssShape.WriteString("from ")
				} else {
					cssShape.WriteString(",line to ")
				}
				cssShape.WriteString(x)
				cssShape.WriteString("px ")
				cssShape.WriteString(y)
				cssShape.WriteString("px")
			}
		}
		if c.IconSchedule == nil {
			fmt.Fprint(w, svg.EmptyElement("path", svg.Attr("id", routeID.String()), svg.Attr("d", line.String())))
		}
	}
	fmt.Fprint(w, "</defs>")
	href := svg.Attr("href", "#"+id.String())
	use := func(pt geo.Point) {
		fmt.Fprint(w, svg.EmptyElement("use", href, svg.FloatAttr("x", pt.X), svg.FloatAttr("y", pt.Y)))
	}

	fmt.Fprint(w, svg.OpenElement("g", svg.Attr("class", "d2-moving-icons"), svg.Attr("pointer-events", "none")))
	if s := c.IconSchedule; s != nil {
		drawScheduledIcons(w, s, href, line.String(), cssShape.String())
	} else if c.IconPositions != nil {
		for _, pt := range c.IconPositions {
			use(pt)
		}
	} else {
		drawLoopingIcons(w, path, period, href, routeID.String())
	}
	fmt.Fprint(w, "</g>", svg.OpenElement("g", svg.Attr("class", "d2-still-icon"), svg.Attr("pointer-events", "none")))
	use(path.PointAt(path.Length() / 2))
	fmt.Fprint(w, "</g>")
}

func drawLoopingIcons(w *objectWriter, path d2target.IconMotionPath, period float64, href svg.Attribute, routeID string) {
	travel := path.Length() / d2target.IconMotionSpeed
	slots := min(d2target.MaxMovingIconsPerConnection, max(1, int(math.Ceil(travel/period))))
	loop := period * float64(slots)
	active := min(1., travel/loop)
	// The clock has more precision than pixel coordinates: small rate values
	// can place a subsecond journey inside a very long repeating period.
	number := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	for i := 0; i < slots; i++ {
		clock := []svg.Attribute{svg.Attr("dur", number(loop)+"s"), svg.Attr("repeatCount", "indefinite")}
		if i > 0 {
			clock = append(clock, svg.Attr("begin", number(-float64(i)*period)+"s"))
		}
		motion := append([]svg.Attribute{}, clock...)
		if active < 1 {
			motion = append(motion, svg.Attr("calcMode", "linear"), svg.Attr("keyPoints", "0;1;1"), svg.Attr("keyTimes", "0;"+number(active)+";1"))
		}
		fmt.Fprint(w, "<g>", svg.EmptyElement("use", href), svg.OpenElement("animateMotion", motion...), svg.EmptyElement("mpath", svg.Attr("href", "#"+routeID)), "</animateMotion>")
		if active < 1 {
			fade := append(clock, svg.Attr("attributeName", "opacity"), svg.Attr("calcMode", "discrete"), svg.Attr("values", "1;0"), svg.Attr("keyTimes", "0;"+number(active)))
			fmt.Fprint(w, svg.EmptyElement("animate", fade...))
		}
		fmt.Fprint(w, "</g>")
	}
}
