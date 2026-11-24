package d2animate

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestShareStationaryGridShapes(t *testing.T) {
	root := d2target.NewDiagram()
	root.IsFolderOnly = true
	board := d2target.NewDiagram()
	board.GridMotion = true
	root.Layers = []*d2target.Diagram{board}
	group := func(id, body string) string {
		board.Shapes = append(board.Shapes, d2target.Shape{ID: id})
		return `<g class="` + base64.URLEncoding.EncodeToString([]byte(id)) + `">` + body + `</g>`
	}
	cell := func(label string) string {
		return `<g class="shape" ><rect x="10" y="10" width="80" height="40" class=" fill-B6 stroke-B1" style="stroke-width:2;"/></g><text class="text-bold" x="50" y="35" style="text-anchor:middle;font-size:14px">` + label + `</text>`
	}
	left := group("left", cell("Left")) + group("neighbor", cell("Neighbor"))
	right := group("right", cell("Right"))
	moving := group("moving", cell("Different position"))
	original := group("interactive", cell("Has a tooltip"))
	board.Shapes[len(board.Shapes)-1].Tooltip = "A tooltip"
	var resources string
	for id, content := range map[string]string{
		"stops": `<rect fill="url(#localGradient)"/>`, "ink": `<g filter="url(#shadow)"/>`,
		"clip": `<rect clip-path="url(#localClip)"/>`, "anchor": `<a href="#dest"><rect/></a>`,
		"timer": `<rect><animate attributeName="x"/></rect>`, "identified": `<rect id="resource"/>`,
		"event": `<rect onclick="click()"/>`, "titled": `<title>Hover here</title>`,
	} {
		resources += group(id, content)
	}
	open := `<g class="d2-test " viewBox="0 0 100 100">`
	var frames [][]byte
	for _, x := range []string{"1", "2", "3", "4"} {
		frames = append(frames, []byte(open+left+strings.Replace(moving, `x="10"`, `x="`+x+`"`, 1)+right+original+resources+"</g>"))
	}
	defs, out := shareStaticShapes(root, frames, "d2-test")
	// A run on each side of the moving shape preserves their stacking order.
	if bytes.Count(defs, []byte(`id="d2-test-grid-static-`)) != 2 || !bytes.Contains(defs, []byte(`class="d2-test">`+left)) || !bytes.Contains(defs, []byte(right)) {
		t.Fatalf("unexpected shared definitions %s", defs)
	}
	var definitions struct {
		Groups []struct {
			ID     string `xml:"id,attr"`
			Markup string `xml:",innerxml"`
		} `xml:"g"`
	}
	if err := xml.Unmarshal(defs, &definitions); err != nil {
		t.Fatal(err)
	}
	var replacements []string
	for _, g := range definitions.Groups {
		replacements = append(replacements, `<use href="#`+g.ID+`"/>`, g.Markup)
	}
	for i := range out {
		restored := strings.NewReplacer(replacements...).Replace(string(out[i]))
		if restored != string(frames[i]) {
			t.Errorf("frame %d lost stacking order, content, or an interaction", i)
		}
	}
}

func TestStaticShapeSharingFallback(t *testing.T) {
	root := d2target.NewDiagram()
	root.GridMotion = true
	root.Shapes = []d2target.Shape{{ID: "a"}}
	group := `<g class="YQ=="><text>` + strings.Repeat("alpha", 24) + `</text></g>`
	frame := []byte(`<g class="d2-test">` + group + `</g>`)
	for _, tc := range []struct {
		name string
		last string
	}{
		{"missing", `<g class="d2-test">` + strings.ReplaceAll(group, "YQ==", "Yg==") + `</g>`},
		{"changed", strings.ReplaceAll(string(frame), "alpha", "beta")},
		{"duplicate", `<g class="d2-test">` + group + group + `</g>`},
		{"malformed", string(frame[:len(frame)-1])},
		{"id collision", `<g class="d2-test"><g id="d2-test-grid-static-0"/>` + group + `</g>`},
		{"transformed parent", `<g class="d2-test" transform="translate(10)">` + group + `</g>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := [][]byte{frame, frame, []byte(tc.last)}
			defs, out := shareStaticShapes(root, frames, "d2-test")
			if len(defs) != 0 || !bytes.Equal(out[0], frame) || !bytes.Equal(out[2], []byte(tc.last)) {
				t.Fatal("expected intact input frames")
			}
		})
	}
	root.GridMotion = false
	if defs, _ := shareStaticShapes(root, [][]byte{frame, frame}, "d2-test"); defs != nil {
		t.Error("ordinary board animation changed")
	}
}
