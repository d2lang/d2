// Set DEBUG_ASCII=1 environment variable to enable verbose ASCII rendering debug logs.
package d2ascii

import (
	"context"
	"log/slog"
	"math"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/d2lang/d2/d2renderers/d2ascii/asciicanvas"
	"github.com/d2lang/d2/d2renderers/d2ascii/asciiroute"
	"github.com/d2lang/d2/d2renderers/d2ascii/asciishapes"
	"github.com/d2lang/d2/d2renderers/d2ascii/charset"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/log"
)

const (
	defaultFontWidth  = 9.75
	defaultFontHeight = 18.0
	defaultScale      = 1.0
)

type ASCIIartist struct {
	canvas  *asciicanvas.Canvas
	FW      float64
	FH      float64
	chars   charset.Set
	SCALE   float64
	diagram d2target.Diagram
	ctx     context.Context
}
type RenderOpts struct {
	Scale   *float64
	Charset charset.Type
}

type Point = asciiroute.Point

type Boundary = asciiroute.Boundary

// NewBoundary constructs an ASCII routing boundary.
//
// Deprecated: use asciiroute.NewBoundary. This forwarding function will be
// removed after one compatibility release.
func NewBoundary(tl, br Point) *Boundary {
	return asciiroute.NewBoundary(tl, br)
}

func (a *ASCIIartist) GetBoundary(s d2target.Shape) (Point, Point) {

	// For multiple shapes, expand boundary to match the expanded rendering
	posX := float64(s.Pos.X)
	posY := float64(s.Pos.Y)
	width := float64(s.Width)
	height := float64(s.Height)

	if s.Multiple {
		posX -= d2target.MULTIPLE_OFFSET   // Move left to include shadow area
		width += d2target.MULTIPLE_OFFSET  // Include shadow width
		height += d2target.MULTIPLE_OFFSET // Include shadow height
	}

	// Use the same calibration logic as the drawing functions
	shapeCtx := &asciishapes.Context{
		Canvas: a.canvas,
		Chars:  a.chars,
		FW:     a.FW,
		FH:     a.FH,
		Scale:  a.SCALE,
		Ctx:    a.ctx,
	}
	x1, y1, wC, hC := shapeCtx.Calibrate(posX, posY, width, height)

	// Apply the same width adjustments as the drawing code
	preserveWidth := hasConnectionsAtRightEdge(s, a.diagram.Connections, a.FW)
	preserveHeight := hasConnectionsAtTopEdge(s, a.diagram.Connections, a.FH)
	if preserveWidth && s.Label != "" {
		availableSpace := wC - utf8.RuneCountInString(s.Label)
		if availableSpace >= asciishapes.MinLabelPadding && availableSpace%2 == 1 {
			// Adjust the original width before recalibrating
			width += float64(int(a.FW / a.SCALE))
			x1, y1, wC, hC = shapeCtx.Calibrate(posX, posY, width, height)
		}
	}

	// Apply the same height adjustments as DrawRect for labels
	if s.Label != "" && hC%2 == 0 && !preserveHeight {
		if hC > 2 {
			hC--
			y1++
		} else {
			hC++
		}
	}

	// Apply the same width adjustments as DrawRect for labels
	wC = asciishapes.AdjustWidthForLabel(shapeCtx, posX, posY, width, height, wC, s.Label)

	x2, y2 := x1+wC, y1+hC

	return Point{X: x1, Y: y1}, Point{X: x2, Y: y2}
}

