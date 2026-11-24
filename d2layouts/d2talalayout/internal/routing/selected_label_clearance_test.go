package routing

import (
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestLabelRepairCannotWorsenAnotherOverlap(t *testing.T) {
	text := func(x float64) labelInk {
		return labelInk{box: *geo.NewBox(geo.NewPoint(x, 0), 20, 10), text: true}
	}
	before, after := []labelInk{text(10)}, []labelInk{text(20)}
	// Clear the label on the left while burying more of the one on the
	// right. Counting just the number of contacts would accept this.
	obstacles := []labelInk{text(-1), text(29)}
	if gain := textClearanceGain(before, after, obstacles); gain >= 0 {
		t.Fatalf("accepted a deeper overlap, gain %d", gain)
	}
	if gain := textClearanceGain(before, after, obstacles[:1]); gain != 1 {
		t.Fatalf("expected one cleared label, gain %d", gain)
	}
}
