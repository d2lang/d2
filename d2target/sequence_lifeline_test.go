package d2target

import (
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestSequenceLifelineLabelGaps(t *testing.T) {
	line := func(x, top, bottom float64) Connection {
		c := *BaseConnection()
		c.SequenceRole = "lifeline"
		c.Stroke = "black"
		c.Route = []*geo.Point{geo.NewPoint(x, top), geo.NewPoint(x, bottom)}
		return c
	}
	diagram := Diagram{Connections: []Connection{
		line(0, 0, 300), line(100, 0, 80), line(100, 100, 200), line(200, 0, 300),
	}}
	// A branch guard crosses the first two actors, but only the later segment
	// of the second actor. The far actor and authored message must keep their strokes.
	guard := *BaseShape()
	guard.SequenceRole = "branch"
	guard.Type = ShapeRectangle
	guard.LabelPosition = label.InsideTopLeft.String()
	guard.Pos = Point{X: -10, Y: 100}
	guard.Width, guard.Height = 240, 150
	guard.Text = Text{Label: "[missing]", LabelWidth: 120, LabelHeight: 20}
	diagram.Shapes = []Shape{guard}
	message := *BaseConnection()
	message.SequenceRole = "message"
	message.Text = Text{Label: "across three actors", LabelWidth: 50, LabelHeight: 20}
	message.LabelPosition = label.OutsideTopCenter.String()
	message.Route = []*geo.Point{geo.NewPoint(0, 60), geo.NewPoint(200, 60)}
	diagram.Connections = append(diagram.Connections, message)

	gaps := diagram.SequenceLifelineLabelGaps(false, nil)
	if len(gaps) != 3 || len(gaps[0]) != 1 || len(gaps[1]) != 1 || len(gaps[2]) != 1 {
		t.Fatalf("expected exactly one hole in each intersected segment, got %+v", gaps)
	}
	if a, b := gaps[0][0], gaps[2][0]; *a.TopLeft != *b.TopLeft || a.Width != 124 || a.Height != 24 || a.Opacity != 1 {
		t.Fatalf("inconsistent branch guard opening across actors: %+v, %+v", a, b)
	}
	if a := gaps[1][0]; a.TopLeft.X >= 100 || a.TopLeft.X+a.Width <= 100 || a.TopLeft.Y+a.Height > 60 {
		t.Fatalf("expected a message opening above the arrow, got %+v", a)
	}

	t.Run("paint and animation", func(t *testing.T) {
		animated := diagram
		animated.Shapes = []Shape{guard}
		animated.Connections = append([]Connection(nil), diagram.Connections...)
		animated.Shapes[0].Color = "transparent"
		m := &animated.Connections[4]
		m.MotionLabel = &MotionLabel{Point: Point{X: -10, Y: 150}, Opacity: .5}
		m.MotionClip = &MotionClip{Point: Point{X: -5, Y: 155}, Width: 20, Height: 30}
		gaps := animated.SequenceLifelineLabelGaps(false, nil)
		if len(gaps) != 1 || len(gaps[0]) != 1 {
			t.Fatalf("expected only the visible, moved caption to mask its lifeline, got %+v", gaps)
		}
		if g := gaps[0][0]; *g.TopLeft != (geo.Point{X: -5, Y: 155}) || g.Width != 20 || g.Height != 17 || g.Opacity != .5 {
			t.Fatalf("incorrect clipped, fading caption opening: %+v", g)
		}
		m.MotionLabel.Opacity = 0
		if gaps := animated.SequenceLifelineLabelGaps(false, nil); len(gaps) != 0 {
			t.Fatalf("hidden captions left holes: %+v", gaps)
		}
	})

	t.Run("endpoint caption", func(t *testing.T) {
		m := message
		m.Label = ""
		m.SrcLabel = &Text{Label: "status", LabelWidth: 28, LabelHeight: 16}
		topLeft := m.GetArrowheadLabelPosition(false)
		x := topLeft.X + float64(m.SrcLabel.LabelWidth)/2
		d := Diagram{Connections: []Connection{line(x, 0, 300), m}}
		if gaps := d.SequenceLifelineLabelGaps(false, nil); len(gaps) != 1 || len(gaps[0]) != 1 {
			t.Fatalf("endpoint caption did not clear its lifeline: %+v", gaps)
		}
	})

	t.Run("sketch bow", func(t *testing.T) {
		m := message
		m.MotionLabel = &MotionLabel{Point: Point{X: 5, Y: 1500}, Opacity: 1}
		d := Diagram{Connections: []Connection{line(0, 0, 3000), m}}
		if gaps := d.SequenceLifelineLabelGaps(false, nil); len(gaps) != 0 {
			t.Fatalf("straight stroke did not reach caption: %+v", gaps)
		}
		if gaps := d.SequenceLifelineLabelGaps(true, nil); len(gaps) != 1 || len(gaps[0]) != 1 {
			t.Fatalf("sketch stroke can bow into the caption: %+v", gaps)
		}
	})

	t.Run("legacy", func(t *testing.T) {
		legacy := Diagram{Connections: []Connection{line(0, 0, 300)}, Shapes: []Shape{guard}}
		legacy.Connections[0].SequenceRole = ""
		if gaps := legacy.SequenceLifelineLabelGaps(false, nil); len(gaps) != 0 {
			t.Fatalf("unmarked route was masked: %+v", gaps)
		}
	})
}
