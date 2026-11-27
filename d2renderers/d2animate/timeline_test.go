package d2animate

import (
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestBoardIsHeldLongEnoughToRead(t *testing.T) {
	board := func(caption string) *d2target.Diagram {
		d := d2target.NewDiagram()
		d.Shapes = []d2target.Shape{{ID: "a", Type: d2target.ShapeRectangle, Width: 100, Height: 60, Opacity: 1}}
		if caption != "" {
			d.Root.Annotation = &d2target.Annotation{Label: caption}
		}
		return d
	}
	total := func(frames []Frame) int {
		ms := 0
		for _, f := range frames {
			ms += f.DurationMS
		}
		return ms
	}

	// The interval is a floor: a board with nothing to read keeps it.
	if got := total(AppendBoardFrames(nil, board(""), 800)); got != 800 {
		t.Fatalf("silent board is held %dms, want the 800ms interval", got)
	}
	// A short caption fits inside a long interval.
	short := board("Go.")
	if got := total(AppendBoardFrames(nil, short, 5000)); got != 5000 {
		t.Fatalf("short caption is held %dms, want the 5000ms interval", got)
	}
	// A sentence outlasts a short interval.
	sentence := board("One replica has fallen four seconds behind the primary.")
	want := sentence.ReadingMS()
	if want <= 800 {
		t.Fatalf("test sentence reads in %dms, expected longer than the interval", want)
	}
	if got := total(AppendBoardFrames(nil, sentence, 800)); got != want {
		t.Fatalf("sentence is held %dms, want its reading time %dms", got, want)
	}

	// With a transition, the samples take their own time out of the same
	// hold and the board keeps the rest.
	sampled := board("One replica has fallen four seconds behind the primary.")
	sampled.TweenFrames = []*d2target.Diagram{board(""), board("")}
	sampled.TweenDurationMS = 400
	frames := AppendBoardFrames(nil, sampled, 800)
	if got := total(frames); got != want {
		t.Fatalf("sampled sentence is held %dms, want %dms", got, want)
	}
	if last := frames[len(frames)-1]; last.Diagram != sampled || last.DurationMS != want-400 {
		t.Fatalf("the board itself is held %dms after its transition, want %dms", last.DurationMS, want-400)
	}
}
