package d2animate

import (
	"fmt"
	"image"
	"math"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2target"
)

// LayerDuration reads the transition duration from the document root, even
// when only a subtree will be exported.
func LayerDuration(root *d2target.Diagram) (int, error) {
	if root.Config == nil || root.Config.AnimateDuration == nil {
		return 0, nil
	}
	ms := *root.Config.AnimateDuration
	if ms < 0 || ms > d2target.MaxTweenDurationMS {
		return 0, fmt.Errorf("animate-duration must be between 0 and %d milliseconds", d2target.MaxTweenDurationMS)
	}
	return int(ms), nil
}

type layerBoard struct {
	start, end int               // Half-open range of this subtree in preorder.
	depth      int               // Only layers introduce a new level of detail.
	layer      *d2target.Diagram // Boards sharing this layer use one coordinate system.
	parent     *d2target.Diagram // The board on which this layer is declared.
}

type boardPair struct {
	from, to *d2target.Diagram
}

type childBoard struct {
	parent     *d2target.Diagram
	kind, name string
}

// LayerNavigation resolves links between authored boards in a validated
// export tree. Tween samples are artwork, never navigation destinations.
type LayerNavigation struct {
	root       *d2target.Diagram
	path       []string
	boards     map[*d2target.Diagram]layerBoard
	children   map[childBoard]*d2target.Diagram
	focus      map[boardPair]*d2target.Shape
	layerFocus map[boardPair]*d2target.Shape
}

// NewLayerNavigation indexes a selected subtree. boardPath is its absolute
// D2 target path relative to root, e.g. ["layers", "api"].
func NewLayerNavigation(root *d2target.Diagram, boardPath []string) *LayerNavigation {
	nav := &LayerNavigation{
		root:       root,
		path:       boardPath,
		boards:     make(map[*d2target.Diagram]layerBoard),
		children:   make(map[childBoard]*d2target.Diagram),
		focus:      make(map[boardPair]*d2target.Shape),
		layerFocus: make(map[boardPair]*d2target.Shape),
	}
	count := 0
	var visit func(*d2target.Diagram, int, *d2target.Diagram, *d2target.Diagram)
	visit = func(board *d2target.Diagram, depth int, layer, parent *d2target.Diagram) {
		start := count
		count++
		for _, group := range []struct {
			kind     string
			depth    int
			children []*d2target.Diagram
		}{
			{"layers", depth + 1, board.Layers}, {"scenarios", depth, board.Scenarios}, {"steps", depth, board.Steps},
		} {
			for _, child := range group.children {
				nav.children[childBoard{board, group.kind, child.Name}] = child
				childLayer, childParent := layer, parent
				if group.kind == "layers" {
					childLayer, childParent = child, board
				}
				visit(child, group.depth, childLayer, childParent)
			}
		}
		nav.boards[board] = layerBoard{start, count, depth, layer, parent}
	}
	visit(root, 0, root, nil)
	for board := range nav.boards {
		for i := range board.Shapes {
			shape := &board.Shapes[i]
			if shape.Width <= 0 || shape.Height <= 0 {
				continue
			}
			if target := nav.destination(shape.Link); target != nil {
				// Studio uses the last matching shape when navigation wasn't
				// initiated by clicking one particular shape.
				nav.focus[boardPair{board, target}] = shape
				nav.layerFocus[boardPair{board, nav.boards[target].layer}] = shape
			}
		}
	}
	return nav
}

// HasLayerPortal reports whether an authored shape can take the camera between
// two drawable boards in this export tree.
func (nav *LayerNavigation) HasLayerPortal() bool {
	for pair := range nav.focus {
		if _, ok := nav.Zoom(pair.from, pair.to); ok {
			return true
		}
	}
	return false
}

// destination resolves a compiled absolute board link. Path components can
// contain literal dots, quotes and other characters escaped in D2 keys.
func (nav *LayerNavigation) destination(link string) *d2target.Diagram {
	if link != "root" && !strings.HasPrefix(link, "root.") {
		return nil
	}
	key, err := d2parser.ParseKey(link)
	if err != nil {
		return nil
	}
	path := key.StringIDA()[1:]
	if len(path) < len(nav.path) || !slices.Equal(path[:len(nav.path)], nav.path) {
		return nil
	}
	path = path[len(nav.path):]
	if len(path)%2 != 0 {
		return nil
	}
	dest := nav.root
	for i := 0; i < len(path); i += 2 {
		dest = nav.children[childBoard{dest, path[i], path[i+1]}]
		if dest == nil {
			return nil
		}
	}
	return dest
}

// LayerZoom moves through the linking shape on the enclosing board.
type LayerZoom struct {
	Focus  *d2target.Shape
	Inward bool
	offset image.Point // Placement of the enclosing board in the shared SVG canvas.
}