func (a *ASCIIartist) GetCanvas() *asciicanvas.Canvas { return a.canvas }
func (a *ASCIIartist) GetChars() charset.Set          { return a.chars }
func (a *ASCIIartist) GetDiagram() *d2target.Diagram  { return &a.diagram }
func (a *ASCIIartist) GetFontWidth() float64          { return a.FW }
func (a *ASCIIartist) GetFontHeight() float64         { return a.FH }
func (a *ASCIIartist) GetScale() float64              { return a.SCALE }
func (a *ASCIIartist) GetContext() context.Context    { return a.ctx }
func (a *ASCIIartist) GetBoundaryForShape(s d2target.Shape) (asciiroute.Point, asciiroute.Point) {
	p1, p2 := a.GetBoundary(s)
	return asciiroute.Point{X: p1.X, Y: p1.Y}, asciiroute.Point{X: p2.X, Y: p2.Y}
}
func (a *ASCIIartist) CalibrateXY(x, y float64) (float64, float64) {
	return a.calibrateXY(x, y)
}

func NewASCIIartist() *ASCIIartist {
	artist := &ASCIIartist{
		FW:      defaultFontWidth,
		FH:      defaultFontHeight,
		SCALE:   defaultScale,
		chars:   charset.New(charset.Unicode),
		diagram: *d2target.NewDiagram(),
	}

	return artist
}

func (a *ASCIIartist) calculateExtendedBounds(diagram *d2target.Diagram) (tl, br d2target.Point) {
	tl, br = diagram.NestedBoundingBox()

	for _, conn := range diagram.Connections {
		if conn.Label != "" && len(conn.Route) > 1 {
			maxDiff := 0.0
			bestX := 0.0
			for i := 0; i < len(conn.Route)-1; i++ {
				diffY := math.Abs(conn.Route[i].Y - conn.Route[i+1].Y)
				diffX := math.Abs(conn.Route[i].X - conn.Route[i+1].X)
				diff := math.Max(diffY, diffX)
				if diff > maxDiff {
					maxDiff = diff
					bestX = conn.Route[i].X
					if diff == diffX {
						bestX = conn.Route[i].X + (math.Copysign(1, conn.Route[i+1].X-conn.Route[i].X) * diff / 2)
					}
				}
			}
			labelX := bestX - float64(utf8.RuneCountInString(conn.Label))/2*a.FW
			labelX2 := bestX + float64(utf8.RuneCountInString(conn.Label))/2*a.FW
			midY := (conn.Route[0].Y + conn.Route[len(conn.Route)-1].Y) / 2
			labelY := midY - a.FH
			labelY2 := midY + a.FH
			if int(labelX) < tl.X {
				tl.X = int(labelX)
			}
			if int(labelX2) > br.X {
				br.X = int(labelX2)
			}
			if int(labelY) < tl.Y {
				tl.Y = int(labelY)
			}
			if int(labelY2) > br.Y {
				br.Y = int(labelY2)
			}
		}

		if conn.DstLabel != nil && len(conn.Route) > 0 {
			lastRoute := conn.Route[len(conn.Route)-1]
			labelX := lastRoute.X - float64(utf8.RuneCountInString(conn.DstLabel.Label))*a.FW
			labelX2 := lastRoute.X + float64(utf8.RuneCountInString(conn.DstLabel.Label))*a.FW
			labelY := lastRoute.Y - a.FH
			labelY2 := lastRoute.Y + a.FH
			if int(labelX) < tl.X {
				tl.X = int(labelX)
			}
			if int(labelX2) > br.X {
				br.X = int(labelX2)
			}
			if int(labelY) < tl.Y {
				tl.Y = int(labelY)
			}
			if int(labelY2) > br.Y {
				br.Y = int(labelY2)
			}
		}

		if conn.SrcLabel != nil && len(conn.Route) > 0 {
			firstRoute := conn.Route[0]
			labelX := firstRoute.X - float64(utf8.RuneCountInString(conn.SrcLabel.Label))*a.FW
			labelX2 := firstRoute.X + float64(utf8.RuneCountInString(conn.SrcLabel.Label))*a.FW
			labelY := firstRoute.Y - a.FH
			labelY2 := firstRoute.Y + a.FH
			if int(labelX) < tl.X {
				tl.X = int(labelX)
			}
			if int(labelX2) > br.X {
				br.X = int(labelX2)
			}
			if int(labelY) < tl.Y {
				tl.Y = int(labelY)
			}
			if int(labelY2) > br.Y {
				br.Y = int(labelY2)
			}
		}
	}

	return tl, br
}

