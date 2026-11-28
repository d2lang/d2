package d2svg

import (
	"fmt"
	"math"
	"strconv"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
)

// Reuse one instance for successive trips on a repeating clock. Even a long
// board hold needs only the copies that can fit along the route at one time.
func drawScheduledIcons(w *objectWriter, s *d2target.ConnectionIconSchedule, href svg.Attribute, routeID string) {
	num := func(x float64) string { return strconv.FormatFloat(x, 'g', -1, 64) }
	global := fmt.Sprintf("--d2-icon-film-ms:%dms;--d2-icon-start:%s;--d2-icon-film:%s;--d2-icon-hold:%s;--d2-icon-route:url(#%s);--d2-icon-transit:%s", s.FilmMS, num(float64(s.StartMS)/1000), num(float64(s.FilmMS)/1000), num(float64(s.DurationMS)/1000), routeID, num(s.Transit))
	fmt.Fprint(w, svg.OpenElement("g", svg.Attr("class", "d2-icon-tour-clock"), svg.Attr("style", global)), `<g class="d2-icon-layer-clock"><g class="d2-icon-trips">`)
	track := func(delay, initial, cycle float64) {
		if !s.FixedLayer && delay >= float64(s.DurationMS)/1000 {
			return
		}
		// One-time trips retain their previous progress. Periodic trips first
		// appear at delay, then reuse this instance every cycle seconds.
		style := "--d2-icon-origin:" + num(delay-initial*s.Transit)
		if s.FixedLayer {
			// A pinned board runs beyond its normal hold. Each icon gets its own
			// repeating or one-shot clock starting when the destination opens.
			if math.IsInf(cycle, 1) {
				style += ";--d2-icon-offset:" + num(initial*s.Transit) + ";--d2-icon-end:" + num(s.Transit) +
					";--d2-icon-free-duration:" + num((1-initial)*s.Transit) + "s;--d2-icon-fill:both" +
					";--d2-icon-alive:max(0,sign(1 - var(--d2-icon-free-phase)))"
			} else {
				style += ";--d2-icon-end:" + num(cycle) + ";--d2-icon-free-duration:" + num(cycle) +
					"s;--d2-icon-delay:" + num(delay) + "s;--d2-icon-count:infinite"
			}
		}
		if !math.IsInf(cycle, 1) {
			style += ";--d2-icon-phase:mod(var(--d2-icon-age)," + num(cycle) + ")"
		}
		fmt.Fprint(w, svg.OpenElement("g", svg.Attr("style", style)), svg.EmptyElement("use", href), "</g>")
	}
	for _, p := range s.Carried {
		track(0, p, math.Inf(1))
	}
	if !math.IsInf(s.Period, 1) {
		slots := min(d2target.MaxMovingIconsPerConnection, max(1, int(math.Ceil(s.Transit/s.Period))))
		for i := 0; i < slots; i++ {
			track(s.First+float64(i)*s.Period, 0, float64(slots)*s.Period)
		}
	}
	fmt.Fprint(w, "</g></g></g>")
}
