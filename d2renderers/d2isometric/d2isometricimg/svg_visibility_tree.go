package d2isometricimg

import (
	"math"
	"math/bits"

	"github.com/d2lang/d2/lib/xsort"
)

type svgVisibilityTreeNode struct {
	box                     svgBox
	left, right, start, end int
}

// Dense meshes can place thousands of small faces in a uniform-grid cell.
// A median bounding-box tree keeps those queries proportional to actual local
// overlap, rather than the unrelated faces sharing a coarse spatial bucket.
type svgVisibilityTree struct {
	polygons []svgVisibilityPolygon
	indices  []int
	nodes    []svgVisibilityTreeNode
}

func svgNewVisibilityTree(polygons []svgVisibilityPolygon, faces []svgVisibilityFace, budget *svgVisibilityBudget) (*svgVisibilityTree, error) {
	tree := &svgVisibilityTree{polygons: polygons}
	for i, p := range polygons {
		if len(p.points) > 0 && faces[i].opaque {
			tree.indices = append(tree.indices, i)
		}
	}
	if len(tree.indices) == 0 {
		return tree, nil
	}
	var build func(int, int) (int, error)
	build = func(start, end int) (int, error) {
		if err := budget.spend(end - start); err != nil {
			return 0, err
		}
		box := svgBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		centers := box
		for _, id := range tree.indices[start:end] {
			b := polygons[id].box
			box.minX, box.minY = math.Min(box.minX, b.minX), math.Min(box.minY, b.minY)
			box.maxX, box.maxY = math.Max(box.maxX, b.maxX), math.Max(box.maxY, b.maxY)
			x, y := (b.minX+b.maxX)/2, (b.minY+b.maxY)/2
			centers.minX, centers.minY = math.Min(centers.minX, x), math.Min(centers.minY, y)
			centers.maxX, centers.maxY = math.Max(centers.maxX, x), math.Max(centers.maxY, y)
		}
		index := len(tree.nodes)
		tree.nodes = append(tree.nodes, svgVisibilityTreeNode{box: box, left: -1, right: -1, start: start, end: end})
		if end-start <= 16 {
			return index, nil
		}
		xAxis := centers.maxX-centers.minX >= centers.maxY-centers.minY
		center := func(id int) float64 {
			b := polygons[id].box
			if xAxis {
				return b.minX + b.maxX
			}
			return b.minY + b.maxY
		}
		if err := budget.spend((end - start) * bits.Len(uint(end-start))); err != nil {
			return 0, err
		}
		// Indices are distinct, so this is a total order.
		xsort.Func(tree.indices[start:end], func(a, b int) int {
			ca, cb := center(a), center(b)
			switch {
			case ca < cb:
				return -1
			case ca > cb:
				return 1
			}
			return a - b
		})
		middle := (start + end) / 2
		left, err := build(start, middle)
		if err != nil {
			return 0, err
		}
		right, err := build(middle, end)
		if err != nil {
			return 0, err
		}
		tree.nodes[index].left, tree.nodes[index].right = left, right
		return index, nil
	}
	_, err := build(0, len(tree.indices))
	return tree, err
}

// query appends the faces whose bounds overlap box to result. The traversal
// stack is the caller's scratch storage and is returned for reuse.
func (t *svgVisibilityTree) query(box svgBox, budget *svgVisibilityBudget, stack []int32, result []int) ([]int32, []int, error) {
	if len(t.nodes) == 0 {
		return stack, result, nil
	}
	stack = append(stack[:0], 0)
	for len(stack) > 0 {
		if err := budget.spend(1); err != nil {
			return stack, result, err
		}
		node := &t.nodes[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		if !node.box.overlaps(box) {
			continue
		}
		if node.left >= 0 {
			stack = append(stack, int32(node.right), int32(node.left))
			continue
		}
		if err := budget.spend(node.end - node.start); err != nil {
			return stack, result, err
		}
		for _, id := range t.indices[node.start:node.end] {
			if t.polygons[id].box.overlaps(box) {
				result = append(result, id)
			}
		}
	}
	return stack, result, nil
}
