package d2scenebuild

import (
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2sketch"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
)

// Apply the pencil passes between the path's fill and outline. The clip uses
// the actual rough-go silhouette, even for a compound shape such as a cylinder.
func (b *builder) addPencilFill(node *d2scene.Node, path d2scene.Path, s d2target.Shape, pencil *d2sketch.PencilFill, object string) error {
	paper, err := b.paint(pencil.Paper, object+" pencil paper")
	if err != nil {
		return err
	}
	ink, err := b.paint(pencil.Ink, object+" pencil fill")
	if err != nil {
		return err
	}
	path.Fill = paper
	if len(pencil.Bands) == 0 {
		node.Primitive = path
		return nil
	}
	commands := len(pencil.Bands) * 5
	extraCommands := commands + len(path.Commands) // bands and their silhouette clip
	if pencil.GraphiteOpacity > 0 {
		extraCommands += commands
	}
	if path.Stroke != nil {
		extraCommands += len(path.Commands)
	}
	if err := b.chargeSketchPathCommands(object+" pencil shading", extraCommands); err != nil {
		return err
	}
	x := float64(s.Pos.X) - node.Transform.E
	y := float64(s.Pos.Y) - node.Transform.F
	bands := d2scene.Path{Commands: make([]d2scene.PathCommand, 0, commands), Fill: ink}
	for _, band := range pencil.Bands {
		bands.Commands = append(bands.Commands, d2scene.MoveTo(x+band[0].X, y+band[0].Y))
		for _, p := range band[1:] {
			bands.Commands = append(bands.Commands, d2scene.LineTo(x+p.X, y+p.Y))
		}
		bands.Commands = append(bands.Commands, d2scene.ClosePath())
	}

	stroke := path.Stroke
	path.Stroke = nil
	node.Primitive = path
	path.Fill = nil
	layer := d2scene.NewNode(nil)
	layer.ID = node.ID + ":pencil"
	layer.Clip = &d2scene.Clip{Path: path, Transform: d2scene.Identity()}
	layer.Children = []*d2scene.Node{d2scene.NewNode(bands)}
	node.Children = append(node.Children, layer)
	if pencil.GraphiteOpacity > 0 {
		bands.Fill, err = b.paint(color.N1, object+" pencil graphite")
		if err != nil {
			return err
		}
		graphite := d2scene.NewNode(bands)
		graphite.Opacity = pencil.GraphiteOpacity
		layer.Children = append(layer.Children, graphite)
	}
	if stroke != nil {
		path.Stroke = stroke
		node.Children = append(node.Children, d2scene.NewNode(path))
	}
	return nil
}
