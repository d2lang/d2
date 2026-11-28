package d2svg

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
)

func sequenceGroup(step int) string {
	return svg.OpenElement("g", svg.IntAttr("data-d2-sequence-step", step))
}

// All the messages share a clock. A message stays visible after its turn,
// including the final hold. Without screen animation, the SVG is the complete
// authored drawing, including links and label masks.
func writeSequencePlaybackCSS(buf *bytes.Buffer, hash string, duration int) {
	buf.WriteString("@media screen and (prefers-reduced-motion:no-preference){")
	for start := 0; start < duration-d2target.SequenceMessageIntervalMS; start += d2target.SequenceMessageIntervalMS {
		step := strconv.Itoa(start/d2target.SequenceMessageIntervalMS + 1)
		writeSequenceArrowCSS(buf, hash, start/d2target.SequenceMessageIntervalMS+1, start, duration)
		name := hash + "-message-" + step
		// The fixed head, labels, masks and annotations all enter on arrival.
		fmt.Fprintf(buf, ".%s [data-d2-sequence-step=%q],.%s [data-d2-sequence-arrived=%q]{animation:%s %dms steps(1,end) infinite}", hash, step, hash, step, name, duration)
		fmt.Fprintf(buf, "@keyframes %s{0%%{visibility:hidden}%s%%,100%%{visibility:visible}} ", name, sequencePercent(start+d2target.SequenceMessageRevealMS, duration))
	}
	buf.WriteString("} ")
}

func sequencePercent(elapsed, duration int) string {
	return svg.FormatFloat(100 * float64(elapsed) / float64(duration))
}
