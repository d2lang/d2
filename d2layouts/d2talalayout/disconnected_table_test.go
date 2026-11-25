package d2talalayout_test

import "testing"

func TestDisconnectedTablePreservesTreeRanks(t *testing.T) {
	const fan = `direction: down
Intake -> Verify
Intake -> Review
Intake -> Approve
`
	tree := hierarchyGeometry(t, fan)
	withTable := hierarchyGeometry(t, fan+"Registry: {shape: sql_table; id: int {constraint: primary_key}; name: varchar}\n")
	for _, id := range []string{"Verify", "Review", "Approve"} {
		a, b := tree[id], withTable[id]
		originA, originB := tree["Intake"], withTable["Intake"]
		if a.Pos.X-originA.Pos.X != b.Pos.X-originB.Pos.X || a.Pos.Y-originA.Pos.Y != b.Pos.Y-originB.Pos.Y {
			t.Errorf("%s moved relative to Intake when an unrelated table was added", id)
		}
	}
}
