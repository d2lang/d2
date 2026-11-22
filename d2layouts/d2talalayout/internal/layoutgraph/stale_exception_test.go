package layoutgraph

import (
	"context"
	"errors"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// TestStaleIndexDoesNotExceptCheckedContainer: the
// overlap exception index is reused, not rebuilt, by consecutive dirty-path
// commits, so after g.Nodes is reordered outside a commit and UpdateState is
// called it still names nodes by their old positions. The original's
// exceptions were only the checked node's descendants and ancestors; the
// descendant walk also marks the checked container itself (its seen set), and
// that mark must not except the candidate that the stale index places at the
// container's old position. The original rejects this commit (P overlaps A).
func TestStaleIndexDoesNotExceptCheckedContainer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := NewGraph()
	A := NewNode(1, 400, 400)
	A.TopLeft = geo.NewPoint(0, 0)
	a1 := NewNode(2, 50, 50)
	a1.TopLeft = geo.NewPoint(20, 20)
	P := NewNode(3, 400, 400)
	P.TopLeft = geo.NewPoint(2000, 0)
	p1 := NewNode(4, 50, 50)
	p1.TopLeft = geo.NewPoint(2020, 20)
	g.AddNewNodeToContainer(nil, P)
	g.AddNewNodeToContainer(A, a1)
	g.AddNewNodeToContainer(nil, A)
	g.AddNewNodeToContainer(P, p1)

	txn, err := g.newTransactionWithOptionsContext(ctx, TransactionOptions{IgnoreContainerEscape: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	txn.AddOp(func() error { return nil })
	if err := txn.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	txn.Clear()
	g.Nodes[0], g.Nodes[2] = g.Nodes[2], g.Nodes[0]
	if err := txn.UpdateState(); err != nil {
		t.Fatal(err)
	}
	txn.AddOp(func() error {
		P.TopLeft.X = 300
		return nil
	})
	if err := txn.Commit(ctx); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("commit of overlapping containers = %v, want ErrInvalidCandidate (as the original)", err)
	}
	if P.TopLeft.X != 2000 {
		t.Fatalf("P.x = %v after the rejected commit, want 2000", P.TopLeft.X)
	}
}
