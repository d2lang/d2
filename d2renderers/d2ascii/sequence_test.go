package d2ascii_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2ascii"
	"github.com/d2lang/d2/d2renderers/d2ascii/charset"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/log"
)

func TestSequenceBranchCaptionsAndRoutes(t *testing.T) {
	data, err := os.ReadFile("../../e2etests/testdata/sequence_v2/branched_shared_rows/dagre/board.exp.json")
	if err != nil {
		t.Fatal(err)
	}
	var board d2target.Diagram
	if err := json.Unmarshal(data, &board); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []charset.Type{charset.ASCII, charset.Unicode} {
		output, err := d2ascii.NewASCIIartist().Render(log.WithDefault(t.Context()), &board, &d2ascii.RenderOpts{Charset: ch})
		if err != nil {
			t.Fatal(err)
		}
		text := string(output)
		i := strings.Index(text, "[Missing]")
		if !strings.Contains(text, "Outcome [Available]") || i < 0 {
			t.Fatalf("lost the frame title or guard:\n%s", text)
		}
		before := strings.Split(text[:i], "\n")
		sep := before[len(before)-2]
		if strings.Count(sep, "- ") < 8 {
			t.Fatalf("branch divider does not span the frame:\n%s", text)
		}
		if strings.Count(text, charset.New(ch).ArrowLeft()) != 2 || strings.Count(text, charset.New(ch).ArrowRight()) != 1 {
			t.Fatalf("a message lost its arrowhead:\n%s", text)
		}
	}
}
