package labeling

import (
	"testing"

	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/nodeshape"
)

// compareLabelPositionsReference is compareLabelPositions as it was before
// the tier preference sets were built once per node.
func compareLabelPositionsReference(node *layoutgraph.Node, first, second label.Position) int {
	score := func(position label.Position) int {
		score := -1
		for index, tier := range []nodeshape.LabelTier{nodeshape.Good, nodeshape.OK, nodeshape.Unideal, nodeshape.Bad} {
			if _, found := node.Shape.LabelPositionPreferences(tier)[position]; found {
				score = index + 1
			}
		}
		return score
	}
	firstScore, secondScore := score(first), score(second)
	if firstScore < secondScore {
		return 1
	}
	if firstScore > secondScore {
		return -1
	}
	return 0
}

// Cached tier preferences must compare every pair of positions as the
// reference does, for every shape type, also when the node is resized in
// between (the preference sets do not depend on the box).
func TestCachedLabelTierPreferencesMatchReference(t *testing.T) {
	shapes := []string{
		shape.SQUARE_TYPE, shape.REAL_SQUARE_TYPE, shape.PARALLELOGRAM_TYPE, shape.DOCUMENT_TYPE,
		shape.CYLINDER_TYPE, shape.QUEUE_TYPE, shape.PAGE_TYPE, shape.PACKAGE_TYPE, shape.STEP_TYPE,
		shape.CALLOUT_TYPE, shape.STORED_DATA_TYPE, shape.PERSON_TYPE, shape.C4_PERSON_TYPE,
		shape.DIAMOND_TYPE, shape.OVAL_TYPE, shape.CIRCLE_TYPE, shape.HEXAGON_TYPE, shape.CLOUD_TYPE,
		shape.TABLE_TYPE, shape.CLASS_TYPE, shape.TEXT_TYPE, shape.CODE_TYPE, shape.IMAGE_TYPE,
	}
	for _, shapeType := range shapes {
		node := layoutgraph.NewNode(1, 80, 40)
		node.SetShape(shapeType)
		var cached *labelTierPreferences
		for first := label.Position(-1); first <= label.BorderRightBottom+2; first++ {
			for second := label.Position(-1); second <= label.BorderRightBottom+2; second++ {
				node.Width = float64(20 + int(first)*3)
				want := compareLabelPositionsReference(node, first, second)
				if got := compareLabelPositions(node, first, second); got != want {
					t.Fatalf("%s %v %v: compareLabelPositions %d, want %d", shapeType, first, second, got, want)
				}
				if got := compareCachedLabelPositions(&cached, node, first, second); got != want {
					t.Fatalf("%s %v %v: cached %d, want %d", shapeType, first, second, got, want)
				}
			}
		}
	}
}
