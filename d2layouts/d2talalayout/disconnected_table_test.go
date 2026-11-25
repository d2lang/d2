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

func TestTreeJunctionRetainsFixedCoordinates(t *testing.T) {
	const source = `direction: right
Review: {left: 125; top: 165}
Reference: {left: 600; top: 165}
Intake -> Review -> Verify
Review -> Audit
Review -> Settle
`
	for _, tc := range []struct{ name, extra string }{
		{"junction", ""},
		{"disconnected SQL table", "Registry: {shape: sql_table; id: int {constraint: primary_key}}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			positions := hierarchyGeometry(t, source+tc.extra)
			for id, wantX := range map[string]int{"Review": 125, "Reference": 600} {
				got := positions[id].Pos
				if got.X != wantX || got.Y != 165 {
					t.Errorf("%s at (%d, %d), want (%d, 165)", id, got.X, got.Y, wantX)
				}
			}
		})
	}
}

func TestTreeJunctionStaysNearFixedNeighbor(t *testing.T) {
	const source = `direction: down
Hub: {near: Reference}
Input -> Hub -> Verify
Hub -> Audit
Hub -> Settle
Reference: {left: 600; top: 360}
Registry: {shape: sql_table; id: int {constraint: primary_key}; name: varchar}
`
	shapes := hierarchyGeometry(t, source)
	ref := shapes["Reference"]
	if ref.Pos.X != 600 || ref.Pos.Y != 360 {
		t.Fatalf("fixed reference moved to (%d, %d)", ref.Pos.X, ref.Pos.Y)
	}
	// Measure gaps between body rectangles so a wide neighboring shape
	// can't appear farther away simply because its center is farther away.
	distanceSquared := func(id string) int {
		n := shapes[id]
		dx := max(0, n.Pos.X-ref.Pos.X-ref.Width, ref.Pos.X-n.Pos.X-n.Width)
		dy := max(0, n.Pos.Y-ref.Pos.Y-ref.Height, ref.Pos.Y-n.Pos.Y-n.Height)
		return dx*dx + dy*dy
	}
	want := distanceSquared("Hub")
	for id := range shapes {
		if id != "Reference" && distanceSquared(id) < want {
			t.Errorf("%s ended up closer to Reference than the requested Hub", id)
		}
	}
}
