// Package d2layoutfeatures checks graph features against D2's built-in layouts.
package d2layoutfeatures

import (
	"fmt"
	"strings"

	"github.com/d2lang/d2/d2graph"
)

type features uint8

const (
	nearObject features = 1 << iota
	containerDimensions
	topLeft
	descendantEdges
)

// Check rejects diagram attributes unsupported by the named built-in layout.
func Check(name string, g *d2graph.Graph) error {
	name = strings.ToLower(name)
	var supported features
	switch name {
	case "dagre":
	case "elk":
		supported = containerDimensions | descendantEdges
	case "tala":
		supported = nearObject | containerDimensions | topLeft | descendantEdges
	default:
		return fmt.Errorf("unknown built-in layout engine %q", name)
	}

	for _, obj := range g.Objects {
		if obj.Top != nil || obj.Left != nil {
			if supported&topLeft == 0 {
				return fmt.Errorf(`Object "%s" has attribute "top" and/or "left" set, but layout engine "%s" does not support locked positions. See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, obj.AbsID(), name)
			}
		}
		if (obj.WidthAttr != nil || obj.HeightAttr != nil) &&
			len(obj.ChildrenArray) > 0 && !obj.IsGridDiagram() {
			if supported&containerDimensions == 0 {
				return fmt.Errorf(`Object "%s" has attribute "width" and/or "height" set, but layout engine "%s" does not support dimensions set on containers. See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, obj.AbsID(), name)
			}
		}

		if obj.NearKey != nil {
			_, isKey := g.Root.HasChild(d2graph.Key(obj.NearKey))
			if isKey {
				if supported&nearObject == 0 {
					return fmt.Errorf(`Object "%s" has "near" set to another object, but layout engine "%s" only supports constant values for "near". See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, obj.AbsID(), name)
				}
			}
		}
	}
	if supported&descendantEdges == 0 {
		for _, e := range g.Edges {
			// descendant edges are ok in sequence diagrams
			if e.Src.OuterSequenceDiagram() != nil || e.Dst.OuterSequenceDiagram() != nil {
				continue
			}
			if !e.Src.IsContainer() && !e.Dst.IsContainer() {
				continue
			}
			if e.Src == e.Dst {
				return fmt.Errorf(`Connection "%s" is a self loop on a container, but layout engine "%s" does not support this. See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, e.AbsID(), name)
			}
			if e.Src.IsDescendantOf(e.Dst) || e.Dst.IsDescendantOf(e.Src) {
				return fmt.Errorf(`Connection "%s" goes from a container to a descendant, but layout engine "%s" does not support this. See https://d2lang.com/tour/layouts/#layout-specific-functionality for more.`, e.AbsID(), name)
			}
		}
	}
	return nil
}