// Zoom uses the enclosing board's portal in both directions, even when
// the child has its own link pointing back to the parent.
func (nav *LayerNavigation) Zoom(from, to *d2target.Diagram) (LayerZoom, bool) {
	a, aOK := nav.boards[from]
	b, bOK := nav.boards[to]
	if !aOK || !bOK || from.IsFolderOnly || to.IsFolderOnly {
		return LayerZoom{}, false
	}
	inward := a.depth < b.depth
	if !inward {
		a, b = b, a
		from, to = to, from
	}
	shape := nav.focus[boardPair{from, to}]
	if shape == nil {
		// A return link inherited by an inner step still exits through the
		// enclosing board's shape that opens that layer.
		shape = nav.focus[boardPair{from, b.layer}]
	}
	if shape == nil {
		// A portal may open a particular step. Its other steps still return
		// through that shape after the layer's local playback advances.
		shape = nav.layerFocus[boardPair{from, b.layer}]
	}
	if a.depth == b.depth || nav.boards[a.layer].start > nav.boards[b.layer].start || nav.boards[b.layer].end > nav.boards[a.layer].end || shape == nil {
		return LayerZoom{}, false
	}
	return LayerZoom{Focus: shape, Inward: inward}, true
}

// LayerLeg visits two boards through a shape on their enclosing board.
type LayerLeg struct {
	From, To *d2target.Diagram
	Zoom     LayerZoom
}

// Route follows real portals up to the common layer and back down into the
// destination. A direct link takes precedence over the longer tree path.
// No route exists between two steps on the same layer or across a missing portal.
func (nav *LayerNavigation) Route(from, to *d2target.Diagram) []LayerLeg {
	if zoom, ok := nav.Zoom(from, to); ok {
		return []LayerLeg{{from, to, zoom}}
	}
	a, b := nav.boards[from], nav.boards[to]
	if a.layer == nil || b.layer == nil || a.layer == b.layer {
		return nil
	}
	left, right := from, to
	var rising, falling []*d2target.Diagram
	for a.layer != b.layer {
		if a.depth >= b.depth {
			rising = append(rising, left)
			left = a.parent
			a = nav.boards[left]
		} else {
			falling = append(falling, right)
			right = b.parent
			b = nav.boards[right]
		}
	}

	// At the join, use the exact endpoint if it is a step on the common
	// layer. Otherwise use the actual enclosing board, or its shared layer
	// when the branches were declared on different steps.
	pivot := left
	if len(falling) == 0 {
		pivot = right
	} else if len(rising) > 0 && left != right {
		pivot = a.layer
	}
	path := append(rising, pivot)
	slices.Reverse(falling)
	path = append(path, falling...)
	legs := make([]LayerLeg, 0, len(path)-1)
	for i := 1; i < len(path); i++ {
		zoom, ok := nav.Zoom(path[i-1], path[i])
		if !ok {
			return nil
		}
		legs = append(legs, LayerLeg{path[i-1], path[i], zoom})
	}
	return legs
}

// LayerPose maps artwork to viewport coordinates as scale*point + (DX,DY).
type LayerPose struct {
	Scale, DX, DY, Opacity float64
}

const (
	layerPanPhase    = 0.4
	layerShrinkPhase = 0.5
)

func studioExpo(t float64) float64 {
	if t <= 0 {
		return 0
	}
	return math.Exp2(10 * (min(t, 1) - 1))
}

// Poses returns the enclosing and inner boards' transforms, in that order
// for either direction. Studio uses 400ms for the pan, 600ms for the zoom,
// and 500ms for the outgoing child's fade. Scale each phase to the chosen
// transition duration.
func (z LayerZoom) Poses(viewport image.Rectangle, progress float64) (parent, child LayerPose) {
	t := max(0, min(progress, 1))
	if !z.Inward {
		t = 1 - t
	}
	pan := min(t/layerPanPhase, 1)
	pan = 1 - (1-pan)*(1-pan)
	zoom := studioExpo((t - layerPanPhase) / (1 - layerPanPhase))
	childZoom := zoom
	if !z.Inward {
		childZoom = studioExpo(1 - progress/layerShrinkPhase)
	}

	cx := float64(viewport.Min.X) + float64(viewport.Dx())/2
	cy := float64(viewport.Min.Y) + float64(viewport.Dy())/2
	fx := float64(z.Focus.Pos.X+z.offset.X) + float64(z.Focus.Width)/2
	fy := float64(z.Focus.Pos.Y+z.offset.Y) + float64(z.Focus.Height)/2
	fill := max(1, float64(viewport.Dx())/float64(z.Focus.Width), float64(viewport.Dy())/float64(z.Focus.Height))
	ps := 1 + (fill-1)*zoom
	cs := childZoom + (1-childZoom)/fill
	return LayerPose{ps, pan*(cx-fx) + (1-ps)*fx, pan*(cy-fy) + (1-ps)*fy, 1 - zoom},
		LayerPose{cs, (1 - cs) * cx, (1 - cs) * cy, childZoom}
}
