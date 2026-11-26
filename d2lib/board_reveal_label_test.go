package d2lib

import (
	"bytes"
	"encoding/xml"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

// Capture the rendered text, including multiline tspans, its transforms, and
// the opacity it inherits from the shape. Either the shape or a separate
// caption can paint the label as long as the visible result stays steady.
type arrivalLettering struct {
	element   string
	transform string
	opacity   float64
}

func visibleArrivalLettering(t *testing.T, board *d2target.Diagram, phrase string) []arrivalLettering {
	t.Helper()
	doc, err := d2svg.Render(board, nil)
	if err != nil {
		t.Fatal(err)
	}
	var results []arrivalLettering
	stack := []arrivalLettering{{opacity: 1}}
	d := xml.NewDecoder(bytes.NewReader(doc))
	for {
		at := d.InputOffset()
		token, err := d.Token()
		if err == io.EOF {
			return results
		}
		if err != nil {
			t.Fatal(err)
		}
		switch e := token.(type) {
		case xml.StartElement:
			ink := stack[len(stack)-1]
			for _, attr := range e.Attr {
				if attr.Name.Local == "transform" || e.Name.Local == "svg" && len(stack) > 2 {
					// A Markdown label has its own positioned SVG viewport.
					ink.transform += " " + attr.Name.Local + "=" + attr.Value
				}
				if attr.Name.Local == "style" {
					for _, decl := range strings.Split(attr.Value, ";") {
						if value, ok := strings.CutPrefix(decl, "opacity:"); ok {
							alpha, err := strconv.ParseFloat(value, 64)
							if err != nil {
								t.Fatal(err)
							}
							ink.opacity *= alpha
						}
					}
				}
			}
			if e.Name.Local != "text" {
				stack = append(stack, ink)
				continue
			}
			if err := d.Skip(); err != nil {
				t.Fatal(err)
			}
			ink.element = string(doc[at:d.InputOffset()])
			if ink.opacity > 0 && strings.Contains(ink.element, phrase) {
				results = append(results, ink)
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

func TestBoardRevealKeepsVisibleLetteringStill(t *testing.T) {
	for _, tc := range []struct {
		name, base, edit, id, phrase string
	}{
		{"spring", "order -> queue", "ship: Shipping; queue -> ship", "ship", "Shipping"},
		{"reverse multiline", "queue", `report: "Settlement report\nfor finance" {style.opacity: 0.45}; report -> queue`, "report", "Settlement report"},
		{"container", "inbox -> queue", "audit: {review: Compliance review; archive: Archive; review -> archive}; queue -> audit.review", "audit.review", "Compliance review"},
		{"markdown", "queue", "note: |md **Important** |; queue -> note", "note", "Important"},
		{"markdown rectangle", "queue", "status: |md **Available** | {shape: rectangle}; queue -> status", "status", "Available"},
		{"table row", "queue", "ledger: Ledger {shape: sql_table; account_number: int}; queue -> ledger", "ledger", "account_number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "vars: {d2-config: {animate-duration: 800ms; animate-interval: 1600ms}}\ndirection: right\n" +
				tc.base + "\nsteps: {add: {" + tc.edit + "}}"
			root, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
			step := root.Steps[0]
			cut, _ := compileContinuity(t, strings.Replace(source, "800ms", "0ms", 1), d2talalayout.ContinueBoard)
			if !reflect.DeepEqual(step.Shapes, cut.Steps[0].Shapes) || !reflect.DeepEqual(step.Connections, cut.Steps[0].Connections) {
				t.Fatal("label animation changed the authored endpoint")
			}
			want := visibleArrivalLettering(t, step, tc.phrase)
			if len(want) != 1 || len(step.TweenFrames) < 3 {
				t.Fatalf("expected one settled label and a sampled arrival, got %d labels and %d samples", len(want), len(step.TweenFrames))
			}
			body := shapeByID(t, step, tc.id)
			if want[0].opacity != body.Opacity {
				t.Fatalf("settled label opacity %g, want authored opacity %g", want[0].opacity, body.Opacity)
			}
			visible, whileMoving, rebound := false, false, false
			for i, frame := range step.TweenFrames {
				got := visibleArrivalLettering(t, frame, tc.phrase)
				if len(got) == 0 {
					if visible {
						t.Fatalf("label blinked off at sample %d", i)
					}
					continue
				}
				if len(got) != 1 || got[0] != want[0] {
					t.Fatalf("sample %d changed visible label: got %+v, want %+v", i, got, want)
				}
				visible = true
				s := shapeByID(t, frame, tc.id)
				if s.Opacity == 0 {
					t.Fatalf("label appeared before its node at sample %d", i)
				}
				whileMoving = whileMoving || !sameSlot(&s, &body)
				rebound = rebound || s.Width > body.Width
			}
			if !visible {
				t.Fatal("label never appeared during the transition")
			}
			if tc.name == "spring" && !rebound {
				t.Fatal("label was hidden during the box's rebound")
			}
			if (tc.name == "spring" || tc.name == "markdown rectangle") && !whileMoving {
				t.Fatal("box must still be opening on the first visible label frames")
			}
		})
	}
}

func TestBoardRevealPreservesOuterMarkdownLink(t *testing.T) {
	const outer = "https://example.com/outer"
	const inner = "https://example.com/inline"
	for _, shape := range []string{"text", "rectangle"} {
		t.Run(shape, func(t *testing.T) {
			source := `vars: {d2-config: {animate-duration: 800ms; animate-interval: 1600ms}}
queue
steps: {add: {note: |md [Help](` + inner + ` "Inline title") | {shape: ` + shape + `; link: "` + outer + `"}; queue -> note}}`
			d, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
			step := d.Steps[0]
			if len(step.TweenFrames) < 2 {
				t.Fatal("expected a sampled arrival")
			}
			visible := 0
			for i, f := range step.TweenFrames[1:] {
				s := shapeByID(t, f, "note")
				if s.Opacity == 0 || s.Label == "" {
					continue
				}
				visible++
				doc, err := d2svg.Render(f, nil)
				if err != nil {
					t.Fatal(err)
				}
				if s.Link != outer || !bytes.Contains(doc, []byte(`href="`+outer+`"`)) || bytes.Count(doc, []byte(`class="appendix-icon"`)) != 1 {
					t.Fatalf("visible Markdown missing its outer link or badge at sample %d", i+1)
				}
				if bytes.Contains(doc, []byte(`href="`+inner+`"`)) || bytes.Contains(doc, []byte("<title>Inline title</title>")) {
					t.Fatalf("nested Markdown target became active at sample %d", i+1)
				}
			}
			if visible == 0 {
				t.Fatal("expected a visible linked caption during the transition")
			}
		})
	}
}
