package d2isometricimg

import (
	"fmt"
	"strings"
)

// Pattern artwork and its dependencies can recur on many distinct surfaces.
// The keys include all serialized attributes and children except the generated ID.
type nativeSVGPatterns struct {
	ids     map[string]string
	pending strings.Builder
}

// One retained surface can cover hundreds of mesh triangles and also cast a
// shadow. Serialize its vectors once per export, then reuse the same definition
// through each geometry/lighting transform; paper patterns stay vector artwork.
func (w *nativeSVGWriter) surfaceDefinition(surface *nativeVectorSurface) string {
	if w.err != nil {
		return ""
	}
	if w.surfaces == nil {
		w.surfaces = make(map[*nativeVectorSurface]string)
	}
	if id := w.surfaces[surface]; id != "" {
		return id
	}
	id := fmt.Sprintf("surface-%d", len(w.surfaces))
	fragment, err := nativeSurfaceSVGWithPatterns(w.ctx, surface, id+"-", &w.patterns)
	if err != nil {
		w.err = err
		return ""
	}
	w.write(`<defs>%s<g id="%s">%s</g></defs>`, w.patterns.pending.String(), id, fragment)
	w.patterns.pending.Reset()
	if w.err != nil {
		return ""
	}
	w.surfaces[surface] = id
	return id
}
