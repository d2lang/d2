package d2layouts

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
)

func TestPartialLayoutSurvivesOlderCoreResponse(t *testing.T) {
	g := compileLayoutTestGraph(t, "stage")
	g.IsPartialLayout = true
	core := func(_ context.Context, part *d2graph.Graph) error {
		if !part.IsPartialLayout {
			t.Fatal("nested core received a whole-board flag")
		}
		part.Objects[0].Box = geo.NewBox(geo.NewPoint(0, 0), 80, 40)
		payload, err := d2graph.SerializeGraph(part)
		if err != nil {
			return err
		}
		// An older layout core returns a valid graph with no partial flag.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil {
			return err
		}
		delete(fields, "isPartialLayout")
		payload, err = json.Marshal(fields)
		if err != nil {
			return err
		}
		return d2graph.DeserializeGraph(payload, part)
	}
	if err := LayoutNested(log.WithDefault(t.Context()), g, GraphInfo{}, core, nil); err != nil {
		t.Fatal(err)
	}
	if !g.IsPartialLayout {
		t.Fatal("the returned partial graph lost its enclosing layout context")
	}
}
