//go:build js && wasm

package d2wasm

import (
	"encoding/json"
	"testing"
)

func TestSequenceV2WASMCompile(t *testing.T) {
	call := wrapWASMCall(Compile)
	defer call.Release()
	request, err := json.Marshal(CompileRequest{Opts: &CompileOptions{}, FS: map[string]string{"index": `shape: sequence-diagram {
  mirrored: true; numbered: true
}
request: {shape: edge-group {branched: true}; label.style.fill: red; enter: {a -> b.work: Start}; leave: {b.work -> a: Done}}
b.work: Processing
`}})
	if err != nil {
		t.Fatal(err)
	}
	got := call.Invoke(string(request)).String()
	var response struct {
		Data  CompileResponse `json:"data"`
		Error *WASMError      `json:"error"`
	}
	if err := json.Unmarshal([]byte(got), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	if root := response.Data.Graph.Root; root == nil || root.Sequence == nil || !root.Sequence.Mirrored || !root.Sequence.Numbered {
		t.Fatal("missing graph sequence options")
	}
	frame := false
	for _, obj := range response.Data.Graph.Objects {
		if obj.ID == "request" {
			frame = obj.Sequence != nil && obj.Sequence.Branched && obj.LabelFill != nil && obj.LabelFill.Value == "red"
		}
	}
	if !frame {
		t.Fatal("missing frame options or caption fill")
	}
	shapes := make(map[string]bool)
	for _, s := range response.Data.Diagram.Shapes {
		shapes[s.ID] = true
		if s.ID == "request.enter" && s.SequenceRole != "branch" {
			t.Fatalf("branch shape role = %q", s.SequenceRole)
		}
	}
	for _, id := range []string{"a", "b", "b.work", "a.mirror", "b.mirror", "request", "request.enter"} {
		if !shapes[id] {
			t.Errorf("missing shape %q", id)
		}
	}
	messages := 0
	for _, c := range response.Data.Diagram.Connections {
		if c.Label == "1. Start" || c.Label == "2. Done" {
			if c.SequenceRole != "message" {
				t.Fatalf("message connection role = %q", c.SequenceRole)
			}
			messages++
		}
	}
	if messages != 2 {
		t.Errorf("numbered messages = %d, want 2", messages)
	}
}
