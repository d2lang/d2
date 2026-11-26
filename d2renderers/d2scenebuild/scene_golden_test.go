package d2scenebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// sceneGoldenCases pins whole built scenes for every arrowhead and built-in
// fill pattern over several stroke widths and route directions. The digests
// in testdata/scene_golden.txt were recorded from the table-free
// implementation; refactors must keep every scene byte-identical.
func sceneGoldenCases(t *testing.T) map[string]string {
	t.Helper()
	got := make(map[string]string)
	routes := map[string][]*geo.Point{
		"east":  {{X: 20, Y: 10}, {X: 80, Y: 10}},
		"west":  {{X: 80, Y: 10}, {X: 20, Y: 10}},
		"diag":  {{X: 20, Y: 20}, {X: 47.25, Y: 33.5}, {X: 80, Y: 3}},
		"south": {{X: 10, Y: 20}, {X: 10, Y: 77.5}},
	}
	digest := func(name string, diagram *d2target.Diagram) {
		pad := int64(0)
		document, err := Build(context.Background(), diagram, Options{Pad: &pad})
		if err != nil {
			got[name] = "error: " + err.Error()
			return
		}
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		sum := sha256.Sum256(data)
		got[name] = hex.EncodeToString(sum[:])
	}
	for _, arrowhead := range allArrowheads {
		for _, width := range []int{1, 2, 3, 5, 8} {
			for routeName, route := range routes {
				diagram := validDiagram()
				diagram.Connections[0].SrcArrow = arrowhead
				diagram.Connections[0].DstArrow = d2target.CfMany
				if width%2 == 1 {
					diagram.Connections[0].SrcArrow, diagram.Connections[0].DstArrow = d2target.ArrowArrowhead, arrowhead
				}
				diagram.Connections[0].StrokeWidth = width
				diagram.Connections[0].Route = route
				digest(fmt.Sprintf("arrow/%s/%d/%s", arrowhead, width, routeName), diagram)
			}
		}
	}
	for _, pattern := range []string{"", "none", "dots", "lines", "grain", "paper", "DOTS", "bogus"} {
		for _, shapeType := range []string{d2target.ShapeRectangle, d2target.ShapeCircle, d2target.ShapeHexagon} {
			diagram := validDiagram()
			diagram.Shapes[0].FillPattern = pattern
			diagram.Shapes[0].Type = shapeType
			diagram.Shapes[1].FillPattern = pattern
			diagram.Shapes[1].Multiple = true
			digest(fmt.Sprintf("pattern/%s/%s", pattern, shapeType), diagram)
		}
	}
	for _, pattern := range []string{"", "dots", "paper"} {
		for _, double := range []bool{false, true} {
			for _, radius := range []int{0, 7} {
				diagram := validDiagram()
				diagram.Root.FillPattern = pattern
				diagram.Root.DoubleBorder = double
				diagram.Root.BorderRadius = radius
				diagram.Root.Fill = "#eef"
				diagram.Root.Stroke = "#123"
				diagram.Root.StrokeWidth = 3
				digest(fmt.Sprintf("root/%s/%v/%d", pattern, double, radius), diagram)
			}
		}
	}
	for _, position := range []string{"", "OUTSIDE_TOP_LEFT", "OUTSIDE_BOTTOM_RIGHT", "OUTSIDE_RIGHT_MIDDLE", "INSIDE_MIDDLE_CENTER", "BORDER_TOP_CENTER", "BORDER_LEFT_BOTTOM"} {
		for _, shapeType := range []string{d2target.ShapeRectangle, d2target.ShapeHexagon} {
			for _, modifier := range []string{"plain", "3d", "multiple"} {
				for _, language := range []string{"", "markdown"} {
					diagram := validDiagram()
					a := &diagram.Shapes[0]
					a.Type = shapeType
					a.Width, a.Height = 60, 40
					a.Label, a.LabelPosition, a.Language = "hello *world*", position, language
					a.FontSize, a.LabelWidth, a.LabelHeight = 16, 70, 21
					a.ThreeDee = modifier == "3d"
					a.Multiple = modifier == "multiple"
					digest(fmt.Sprintf("label/%s/%s/%s/%s", position, shapeType, modifier, language), diagram)
				}
			}
		}
	}
	return got
}

func TestSceneGolden(t *testing.T) {
	got := sceneGoldenCases(t)
	keys := make([]string, 0, len(got))
	for key := range got {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key + " " + got[key] + "\n")
	}
	const path = "testdata/scene_golden.txt"
	if os.Getenv("SCENE_GOLDEN_RECORD") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != b.String() {
		wantLines := strings.Split(string(want), "\n")
		gotLines := strings.Split(b.String(), "\n")
		for i := range min(len(wantLines), len(gotLines)) {
			if wantLines[i] != gotLines[i] {
				t.Errorf("golden mismatch:\nwant %s\n got %s", wantLines[i], gotLines[i])
			}
		}
		t.Fatalf("scene golden differs (%d vs %d lines)", len(wantLines), len(gotLines))
	}
}
