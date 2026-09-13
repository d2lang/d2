package d2ir

import "testing"

func TestCompositeContainsNodeHandlesExistingCycle(t *testing.T) {
	root := &Map{}
	child := &Field{Composite: root}
	root.Fields = []*Field{child}

	if !compositeContainsNode(root, child) {
		t.Fatal("compositeContainsNode did not find child in cyclic structure")
	}
	if compositeContainsNode(root, &Field{}) {
		t.Fatal("compositeContainsNode found unrelated node in cyclic structure")
	}
}
