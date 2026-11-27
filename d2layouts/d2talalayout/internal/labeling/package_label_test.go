package labeling

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

func TestPackageTopLabelPlacementUsesTabWhenItFits(t *testing.T) {
	t.Parallel()

	t.Run("container default InsideTopCenter places inside tab", func(t *testing.T) {
		t.Parallel()

		g := layoutgraph.NewGraph()
		pkg := layoutgraph.NewNode(1, 200, 200)
		pkg.TopLeft = geo.NewPoint(0, 0)
		pkg.SetShape(shape.PACKAGE_TYPE)
		pkg.SetContainer(true)
		pkg.Label = &layoutgraph.Label{
			Text:   "aa",
			Width:  40,
			Height: 20,
		}
		child := layoutgraph.NewNode(2, 60, 40)
		child.TopLeft = geo.NewPoint(70, 90)
		child.Container = pkg
		g.Nodes = []*layoutgraph.Node{pkg, child}
		g.Containers[nil] = []*layoutgraph.Node{pkg}
		g.Containers[pkg] = []*layoutgraph.Node{child}

		Initialize(g)
		if err := Place(context.Background(), g); err != nil {
			t.Fatalf("Place() error = %v", err)
		}
		if pkg.Label.Position != label.InsideTopCenter {
			t.Fatalf("pkg.Label.Position = %v, want InsideTopCenter", pkg.Label.Position)
		}
		tl := pkg.LabelTopLeft(pkg.Label.Position, pkg.Label.Width, pkg.Label.Height)
		if tl.X != 30 || tl.Y != 5 {
			t.Fatalf("LabelTopLeft() = (%v, %v), want (30, 5)", tl.X, tl.Y)
		}
	})

	t.Run("fixed top-left places inside tab when it fits and below when it overflows", func(t *testing.T) {
		t.Parallel()

		pkg := layoutgraph.NewNode(1, 200, 200)
		pkg.TopLeft = geo.NewPoint(0, 0)
		pkg.SetShape(shape.PACKAGE_TYPE)

		fit := pkg.LabelTopLeft(label.InsideTopLeft, 40, 20)
		if fit.X != 5 || fit.Y != 5 {
			t.Fatalf("fitting LabelTopLeft() = (%v, %v), want (5, 5)", fit.X, fit.Y)
		}

		overflow := pkg.LabelTopLeft(label.InsideTopLeft, 95, 20)
		if overflow.X != 5 || overflow.Y != 45 {
			t.Fatalf("overflowing LabelTopLeft() = (%v, %v), want (5, 45)", overflow.X, overflow.Y)
		}
	})

	t.Run("KeepInBody code label stays in main body and survives clone", func(t *testing.T) {
		t.Parallel()

		g := layoutgraph.NewGraph()
		pkg := layoutgraph.NewNode(1, 200, 200)
		pkg.TopLeft = geo.NewPoint(0, 0)
		pkg.SetShape(shape.PACKAGE_TYPE)
		pkg.SetContainer(true)
		pkg.Label = &layoutgraph.Label{
			Text:       "hi",
			Width:      40,
			Height:     20,
			KeepInBody: true,
		}
		child := layoutgraph.NewNode(2, 60, 40)
		child.TopLeft = geo.NewPoint(70, 90)
		child.Container = pkg
		g.Nodes = []*layoutgraph.Node{pkg, child}
		g.Containers[nil] = []*layoutgraph.Node{pkg}
		g.Containers[pkg] = []*layoutgraph.Node{child}

		Initialize(g)
		if err := Place(context.Background(), g); err != nil {
			t.Fatalf("Place() error = %v", err)
		}
		tl := pkg.LabelTopLeft(label.InsideTopCenter, pkg.Label.Width, pkg.Label.Height)
		if tl.X != 80 || tl.Y != 45 {
			t.Fatalf("KeepInBody LabelTopLeft(InsideTopCenter) = (%v, %v), want (80, 45)", tl.X, tl.Y)
		}
		tlLeft := pkg.LabelTopLeft(label.InsideTopLeft, pkg.Label.Width, pkg.Label.Height)
		if tlLeft.X != 5 || tlLeft.Y != 45 {
			t.Fatalf("KeepInBody LabelTopLeft(InsideTopLeft) = (%v, %v), want (5, 45)", tlLeft.X, tlLeft.Y)
		}

		cloned, err := layoutgraph.Clone(context.Background(), g)
		if err != nil {
			t.Fatalf("Clone() error = %v", err)
		}
		var clonedPkg *layoutgraph.Node
		for _, n := range cloned.Nodes {
			if n.ID == pkg.ID {
				clonedPkg = n
				break
			}
		}
		if clonedPkg == nil || clonedPkg.Label == nil || !clonedPkg.Label.KeepInBody {
			t.Fatalf("cloned package did not preserve KeepInBody")
		}
		clonedTL := clonedPkg.LabelTopLeft(label.InsideTopCenter, clonedPkg.Label.Width, clonedPkg.Label.Height)
		if clonedTL.X != 80 || clonedTL.Y != 45 {
			t.Fatalf("cloned KeepInBody LabelTopLeft() = (%v, %v), want (80, 45)", clonedTL.X, clonedTL.Y)
		}
	})

	t.Run("inside icon stays in main body via IconTopLeft", func(t *testing.T) {
		t.Parallel()

		pkg := layoutgraph.NewNode(1, 200, 200)
		pkg.TopLeft = geo.NewPoint(0, 0)
		pkg.SetShape(shape.PACKAGE_TYPE)

		tl := pkg.IconTopLeft(label.InsideTopLeft, 24, 24)
		if tl.X != 5 || tl.Y != 45 {
			t.Fatalf("IconTopLeft(InsideTopLeft) = (%v, %v), want (5, 45)", tl.X, tl.Y)
		}
		center := pkg.IconTopLeft(label.InsideTopCenter, 24, 24)
		if center.X != 88 || center.Y != 45 {
			t.Fatalf("IconTopLeft(InsideTopCenter) = (%v, %v), want (88, 45)", center.X, center.Y)
		}
	})
}
