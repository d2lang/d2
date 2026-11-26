package lspserver

import (
	"strings"
	"testing"

	"github.com/d2lang/util-go/assert"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2oracle"
)

// L78 compares the public LSP operation with D2's graph editor on shape
// renames supported by both, including the value of a near attribute.
func TestL78_RenameMatchesOracle(t *testing.T) {
	const uri = "untitled:oracle"
	for _, tc := range []struct {
		name, source, key, newName string
		board                      []string
	}{
		{"near", "¦a\nnote: \"about a\" {near: a}", "a", "b", nil},
		{"dotted near", "¦a\nnote.near: a\nother: {near: top-center}", "a", "b", nil},
		{"descendant near", "¦box: {item}\nnote: {near: box.item}", "box", "group", nil},
		{"nested shape", "box: {¦item: Label}\nbox.item -> other\nother.item", "box.item", "worker", nil},
		{"parent path", "¦db\nbox: {client -> _.db}", "db", "store", nil},
		{"edges and attributes", "¦api -> db\n(api -> db)[0].style.stroke: red\napi.shape: page", "api", "web", nil},
		{"layer", "root shape\nlayers: {one: {¦a}}", "a", "c", []string{"one"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := strings.Replace(tc.source, "¦", "", 1)
			graph, _, err := d2compiler.Compile("diagram.d2", strings.NewReader(source), nil)
			assert.Success(t, err)
			renamed, _, err := d2oracle.Rename(graph, tc.board, tc.key, tc.newName)
			assert.Success(t, err)

			client := newTestClient(t)
			pos := openAtCursor(t, client, uri, tc.source)
			changes := client.rename(pos, tc.newName)
			assert.Equal(t, 1, len(changes))
			edited := applyEdits(t, source, changes[uri])
			got, _, err := d2compiler.Compile("diagram.d2", strings.NewReader(edited), nil)
			assert.Success(t, err)
			assert.JSON(t, renameResult(renamed), renameResult(got))
			if len(tc.board) > 0 {
				assert.JSON(t, renameResult(d2oracle.GetBoardGraph(renamed, tc.board)), renameResult(d2oracle.GetBoardGraph(got, tc.board)))
			}
		})
	}
}

// Compare the diagram rather than the particular text edits or formatting.
// Edge IDs include their endpoints, arrow direction, and parallel-edge index.
type renamedShape struct{ Shape, Label, Near string }

type renamedDiagram struct {
	Shapes      map[string]renamedShape
	Connections map[string]string
}

func renameResult(g *d2graph.Graph) renamedDiagram {
	out := renamedDiagram{
		Shapes:      make(map[string]renamedShape),
		Connections: make(map[string]string),
	}
	for _, obj := range g.Objects {
		var near string
		if obj.NearKey != nil {
			near = d2format.Format(obj.NearKey)
		}
		out.Shapes[obj.AbsID()] = renamedShape{obj.Shape.Value, obj.Label.Value, near}
	}
	for _, edge := range g.Edges {
		out.Connections[edge.AbsID()] = edge.Label.Value
	}
	return out
}