func (a *ASCIIartist) Render(ctx context.Context, diagram *d2target.Diagram, opts *RenderOpts) ([]byte, error) {
	if opts == nil {
		opts = &RenderOpts{}
	}

	if os.Getenv("DEBUG_ASCII") == "" {
		ctx = log.Leveled(ctx, slog.LevelInfo)
	}
	a.ctx = ctx
	chars := a.chars
	if opts.Charset == charset.ASCII {
		chars = charset.New(charset.ASCII)
	} else if opts.Charset == charset.Unicode {
		chars = charset.New(charset.Unicode)
	}
	originalChars := a.chars
	a.chars = chars
	defer func() {
		a.chars = originalChars
	}()
	xOffset := 0
	yOffset := 0
	a.diagram = *diagram
	tl, br := a.calculateExtendedBounds(diagram)
	if tl.X < 0 {
		xOffset = -tl.X
		br.X += -tl.X
		tl.X = 0
	}
	if tl.Y < 0 {
		yOffset = -tl.Y
		br.Y += -tl.Y
		tl.Y = 0
	}
	w := int(math.Ceil(float64(br.X - tl.X)))
	h := int(math.Ceil(float64(br.Y - tl.Y)))

	w = int(math.Round((float64(w) / a.FW) * a.SCALE))
	h = int(math.Round((float64(h) / a.FH) * a.SCALE))

	maxLabelLen := 0
	for _, shape := range diagram.Shapes {
		if utf8.RuneCountInString(shape.Label) > maxLabelLen {
			maxLabelLen = utf8.RuneCountInString(shape.Label)
		}
	}
	padding := maxLabelLen + asciishapes.MinLabelPadding
	log.Debug(ctx, "canvas setup", slog.Int("maxLabelLen", maxLabelLen), slog.Int("padding", padding), slog.Int("width", w+padding+1), slog.Int("height", h+padding+1))

	a.canvas = asciicanvas.New(w+padding+1, h+padding+1)

	// Lifelines are behind frame borders, actors and activations. Their
	// routes already contain the gaps for notes and repeated headers.
	for _, conn := range diagram.Connections {
		if conn.SequenceRole == "lifeline" && conn.Opacity > 0 && len(conn.Route) == 2 {
			x, start := a.calibrateXY(conn.Route[0].X+float64(xOffset), conn.Route[0].Y+float64(yOffset))
			_, end := a.calibrateXY(conn.Route[1].X+float64(xOffset), conn.Route[1].Y+float64(yOffset))
			for y := int(start); y <= int(end); y++ {
				a.canvas.Set(int(x), y, a.chars.Vertical())
			}
		}
	}
	log.Debug(ctx, "processing shapes", slog.Int("count", len(diagram.Shapes)), slog.Int("xOffset", xOffset), slog.Int("yOffset", yOffset))
	for i, shape := range diagram.Shapes {
		if shape.SequenceRole == "branch" || (shape.SequenceRole != "" && shape.Opacity == 0) {
			continue
		}
		// The text grid draws the frame caption at its layout anchor after the
		// routes. The generic rectangle centers it on top of the first guard.
		if shape.SequenceRole == "group" {
			shape.Label = ""
		}
		log.Debug(ctx, "processing shape", slog.Int("index", i), slog.String("id", shape.ID), slog.String("type", shape.Type), slog.Float64("x", float64(shape.Pos.X)), slog.Float64("y", float64(shape.Pos.Y)), slog.Float64("width", float64(shape.Width)), slog.Float64("height", float64(shape.Height)))

		originalX, originalY := shape.Pos.X, shape.Pos.Y
		adjustedX := shape.Pos.X + xOffset
		adjustedY := shape.Pos.Y + yOffset
		log.Debug(ctx, "position adjusted", slog.Float64("originalX", float64(originalX)), slog.Float64("originalY", float64(originalY)), slog.Int("newX", adjustedX), slog.Int("newY", adjustedY))

		preserveWidth := hasConnectionsAtRightEdge(shape, diagram.Connections, a.FW)
		preserveHeight := hasConnectionsAtTopEdge(shape, diagram.Connections, a.FH)
		log.Debug(ctx, "edge connections", slog.Bool("preserveWidth", preserveWidth))

		shapeCtx := &asciishapes.Context{
			Canvas: a.canvas,
			Chars:  a.chars,
			FW:     a.FW,
			FH:     a.FH,
			Scale:  a.SCALE,
			Ctx:    ctx,
		}

		originalWidth := shape.Width
		adjustedWidth := shape.Width
		if preserveWidth && shape.Label != "" {
			wC := int(math.Round((float64(shape.Width) / a.FW) * a.SCALE))
			availableSpace := wC - utf8.RuneCountInString(shape.Label)
			log.Debug(ctx, "width preservation check", slog.Int("calibrated", wC), slog.Int("labelChars", utf8.RuneCountInString(shape.Label)), slog.Int("available", availableSpace))
			if availableSpace >= asciishapes.MinLabelPadding && availableSpace%2 == 1 {
				adjustedWidth += int(a.FW / a.SCALE)
				log.Debug(ctx, "width adjusted", slog.Int("originalWidth", originalWidth), slog.Int("newWidth", adjustedWidth))
			}
		}

		// For multiple shapes, expand to fill the entire space that would be occupied by the multiple effect
		drawX := float64(adjustedX)
		drawY := float64(adjustedY)
		drawWidth := float64(adjustedWidth)
		drawHeight := float64(shape.Height)

		if shape.Multiple {
			log.Debug(ctx, "multiple shape adjustments", slog.Int("offset", d2target.MULTIPLE_OFFSET))
			// Move position to top-left of total occupied area (shadow extends left and down)
			drawX -= d2target.MULTIPLE_OFFSET // Move left to include shadow area
			// Y stays the same since shadow goes down, not up

			// Expand size to fill entire multiple effect area
			drawWidth += d2target.MULTIPLE_OFFSET  // Include shadow width
			drawHeight += d2target.MULTIPLE_OFFSET // Include shadow height
			log.Debug(ctx, "multiple dimensions", slog.Float64("origX", float64(shape.Pos.X)), slog.Float64("origY", float64(shape.Pos.Y)), slog.Float64("origW", float64(shape.Width)), slog.Float64("origH", float64(shape.Height)), slog.Float64("drawX", drawX), slog.Float64("drawY", drawY), slog.Float64("drawW", drawWidth), slog.Float64("drawH", drawHeight))
		}

		log.Debug(ctx, "final draw parameters", slog.Float64("x", drawX), slog.Float64("y", drawY), slog.Float64("width", drawWidth), slog.Float64("height", drawHeight), slog.String("label", shape.Label))

		log.Debug(ctx, "drawing shape", slog.String("type", shape.Type))
		switch shape.Type {
		case d2target.ShapeRectangle:
			asciishapes.DrawRect(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition, "", preserveHeight)
		case d2target.ShapeSquare:
			asciishapes.DrawRect(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition, "", preserveHeight)
		case d2target.ShapePage:
			asciishapes.DrawPage(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeHexagon:
			asciishapes.DrawHex(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapePerson:
			asciishapes.DrawPerson(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeStoredData:
			asciishapes.DrawStoredData(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeCylinder:
			asciishapes.DrawCylinder(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapePackage:
			asciishapes.DrawPackage(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeParallelogram:
			asciishapes.DrawParallelogram(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeQueue:
			asciishapes.DrawQueue(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeStep:
			asciishapes.DrawStep(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeCallout:
			asciishapes.DrawCallout(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeDocument:
			asciishapes.DrawDocument(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeDiamond:
			asciishapes.DrawDiamond(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition)
		case d2target.ShapeClass:
			asciishapes.DrawClass(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape)
		case d2target.ShapeSQLTable:
			asciishapes.DrawSQLTable(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape)
		default:
			symbol := ""
			switch shape.Type {
			case d2target.ShapeCloud:
				symbol = a.chars.Cloud()
			case d2target.ShapeCircle:
				symbol = a.chars.Circle()
			case d2target.ShapeOval:
				symbol = a.chars.Oval()
			default:
				symbol = ""
			}
			asciishapes.DrawRect(shapeCtx, drawX, drawY, drawWidth, drawHeight, shape.Label, shape.LabelPosition, symbol, preserveHeight)
		}
	}
	// Paint arrowless guides before messages, then place sequence captions on
	// top so spaces and punctuation remain legible where a lifeline crosses.
	for _, arrows := range []bool{false, true} {
		for _, conn := range diagram.Connections {
			hasArrow := conn.DstArrow != d2target.NoArrowhead || conn.SrcArrow != d2target.NoArrowhead
			if hasArrow != arrows || conn.SequenceRole == "lifeline" || conn.SequenceRole != "" && conn.Opacity == 0 {
				continue
			}
			route := make([]*geo.Point, len(conn.Route))
			for i, point := range conn.Route {
				route[i] = geo.NewPoint(point.X+float64(xOffset), point.Y+float64(yOffset))
			}
			conn.Route = route
			if conn.SequenceRole == "divider" && len(route) == 2 {
				// The endpoints describe a frame-wide separator. Snapping it
				// to the small guard shape would collapse its width.
				left, y := a.calibrateXY(route[0].X, route[0].Y)
				right, _ := a.calibrateXY(route[1].X, route[1].Y)
				for x := int(left) + 1; x < int(right); x += 2 {
					a.canvas.Set(x, int(y), a.chars.Hyphen())
				}
				continue
			}
			if conn.SequenceRole == "message" {
				conn.Label = ""
			}
			asciiroute.DrawRoute(a, conn)
		}
	}
	for _, s := range diagram.Shapes {
		if (s.SequenceRole == "branch" || s.SequenceRole == "group") && s.Opacity > 0 && s.Label != "" && color.PaintVisibleThroughout(s.GetFontColor()) {
			pos := label.FromString(s.LabelPosition)
			at := s.LabelTopLeft(pos, s.LabelBox(pos, s.Geometry()))
			if at == nil {
				continue
			}
			x, y := a.sequenceCaptionCell(s.Text, at, xOffset, yOffset)
			if strings.HasPrefix(s.LabelPosition, "INSIDE_TOP_") {
				_, border := a.calibrateXY(0, float64(s.Pos.Y+yOffset))
				y = max(y, int(border)+1)
			}
			if strings.HasSuffix(s.LabelPosition, "_LEFT") {
				x = int(math.Ceil((at.X + float64(xOffset)) / a.FW * a.SCALE))
			}
			a.canvas.DrawLabel(x, y, s.Label)
		}
	}
	for _, c := range diagram.Connections {
		if c.SequenceRole != "message" || c.Opacity == 0 || c.Label == "" || !color.PaintVisibleThroughout(c.GetFontColor()) || len(c.Route) < 2 {
			continue
		}
		at := c.GetLabelTopLeft()
		if at == nil {
			continue
		}
		x, y := a.sequenceCaptionCell(c.Text, at, xOffset, yOffset)
		// Preserve the chosen side of the wire even when the text and wire
		// round into the same terminal row. This also handles self-calls.
		first, last := c.Route[0].Y, c.Route[len(c.Route)-1].Y
		_, top := a.calibrateXY(0, min(first, last)+float64(yOffset))
		_, bottom := a.calibrateXY(0, max(first, last)+float64(yOffset))
		if at.Y+float64(c.LabelHeight) <= min(first, last) {
			y = min(y, int(top)-strings.Count(c.Label, "\n")-1)
		} else if at.Y >= max(first, last) {
			y = max(y, int(bottom)+1)
		}
		// Keep arrowheads if the monospaced label is longer than its
		// proportional counterpart and cannot fit along the shaft.
		if a.sequenceCaptionOverlapsArrow(x, y, c.Label) {
			a.canvas.DrawLabelAbove(x, y, c.Label)
		} else {
			a.canvas.DrawLabel(x, y, c.Label)
		}
	}
	return a.canvas.ToByteArray(a.chars), nil
}

// Center the terminal glyphs in the same measured text box as the graphical
// exports. A fixed-width cell and a proportional SVG glyph have different widths.
func (a *ASCIIartist) sequenceCaptionCell(text d2target.Text, topLeft *geo.Point, xOffset, yOffset int) (int, int) {
	x, y := a.calibrateXY(topLeft.X+float64(xOffset)+float64(text.LabelWidth)/2, topLeft.Y+float64(yOffset)+float64(text.LabelHeight)/2)
	lines := strings.Split(text.Label, "\n")
	width := 0
	for _, line := range lines {
		width = max(width, utf8.RuneCountInString(line))
	}
	return int(x) - width/2, int(y) - len(lines)/2
}

func (a *ASCIIartist) sequenceCaptionOverlapsArrow(x, y int, text string) bool {
	for i, line := range strings.Split(text, "\n") {
		for j := range utf8.RuneCountInString(line) {
			switch a.canvas.Get(x+j, y+i) {
			case a.chars.ArrowLeft(), a.chars.ArrowRight(), a.chars.ArrowUp(), a.chars.ArrowDown():
				return true
			}
		}
	}
	return false
}

func (a *ASCIIartist) calibrateXY(x, y float64) (float64, float64) {
	xC := float64(math.Round((x / a.FW) * a.SCALE))
	yC := float64(math.Round((y / a.FH) * a.SCALE))
	return xC, yC
}

func hasConnectionsAtRightEdge(shape d2target.Shape, connections []d2target.Connection, fontWidth float64) bool {
	shapeRight := float64(shape.Pos.X + shape.Width)
	shapeTop := float64(shape.Pos.Y)
	shapeBottom := float64(shape.Pos.Y + shape.Height)

	for _, conn := range connections {
		if len(conn.Route) == 0 {
			continue
		}

		firstPoint := conn.Route[0]
		lastPoint := conn.Route[len(conn.Route)-1]

		tolerance := fontWidth / 2

		if math.Abs(firstPoint.X-shapeRight) < tolerance &&
			firstPoint.Y >= shapeTop && firstPoint.Y <= shapeBottom {
			return true
		}

		if math.Abs(lastPoint.X-shapeRight) < tolerance &&
			lastPoint.Y >= shapeTop && lastPoint.Y <= shapeBottom {
			return true
		}
	}

	return false
}

func hasConnectionsAtTopEdge(shape d2target.Shape, connections []d2target.Connection, fontHeight float64) bool {
	shapeTop := float64(shape.Pos.Y)
	shapeLeft := float64(shape.Pos.X)
	shapeRight := float64(shape.Pos.X + shape.Width)

	for _, conn := range connections {
		if len(conn.Route) < 2 {
			continue
		}

		// Check if route has horizontal segments connecting to top edge
		for i := 0; i < len(conn.Route)-1; i++ {
			p1 := conn.Route[i]
			p2 := conn.Route[i+1]

			// Check if this is a horizontal segment
			if math.Abs(p1.Y-p2.Y) < 0.1 {
				segmentY := p1.Y
				segmentLeft := math.Min(p1.X, p2.X)
				segmentRight := math.Max(p1.X, p2.X)

				tolerance := fontHeight

				// Check if horizontal segment connects to shape's top edge
				if math.Abs(segmentY-shapeTop) < tolerance &&
					segmentRight >= shapeLeft && segmentLeft <= shapeRight {
					return true
				}
			}
		}
	}

	return false
}
