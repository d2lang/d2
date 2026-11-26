package d2ir

import (
	"maps"
	"strings"
)

// Markdown can be evaluated in its source module and again in the importer.
// Keep the authored text and completed substitutions together so a value
// containing ${...} remains literal while unresolved references can bind later.
// Once published on a Scalar, the map is shared read-only by IR copies.
type markdownBinding struct {
	source string
	bound  map[string]string
}

type markdownReplay struct {
	markdownBinding
	primary  *Scalar
	inImport bool
	added    map[string]string
}

func newMarkdownReplay(primary *Scalar, value string, inImport bool) markdownReplay {
	r := markdownReplay{primary: primary, inImport: inImport}
	if primary.markdown != nil {
		r.markdownBinding = *primary.markdown
	} else {
		r.source = value
	}
	return r
}

func (r *markdownReplay) lookup(key string) (string, bool) {
	value, ok := r.bound[key]
	return value, ok
}

func (r *markdownReplay) record(key, value string) {
	if !r.inImport {
		return
	}
	if r.added == nil {
		r.added = make(map[string]string)
	}
	r.added[key] = value
}

func (r *markdownReplay) finish(rendered string) {
	if !r.inImport || !strings.Contains(rendered, "${") {
		r.primary.markdown = nil
		return
	}
	if len(r.added) == 0 {
		return
	}
	maps.Copy(r.added, r.bound)
	r.primary.markdown = &markdownBinding{source: r.source, bound: r.added}
}
