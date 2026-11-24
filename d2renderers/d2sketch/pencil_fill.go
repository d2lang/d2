package d2sketch

import (
	"math"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/svg"
)

// PencilFill is one sampled pass of diagonal pencil strokes, in shape-local
// coordinates. Both exporters use these same strokes; the shape supplies the
// silhouette. Each quadrilateral is ordered around its perimeter.
type PencilFill struct {
	Paper, Ink      string
	GraphiteOpacity float64
	Bands           [][4]d2scene.Point
}

func SamplePencilFill(s d2target.Shape) *PencilFill {
	t := s.SketchFillTransition
	if t == nil || s.Width <= 0 || s.Height <= 0 {
		return nil
	}
	p := t.Progress
	fill := &PencilFill{Paper: t.From, Ink: t.To}
	// Erasing exposes the surface underneath. Paint the inverse set of strokes
	// in the old color over clear paper so a transparent target really clears.
	if strings.EqualFold(t.To, "transparent") || strings.EqualFold(t.To, "none") {
		fill.Paper, fill.Ink = fill.Ink, fill.Paper
		p = 1 - p
	}
	if p <= 0 {
		return fill
	}
	if p >= 1 {
		fill.Paper = fill.Ink
		return fill
	}

	w, h := float64(s.Width), float64(s.Height)
	// A stroke follows x+y=constant. Cap the number per shape independently
	// of its authored dimensions. Neighboring pencil passes overlap in time.
	count := int(min(64., math.Ceil((w+h)/8)))
	step := (w + h) / float64(count)
	fill.Bands = make([][4]d2scene.Point, 0, count)
	// After the pencil has crossed the shape, close the gaps and let the
	// graphite settle into the exact destination fill and normal sketch texture.
	settle := max(0., (p-.72)/.28)
	settle = settle * settle * (3 - 2*settle)
	fill.GraphiteOpacity = .18 * (1 - settle)
	for i := 0; i < count; i++ {
		center := (float64(i) + .5) * step
		start := .70 * float64(i) / float64(max(1, count-1))
		length := min(1., max(0., (p-start)/.20))
		if length == 0 {
			break
		}
		half := step * (.19 + .32*settle)
		lo, hi := max(0., center-w)-step, min(h, center)+step
		if i%2 == 1 {
			lo, hi = hi, lo
		}
		end := lo + (hi-lo)*length
		band := [4]d2scene.Point{
			{X: center - lo - half, Y: lo}, {X: center - lo + half, Y: lo},
			{X: center - end + half, Y: end}, {X: center - end - half, Y: end},
		}
		if i%2 == 1 {
			band[0], band[2] = band[2], band[0]
		}
		fill.Bands = append(fill.Bands, band)
	}
	return fill
}

// A user-space tile gives compound shapes a single direction of travel. A
// cylinder's rim and walls must not each start a new set of pencil strokes.
func pencilSVGPaint(s d2target.Shape, diagramHash string, x, y int) (defs, paint, stroke string) {
	original, stroke := d2themes.ShapeTheme(s)
	fill := SamplePencilFill(s)
	if fill == nil {
		return "", original, stroke
	}
	if len(fill.Bands) == 0 {
		return "", fill.Paper, stroke
	}
	id := svg.LiteralID("pencil-" + diagramHash + "-" + svg.SVGID(s.ID))
	var path strings.Builder
	for _, band := range fill.Bands {
		for i, p := range band {
			if i == 0 {
				path.WriteByte('M')
			} else {
				path.WriteByte('L')
			}
			path.WriteString(svg.FormatFloat(p.X))
			path.WriteByte(' ')
			path.WriteString(svg.FormatFloat(p.Y))
		}
		path.WriteByte('Z')
	}
	background := d2themes.NewThemableElement("rect", nil)
	background.Width, background.Height = float64(s.Width), float64(s.Height)
	background.Fill = fill.Paper
	outlineID := id.String() + "-strokes"
	pencil := d2themes.NewThemableElement("use", nil)
	pencil.AddAttributes(svg.HrefAttributes("#" + outlineID)...)
	pencil.Fill = fill.Ink
	var out strings.Builder
	out.WriteString("<defs>")
	out.WriteString(svg.EmptyElement("path", svg.Attr("id", outlineID), svg.Attr("d", path.String())))
	out.WriteString(svg.OpenElement("pattern",
		svg.Attr("id", id.String()), svg.Attr("patternUnits", "userSpaceOnUse"),
		svg.IntAttr("x", x), svg.IntAttr("y", y),
		svg.IntAttr("width", s.Width), svg.IntAttr("height", s.Height),
		svg.Attr("viewBox", "0 0 "+strconv.Itoa(s.Width)+" "+strconv.Itoa(s.Height)),
	))
	out.WriteString(background.Render())
	out.WriteString(pencil.Render())
	if fill.GraphiteOpacity > 0 {
		pencil.Fill = color.N1
		pencil.AddAttributes(svg.FloatAttr("opacity", fill.GraphiteOpacity))
		out.WriteString(pencil.Render())
	}
	out.WriteString("</pattern></defs>")
	return out.String(), svg.LocalIRI(id), stroke
}

// TexturePaint selects the material for the existing streak overlay.
// Static sketch textures use the material produced by the element renderer
// (which also resolves gradients). A pencil pattern takes its texture from
// the underlying sampled color, rather than from its temporary paint-server ID.
func TexturePaint(s d2target.Shape, renderedFill string) string {
	if t := s.SketchFillTransition; t != nil {
		if t.Progress == 0 {
			return t.From
		}
		if t.Progress == 1 {
			return t.To
		}
		return s.Fill
	}
	return renderedFill
}

// MarkdownBackground keeps the viewport behind the glyphs clear while pencil
// strokes are visible. A label outside or across the border still needs its
// own backing. At either endpoint use the authored board's normal backing.
func MarkdownBackground(s d2target.Shape, sketch bool) string {
	if !sketch || s.SketchFillTransition == nil {
		return s.Fill
	}
	position := label.FromString(s.LabelPosition)
	p := s.SketchFillTransition.Progress
	if p > 0 && p < 1 && !position.IsOutside() && !position.IsBorder() {
		return ""
	}
	return TexturePaint(s, s.Fill)
}
