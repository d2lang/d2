package textmeasure

import "github.com/rivo/uniseg"

// graphemes iterates over the grapheme clusters of a string. uniseg.Graphemes
// finds the same clusters but also tracks word and sentence boundaries, which
// D2 never asks for and whose Unicode tables would be linked in with it.
type graphemes struct {
	cluster string
	rest    string
	offset  int
	width   int
	state   int
}

func newGraphemes(s string) *graphemes {
	return &graphemes{rest: s, state: -1}
}

// Next advances to the next cluster and reports whether there was one.
func (g *graphemes) Next() bool {
	if g.rest == "" {
		return false
	}
	g.offset += len(g.cluster)
	g.cluster, g.rest, g.width, g.state = uniseg.FirstGraphemeClusterInString(g.rest, g.state)
	return true
}

func (g *graphemes) Str() string {
	return g.cluster
}

func (g *graphemes) Runes() []rune {
	return []rune(g.cluster)
}

// Width is the number of monospace cells the cluster occupies.
func (g *graphemes) Width() int {
	return g.width
}

// Positions returns the byte range of the cluster in the original string.
func (g *graphemes) Positions() (start, end int) {
	return g.offset, g.offset + len(g.cluster)
}
