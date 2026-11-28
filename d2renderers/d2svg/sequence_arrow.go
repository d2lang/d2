package d2svg

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

// The white stroke uncovers the authored shaft, including its original dash
// pattern and rough strokes. The browser measures the same path for its tip.
func openSequenceShaft(buf *bytes.Buffer, c d2target.Connection, path, id string, reverse bool) {
	initial := "1"
	if reverse {
		initial = "-1"
	}
	// Limit the mask surface to this route, including the rough stroke.
	width := c.RenderedStrokeWidth() + 10
	tl, br := geo.Route(c.Route).GetBoundingBox()
	buf.WriteString(svg.OpenElement("mask", svg.Attr("id", id), svg.Attr("maskUnits", "userSpaceOnUse"),
		svg.FloatAttr("x", tl.X-width), svg.FloatAttr("y", tl.Y-width),
		svg.FloatAttr("width", br.X-tl.X+2*width), svg.FloatAttr("height", br.Y-tl.Y+2*width)))
	buf.WriteString(svg.EmptyElement("path", svg.Attr("d", path), svg.Attr("fill", "none"), svg.Attr("stroke", "white"),
		svg.FloatAttr("stroke-width", width), svg.Attr("stroke-linejoin", "round"), svg.Attr("pathLength", "1"),
		svg.Attr("stroke-dasharray", "1 1"), svg.IntAttr("data-d2-sequence-trace", c.SequenceStep), svg.Attr("style", "--d2-trace-start:"+initial)))
	buf.WriteString("</mask>")
	buf.WriteString(svg.OpenElement("g", svg.IntAttr("data-d2-sequence-shaft", c.SequenceStep), svg.Attr("style", "--d2-trace-mask:"+svg.LocalIRI(svg.LiteralID(id)))))
}

func sequenceHead(buf *bytes.Buffer, step int, id, path, paint string, reverse bool, worldToLocal d2scene.Matrix, masks []svg.Attribute) {
	// Keep the native marker and sketch geometry at their authored positions
	// for static media and the final hold. The traveling instance reuses them.
	buf.WriteString(svg.OpenElement("g", append([]svg.Attribute{svg.IntAttr("data-d2-sequence-arrived", step)}, masks...)...))
	buf.WriteString(svg.OpenElement("g", svg.Attr("id", id)))
	buf.WriteString(paint)
	buf.WriteString("</g></g>")
	from, to := "0%", "100%"
	if reverse {
		from, to = to, from
	}
	buf.WriteString(svg.OpenElement("g", svg.IntAttr("data-d2-sequence-flight", step), svg.Attr("display", "none"),
		svg.Attr("style", fmt.Sprintf("--d2-route:path('%s');--d2-from:%s;--d2-to:%s", path, from, to))))
	transform := fmt.Sprintf("matrix(%s %s %s %s %s %s)", svg.FormatFloat(worldToLocal.A), svg.FormatFloat(worldToLocal.B), svg.FormatFloat(worldToLocal.C), svg.FormatFloat(worldToLocal.D), svg.FormatFloat(worldToLocal.E), svg.FormatFloat(worldToLocal.F))
	buf.WriteString(svg.EmptyElement("use", append(svg.HrefAttributes("#"+id), svg.Attr("transform", transform))...))
	buf.WriteString("</g>")
}

func writeSequenceArrowCSS(buf *bytes.Buffer, hash string, stepNumber, start, duration int) {
	step := strconv.Itoa(stepNumber)
	prefix := hash + "-trace-" + step
	at := sequencePercent(start, duration)
	arrival := sequencePercent(start+d2target.SequenceMessageRevealMS, duration)
	rule := func(selector, suffix, timing string) {
		fmt.Fprintf(buf, ".%s [%s=%q]{animation:%s-%s %dms %s infinite}", hash, selector, step, prefix, suffix, duration, timing)
	}
	hold := "0%"
	if start != 0 {
		hold += "," + at + "%"
	}
	fmt.Fprintf(buf, ".%s [data-d2-sequence-message][data-d2-sequence-step=%q]{animation:%s-show %dms linear infinite}", hash, step, prefix, duration)
	fmt.Fprintf(buf, "@keyframes %s-show{%s{visibility:hidden}%s%%,100%%{visibility:visible}}", prefix, hold, arrival)
	rule("data-d2-sequence-trace", "stroke", "linear")
	fmt.Fprintf(buf, "@keyframes %s-stroke{%s{stroke-dashoffset:var(--d2-trace-start)}%s%%,100%%{stroke-dashoffset:0}}", prefix, hold, arrival)
	rule("data-d2-sequence-shaft", "mask", "steps(1,end)")
	fmt.Fprintf(buf, "@keyframes %s-mask{0%%{mask:var(--d2-trace-mask)}%s%%,100%%{mask:none}}", prefix, arrival)
	fmt.Fprintf(buf, ".%s [data-d2-sequence-flight=%q]{display:inline;offset-path:var(--d2-route);offset-anchor:0px 0px;offset-rotate:auto;animation:%s-move %dms linear infinite,%s-hide %dms steps(1,end) infinite}", hash, step, prefix, duration, prefix, duration)
	fmt.Fprintf(buf, "@keyframes %s-move{%s{offset-distance:var(--d2-from)}%s%%,100%%{offset-distance:var(--d2-to)}}", prefix, hold, arrival)
	fmt.Fprintf(buf, "@keyframes %s-hide{0%%{visibility:inherit}%s%%,100%%{visibility:hidden}}", prefix, arrival)
}
