package d2elklayout

import (
	"context"
	"encoding/json"
	"fmt"

	elk "github.com/d2lang/elk-go"
)

// runELKViaJSON is runELK as the original laid requests out, the reference
// the tests compare the typed path with: json.Marshal of the request,
// elk.LayoutJSON, then json.Unmarshal of the response into elkGraph. The
// original then looked each edge's container up by the ID the response gave
// among its objects' IDs, which are the nodes' IDs: containerRefs records the
// node with that ID, or 0 (no object) when no node has it, where layoutWith
// reads containers.
func runELKViaJSON(ctx context.Context, elkGraph *ELKGraph, request *elkGoRequest) error {
	request.sharedSet = false // decoding the response writes the shared option structs
	// The nodes' IDs by elk.NodeRef (the root, then depth first, as build
	// adds them), before decoding the response overwrites them.
	ids := []string{elkGraph.ID}
	var walk func(children []*ELKNode)
	walk = func(children []*ELKNode) {
		for _, n := range children {
			if n != nil {
				ids = append(ids, n.ID)
				walk(n.Children)
			}
		}
	}
	walk(elkGraph.Children)

	raw, err := json.Marshal(elkGraph)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	response, err := elk.LayoutJSON(raw)
	if err != nil {
		return fmt.Errorf("native ELK layout failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := json.Unmarshal(response, &elkGraph); err != nil {
		return err
	}

	request.containerRefs = request.containerRefs[:0]
	for _, e := range elkGraph.Edges {
		if e == nil {
			continue
		}
		ref := int32(-1)
		if e.Container != "" {
			ref = 0
			for i := 1; i < len(ids); i++ {
				if ids[i] == e.Container {
					ref = int32(i)
				}
			}
		}
		request.containerRefs = append(request.containerRefs, ref)
	}
	return nil
}

// setJSONOracle makes runELK lay requests out through runELKViaJSON
// (viaJSON) or through the typed path.
func setJSONOracle(viaJSON bool) {
	jsonOracle = nil
	if viaJSON {
		jsonOracle = runELKViaJSON
	}
}
