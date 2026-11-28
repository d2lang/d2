package d2animate

import (
	"bytes"
	"fmt"
	"strconv"
)

// The authored board's CSS clock pauses while an internal fragment is selected.
// Keep icon departures on that same clock. When a layer includes steps, its
// private sequence restarts on entry with the same delay as the step visibility.
func writeLayerIconClocks(buf *bytes.Buffer, hash string, links []layerLink, slots []layerSlot) {
	buf.WriteString(`<style><![CDATA[
@property --d2-icon-tour-phase{syntax:"<number>";inherits:true;initial-value:0}
@property --d2-icon-layer-phase{syntax:"<number>";inherits:true;initial-value:0}
@property --d2-icon-free-phase{syntax:"<number>";inherits:false;initial-value:-1}
@keyframes d2-icon-free{from{--d2-icon-free-phase:0}to{--d2-icon-free-phase:1}}
@keyframes d2-icon-tour{from{--d2-icon-tour-phase:0}to{--d2-icon-tour-phase:1}}
@keyframes d2-icon-layer{from{--d2-icon-layer-phase:0}to{--d2-icon-layer-phase:1}}
.d2-icon-tour-clock{animation:d2-icon-tour var(--d2-icon-film-ms) linear infinite;animation-play-state:var(--d2-icon-tour-play,running)}
.d2-icon-layer-clock{animation:var(--d2-icon-private-clock,none)}
.d2-icon-trips{--d2-icon-elapsed:calc(var(--d2-icon-tour-phase) * var(--d2-icon-film) - var(--d2-icon-start));--d2-icon-t:calc(max(0,var(--d2-icon-elapsed)) * max(0,sign(var(--d2-icon-hold) - var(--d2-icon-elapsed))))}
.d2-icon-trips>g{--d2-icon-age:calc(var(--d2-icon-t) - var(--d2-icon-origin));--d2-icon-phase:var(--d2-icon-age);offset-path:var(--d2-icon-route);offset-anchor:0px 0px;offset-position:0px 0px;offset-rotate:0deg;offset-distance:calc(var(--d2-icon-phase) / var(--d2-icon-transit) * 100%);opacity:calc(var(--d2-icon-alive,1) * clamp(0,var(--d2-icon-age) * 1000000 + 1,1) * clamp(0,(var(--d2-icon-transit) - var(--d2-icon-phase)) * 1000000,1))}
@supports(offset-path:shape(from 0px 0px,line to 1px 1px) view-box){.d2-icon-trips>g{offset-path:var(--d2-icon-shape)}}
`)
	fmt.Fprintf(buf, `.d2-layer-link-%s:target~.d2-frame-%s{--d2-icon-tour-play:paused}`, hash, hash)
	for i, s := range slots {
		fmt.Fprintf(buf, `#d2-frame-%s-%d{--d2-icon-layer-duration:%g;--d2-icon-layer-start:%g}`, hash, i, float64(s.totalMS)/1000, float64(s.startMS)/1000)
	}
	for _, link := range links {
		selectTarget := "#" + link.id + ":target~"
		if len(link.legs) == 0 || slots[link.to].count == 1 {
			delay := link.duration
			if len(link.legs) == 0 {
				delay = 0 // Plain fragment links switch immediately.
			}
			fmt.Fprintf(buf, `%s#d2-frame-%s-%d .d2-icon-trips>g{--d2-icon-age:calc(var(--d2-icon-offset,0) + var(--d2-icon-free-phase) * (var(--d2-icon-end) - var(--d2-icon-offset,0)));animation:d2-icon-free var(--d2-icon-free-duration) linear calc(var(--d2-click-time,%dms) + var(--d2-icon-delay,0s)) var(--d2-icon-count,1) var(--d2-icon-fill,forwards)}`, selectTarget, hash, link.to, delay)
			continue
		}
		s := slots[link.to]
		delay := "calc(var(--d2-click-time," + strconv.Itoa(link.duration) + "ms) - " + strconv.Itoa(s.startMS) + "ms)"
		fmt.Fprintf(buf, `%s.d2-layer-%s-%d{--d2-icon-private-clock:d2-icon-layer %dms linear %s infinite both}`, selectTarget, hash, s.id, s.totalMS, delay)
		fmt.Fprintf(buf, `%s.d2-layer-%s-%d .d2-icon-trips{--d2-icon-elapsed:calc(var(--d2-icon-layer-phase) * var(--d2-icon-layer-duration) - var(--d2-icon-layer-start))}`, selectTarget, hash, s.id)
	}
	buf.WriteString(`]]></style>`)
}
