package d2isometricimg

import (
	"bytes"
	"context"
	"encoding/json"
	"image/color"
	"image/png"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2isometric"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2target"
)

func TestExplodedLayerOrderAndAdmission(t *testing.T) {
	root := captureDiagram()
	root.Layers = []*d2target.Diagram{
		{Name: "top"},
		{Name: "folder", IsFolderOnly: true, Layers: []*d2target.Diagram{{Name: "nested"}}},
		{Name: "bottom"},
	}
	root.Scenarios = []*d2target.Diagram{{Name: "scenario"}}
	root.Steps = []*d2target.Diagram{{Name: "step"}}
	decks, err := explodedLayers(context.Background(), root, []string{"layers", "selected"})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, deck := range decks {
		paths = append(paths, strings.Join(deck.path, "."))
	}
	want := []string{"root.layers.selected", "root.layers.selected.layers.top", "root.layers.selected.layers.folder.layers.nested", "root.layers.selected.layers.bottom"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("layer order %q, want %q", paths, want)
	}

	for _, gap := range []int{-1, d2target.MaxIsometricLayerGap + 1} {
		root.Layers[2].IsometricLayerGap = &gap
		if _, err := explodedLayers(context.Background(), root, nil); err == nil {
			t.Fatalf("accepted invalid gap %d", gap)
		}
	}
	root.Layers[2].IsometricLayerGap = nil
	root.Layers[2].Layers = []*d2target.Diagram{root}
	if _, err := explodedLayers(context.Background(), root, nil); err == nil {
		t.Fatal("accepted a repeated board")
	}
}

func TestExplodedLayerGapPreservesOtherPairs(t *testing.T) {
	decks := func(top, middle int) []explodedDeck {
		d := []explodedDeck{
			{board: &d2target.Diagram{IsometricLayerGap: &top}, scene: &d2isometric.Scene{}, height: .8},
			{board: &d2target.Diagram{IsometricLayerGap: &middle}, scene: &d2isometric.Scene{}, height: 1.5},
			{board: &d2target.Diagram{}, scene: &d2isometric.Scene{}, height: .2},
		}
		placeExplodedDecks(d, 6, 4, .1)
		return d
	}
	initial, changed := decks(90, 0), decks(90, 140)
	for i := range changed {
		if changed[i].width != 6 || changed[i].depth != 4 || changed[i].thickness != .1 || changed[i].offset.X != 0 {
			t.Fatalf("deck %d lost common footprint: %+v", i, changed[i])
		}
	}
	for i, want := range []float64{0, 140 * d2isometric.SceneScale} {
		pitch := func(d []explodedDeck) float64 { return d[i].surfaceY - d[i+1].surfaceY }
		if got := pitch(changed) - pitch(initial); math.Abs(got-want) > 1e-8 || pitch(initial) <= initial[i+1].height {
			t.Errorf("clearance change below deck %d = %g, want %g", i, got, want)
		}
	}
}

func TestExplodedSVGAndPNG(t *testing.T) {
	root := &d2target.Diagram{IsFolderOnly: true}
	for _, name := range []string{"Top", "Plain"} {
		d := captureDiagram()
		d.Name = name
		root.Layers = append(root.Layers, d)
	}
	root.Layers[0].Shapes[0].Link = `root.layers."Plain"`
	root.Layers[1].Shapes[0].Link = "https://d2lang.com/"
	root.Layers[1].Shapes[0].Tooltip = "external destination"
	root.Layers[1].Shapes[1].Link = "root.steps.omitted"
	root.Layers[1].Root.Fill = "transparent"
	root.Layers[1].Root.Label = "Destination floor"
	original, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Exploded: true, Width: 320, Height: 360, Format: SVG}
	data, err := Render(context.Background(), root, &o)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{`id="deck-0"`, `id="deck-1"`} {
		if got := bytes.Count(data, []byte(id)); got != 1 {
			t.Errorf("fragment %s occurs %d times", id, got)
		}
	}
	for _, fragment := range []string{`xlink:href="#deck-1"`, `xlink:href="https://d2lang.com/"`, "external destination"} {
		if !bytes.Contains(data, []byte(fragment)) {
			t.Errorf("SVG is missing %q", fragment)
		}
	}
	if bytes.Contains(data, []byte("root.steps.omitted")) {
		t.Error("SVG kept a fragment pointing outside the document")
	}
	o.Format = PNG
	data, err = Render(context.Background(), root, &o)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != o.Width || img.Bounds().Dy() != o.Height {
		t.Fatalf("PNG bounds %s", img.Bounds())
	}
	bg := color.NRGBAModel.Convert(img.At(0, 0))
	for half := 0; half < 2; half++ {
		ink := 0
		for y := half * o.Height / 2; y < (half+1)*o.Height/2; y++ {
			for x := 0; x < o.Width; x++ {
				if color.NRGBAModel.Convert(img.At(x, y)) != bg {
					ink++
				}
			}
		}
		if ink < 1000 {
			t.Errorf("only %d painted pixels in half %d", ink, half)
		}
	}
	// A compiled board with an explicit empty root caption must stay
	// unlabelled even when it has a non-empty name as a link target.
	root.Layers[0].Name = "renamed"
	renamed, err := Render(context.Background(), root, &o)
	root.Layers[0].Name = "Top"
	if err != nil || !bytes.Equal(data, renamed) {
		t.Fatalf("suppressed board caption changed the PNG after renaming: %v", err)
	}
	after, err := json.Marshal(root)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("render modified the input diagram")
	}
}

func TestExplodedAggregateMetadataBudget(t *testing.T) {
	root := &d2target.Diagram{IsFolderOnly: true, Layers: []*d2target.Diagram{captureDiagram(), captureDiagram()}}
	for i, name := range []string{"Top", "Base"} {
		root.Layers[i].Name = name
		root.Layers[i].Shapes[0].Link = "https://d2lang.com/"
	}
	o := Options{Exploded: true, Width: 128, Height: 128, LinkBudget: &d2scenebuild.LinkBudget{MaxRegions: 1, MaxStringBytes: 4096}, Format: SVG}
	if _, err := Render(context.Background(), root, &o); err == nil || !strings.Contains(err.Error(), "link region") {
		t.Fatalf("aggregate SVG link budget: %v", err)
	}
	o.Format = PNG
	if _, err := RenderPage(context.Background(), root, &o); err == nil || !strings.Contains(err.Error(), "link region") {
		t.Fatalf("aggregate page link budget: %v", err)
	}
}

func TestExplodedAggregateSourceBudget(t *testing.T) {
	long := strings.Repeat("x", 4<<20)
	root := &d2target.Diagram{IsFolderOnly: true, Layers: []*d2target.Diagram{{Name: "first", Description: long}, {Name: "second", Description: long}}}
	if _, err := explodedLayers(context.Background(), root, nil); err == nil || !strings.Contains(err.Error(), "text exceeds") {
		t.Fatalf("combined source text must be checked before mesh construction: %v", err)
	}
	root.Layers = root.Layers[:1]
	root.Scenarios = []*d2target.Diagram{{Description: long}}
	if _, err := explodedLayers(context.Background(), root, nil); err != nil {
		t.Fatalf("omitted scenario consumed source budget: %v", err)
	}
}
