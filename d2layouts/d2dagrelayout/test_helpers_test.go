package d2dagrelayout

import (
	"github.com/d2lang/d2/d2graph"
)

func newObjectIndex(g *d2graph.Graph) *objectIndex {
	return new(layoutScratch).objectIndex(g)
}

// countParallelEdges returns, for every edge i, the number of edges from
// src[i] to dst[i] (sameWay), and that number plus, when src[i] != dst[i],
// the number of edges from dst[i] to src[i] (bothWays).
func countParallelEdges(src, dst []*d2graph.Object) (sameWay, bothWays []int) {
	return countParallelEdgesInto(make([]int, 2*len(src)), src, dst)
}
