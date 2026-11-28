package d2lib

import (
	"context"
	"testing"

	d2log "github.com/d2lang/d2/lib/log"
)

func TestMovingIconSurvivesParallelEdgeRenumbering(t *testing.T) {
	const source = `a -> b: first {icon.style.animated: 1/s}
a -> b: second {icon.style.animated: 2/s}
steps: {
  remove: {(a -> b)[0]: null}
  replace: {
    (a -> b)[1]: null
    a -> b: third {icon.style.animated: 3/s}
  }
}`
	calls := 0
	board, _, err := Compile(d2log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"(a -> b)[1]", ""} {
		e := board.Steps[i].Connections[0]
		if e.ID != "(a -> b)[0]" || e.IconAnimation.PreviousID == nil || *e.IconAnimation.PreviousID != want {
			t.Fatalf("step %d: edge %q label %q has icon predecessor %+v, want %q", i, e.ID, e.Label, e.IconAnimation, want)
		}
	}
}
