package layoutgraph

import (
	"context"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// An operation that clears (and re-arms) its own transaction while Commit
// runs the operation list: Commit keeps running every operation it started
// with and none added after the Clear, as it did when Clear always started a
// new list (reusing the list's storage there nil'ed
// the entries Commit was still iterating and panicked).
func TestTransactionClearDuringCommitKeepsRunningOperations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, rearm := range []bool{false, true} {
		g := NewGraph()
		n := NewNode(1, 10, 10)
		n.TopLeft = geo.NewPoint(0, 0)
		g.AddNewNodeToContainer(nil, n)
		txn, err := g.newTransactionWithOptionsContext(ctx, TransactionOptions{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var ran []string
		addedRan := false
		txn.AddOp(func() error {
			ran = append(ran, "first")
			txn.Clear()
			if rearm {
				txn.AddOp(func() error {
					addedRan = true
					n.TopLeft.Y = 70
					return nil
				})
			}
			return nil
		})
		txn.AddOp(func() error {
			ran = append(ran, "second")
			n.TopLeft.X = 50
			return nil
		})
		txn.AddOp(func() error {
			ran = append(ran, "third")
			return nil
		})
		if err := txn.Commit(ctx); err != nil {
			t.Fatalf("rearm=%v: Commit = %v", rearm, err)
		}
		if len(ran) != 3 || ran[1] != "second" || ran[2] != "third" {
			t.Fatalf("rearm=%v: operations run %v, want first, second, third", rearm, ran)
		}
		if addedRan || n.TopLeft.X != 50 || n.TopLeft.Y != 0 {
			t.Fatalf("rearm=%v: added op ran=%v, node at (%v, %v), want (50, 0) without the added op", rearm, addedRan, n.TopLeft.X, n.TopLeft.Y)
		}
		wantOps := 0
		if rearm {
			wantOps = 1
		}
		if len(txn.Ops) != wantOps {
			t.Fatalf("rearm=%v: %d operations after Commit, want %d", rearm, len(txn.Ops), wantOps)
		}
		// The re-armed list commits on its own; Clear outside Commit reuses
		// the storage again.
		if rearm {
			if err := txn.Commit(ctx); err != nil {
				t.Fatalf("second Commit = %v", err)
			}
			if !addedRan || n.TopLeft.Y != 70 {
				t.Fatalf("second Commit: added op ran=%v, y=%v", addedRan, n.TopLeft.Y)
			}
		}
		txn.Clear()
		if len(txn.Ops) != 0 {
			t.Fatalf("rearm=%v: %d operations after Clear", rearm, len(txn.Ops))
		}
	}
}

// A Clear inside an operation of a nested Commit of the same transaction
// leaves both running lists intact.
func TestTransactionClearDuringNestedCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := NewGraph()
	n := NewNode(1, 10, 10)
	n.TopLeft = geo.NewPoint(0, 0)
	g.AddNewNodeToContainer(nil, n)
	txn, err := g.newTransactionWithOptionsContext(ctx, TransactionOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ran []string
	nested := false
	txn.AddOp(func() error {
		ran = append(ran, "outer-first")
		if nested {
			return nil
		}
		nested = true
		// Commit the same list again from inside; its first operation (this
		// one) returns at once, then the inner Commit runs the rest.
		return txn.Commit(ctx)
	})
	txn.AddOp(func() error {
		ran = append(ran, "clearing")
		txn.Clear()
		return nil
	})
	txn.AddOp(func() error {
		ran = append(ran, "last")
		return nil
	})
	if err := txn.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{"outer-first", "outer-first", "clearing", "last", "clearing", "last"}
	if len(ran) != len(want) {
		t.Fatalf("ran %v, want %v", ran, want)
	}
	for i := range want {
		if ran[i] != want[i] {
			t.Fatalf("ran %v, want %v", ran, want)
		}
	}
	if txn.runningOps != 0 {
		t.Fatalf("runningOps = %d after Commit", txn.runningOps)
	}
}
