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
	// Like regular string interpolation, a partially resolved group of
	// references can be resolved again when its importer supplies a scope.
	retry bool
}

type markdownReplay struct {
	markdownBinding
	primary  *Scalar
	inImport bool
	missing  bool
	updated  map[string]string
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
	if r.updated == nil {
		r.updated = make(map[string]string, len(r.bound)+1)
		maps.Copy(r.updated, r.bound)
	}
	r.updated[key] = value
}

func (r *markdownReplay) finish(rendered string) {
	if !r.inImport || !strings.Contains(rendered, "${") {
		r.primary.markdown = nil
		return
	}
	if r.updated == nil && (len(r.bound) == 0 || r.retry == r.missing) {
		return
	}
	if r.updated != nil {
		r.bound = r.updated
	}
	r.primary.markdown = &markdownBinding{source: r.source, bound: r.bound, retry: r.missing}
}
