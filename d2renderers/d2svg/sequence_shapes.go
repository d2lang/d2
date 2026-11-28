package d2svg

import (
	"bytes"
	"fmt"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
)

// Keep labels and link targets at their authored size. Only the painted body
// of a span or group grows down from its top edge.
func writeSequenceShapeCSS(buf *bytes.Buffer, hash string, duration int, shapes []d2target.Shape) {
	buf.WriteString("@media screen and (prefers-reduced-motion:no-preference){")
	for i := range shapes {
		s := &shapes[i]
		if s.SequenceStep == 0 {
			continue
		}
		key := fmt.Sprint(i + 1)
		name := hash + "-sequence-shape-" + key
		start := s.SequenceStartMS()
		writeSequenceAppearance(buf, hash, "shape", key, name, start, duration)

		frames := s.SequenceKeyframes()
		complete := start
		if len(frames) > 1 && s.Height > 0 {
			grow := name + "-grow"
			fmt.Fprintf(buf, ".%s [data-d2-sequence-shape=%q] .shape{transform-box:fill-box;transform-origin:center top;animation:%s %dms linear infinite}", hash, key, grow, duration)
			fmt.Fprintf(buf, "@keyframes %s{", grow)
			for _, frame := range frames {
				fmt.Fprintf(buf, "%s%%{transform:scaleY(%s)}", sequencePercent(frame.TimeMS, duration), svg.FormatFloat(float64(frame.Height)/float64(s.Height)))
			}
			buf.WriteString("100%{transform:scaleY(1)}}")
			complete = frames[len(frames)-1].TimeMS
		}
		if s.Annotation != nil || s.LeavingAnnotation != nil {
			writeSequenceAppearance(buf, hash, "complete", key, name+"-complete", complete, duration)
		}
	}
	buf.WriteString("} ")
}

func writeSequenceAppearance(buf *bytes.Buffer, hash, attr, key, name string, at, duration int) {
	fmt.Fprintf(buf, ".%s [data-d2-sequence-%s=%q]{animation:%s %dms steps(1,end) infinite}", hash, attr, key, name, duration)
	fmt.Fprintf(buf, "@keyframes %s{0%%{visibility:hidden}%s%%,100%%{visibility:visible}}", name, sequencePercent(at, duration))
}
