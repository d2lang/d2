package layoutgraph

import (
	"testing"

	"github.com/d2lang/d2/lib/label"
)

func TestOutsideIconReserveKeepsSiblingClearance(t *testing.T) {
	for _, container := range []*Node{nil, NewNode(3, 200, 180)} {
		g := NewGraph()
		owner := NewNode(1, 80, 60)
		owner.Icon = &Icon{Position: label.OutsideRightMiddle}
		g.AddNewNodeToContainer(container, owner)
		if got := owner.OutsideIconReserve(); got != MaxIconSize {
			t.Fatalf("automatic icon reserve = %v, want %d", got, MaxIconSize)
		}
		owner.Icon.FixPosition()
		if got := owner.OutsideIconReserve(); got != 32 {
			t.Fatalf("solitary fixed icon reserve = %v, want 32", got)
		}
		g.AddNewNodeToContainer(container, NewNode(2, 80, 60))
		if got := owner.OutsideIconReserve(); got != MaxIconSize {
			t.Fatalf("fixed icon with sibling reserve = %v, want %d", got, MaxIconSize)
		}
	}
}
