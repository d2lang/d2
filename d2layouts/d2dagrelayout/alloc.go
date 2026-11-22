package d2dagrelayout

import (
	"github.com/d2lang/d2/lib/geo"
)

// Batched allocation for values the layout creates by the thousand. Every
// value handed out is a distinct object, as with individual allocations, and
// every slice is capped at its length, so an append copies it instead of
// growing into its neighbor. The arenas' methods are not inlined: they are
// called from many places, where inlined copies would only add code.

// pointArena hands out points from chunks of at least chunk points.
type pointArena struct {
	buf   []geo.Point
	chunk int
}

//go:noinline
func (a *pointArena) take(n int) []geo.Point {
	if n > len(a.buf) {
		a.buf = make([]geo.Point, max(n, a.chunk))
	}
	s := a.buf[:n:n]
	a.buf = a.buf[n:]
	return s
}

// pointRefArena hands out point slices from chunks of at least chunk entries.
type pointRefArena struct {
	buf   []*geo.Point
	chunk int
}

//go:noinline
func (a *pointRefArena) take(n int) []*geo.Point {
	if n > len(a.buf) {
		a.buf = make([]*geo.Point, max(n, a.chunk))
	}
	s := a.buf[:n:n]
	a.buf = a.buf[n:]
	return s
}

// stringArena hands out pointers to new strings from chunks of at least
// chunk strings.
type stringArena struct {
	buf   []string
	chunk int
}

//go:noinline
func (a *stringArena) ptr(s string) *string {
	if len(a.buf) == 0 {
		a.buf = make([]string, max(1, a.chunk))
	}
	p := &a.buf[0]
	*p = s
	a.buf = a.buf[1:]
	return p
}
