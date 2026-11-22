package labeling

import (
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/label"
)

func compareLabelPositions(node *layoutgraph.Node, first, second label.Position) int {
	return newLabelTierPreferences(node).compare(first, second)
}
