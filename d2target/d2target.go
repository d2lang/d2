package d2target

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
	"github.com/d2lang/d2/lib/svg"
	"github.com/d2lang/d2/lib/textmeasure"
)

const (
	DEFAULT_SHAPE_STROKE_WIDTH = 2

	DEFAULT_ICON_SIZE = 32
	MAX_ICON_SIZE     = 64

	SHADOW_SIZE_X    = 3
	SHADOW_SIZE_Y    = 5
	THREE_DEE_OFFSET = 15
	MULTIPLE_OFFSET  = 10

	INNER_BORDER_OFFSET = 5

	BG_COLOR = color.N7
	FG_COLOR = color.N1

	MIN_ARROWHEAD_STROKE_WIDTH = 2
	ARROWHEAD_PADDING          = 2.

	CONNECTION_ICON_LABEL_GAP = 8
)

var BorderOffset = geo.NewVector(5, 5)

type Config struct {
	AnimateProgress      *bool           `json:"animateProgress,omitempty"`
	AnimateProgressColor string          `json:"animateProgressColor,omitempty"`
	AnimateInterval      *int64          `json:"animateInterval,omitempty"` // Milliseconds per board.
	AnimateDuration      *int64          `json:"animateDuration,omitempty"` // Milliseconds for step transitions and linked layer zooms.
	Scale                *float64        `json:"scale,omitempty"`
	ForceAppendix        *bool           `json:"forceAppendix,omitempty"`
	Sketch               *bool           `json:"sketch"`
	ThemeID              *int64          `json:"themeID"`
	DarkThemeID          *int64          `json:"darkThemeID"`
	Pad                  *int64          `json:"pad"`
	Center               *bool           `json:"center"`
	LayoutEngine         *string         `json:"layoutEngine"`
	ThemeOverrides       *ThemeOverrides `json:"themeOverrides,omitempty"`
	DarkThemeOverrides   *ThemeOverrides `json:"darkThemeOverrides,omitempty"`
	// Data is a data structure for holding user-defined data
	// useful for layout engines that allow users to configure within source code
	Data map[string]interface{} `json:"data,omitempty"`
}

// ResolveAnimateInterval returns the explicit render setting, falling back to
// the source configuration. A nil result leaves the output format's default intact.
func (c *Config) ResolveAnimateInterval(override *int64) *int64 {
	if override != nil || c == nil {
		return override
	}
	return c.AnimateInterval
}

// ResolveAnimateProgress defaults to showing progress when a positive playback
// interval was configured. A format's automatic interval does not count.
func (c *Config) ResolveAnimateProgress(override *bool, hasInterval bool) bool {
	if override != nil {
		return *override
	}
	if c != nil && c.AnimateProgress != nil {
		return *c.AnimateProgress
	}
	return hasInterval
}

// ResolveAnimateProgressColor gives a nonempty render setting precedence over the source.
func (c *Config) ResolveAnimateProgressColor(override string) string {
	if override != "" || c == nil {
		return override
	}
	return c.AnimateProgressColor
}

// ValidateAnimateProgressColor permits an unset override or a solid, opaque
// color with the same meaning in SVG and raster exports.
func ValidateAnimateProgressColor(paint string) error {
	if paint == "" || color.ColorHexRegex.MatchString(paint) {
		return nil
	}
	name := strings.ToLower(paint)
	if name != "transparent" && name != "currentcolor" && go2.Contains(color.NamedColors, name) {
		return nil
	}
	return fmt.Errorf("expected an opaque named color or a hex color (#RGB or #RRGGBB), got %q", paint)
}

// ResolveForceAppendix preserves an explicit false when the source enables footnotes.
func (c *Config) ResolveForceAppendix(override *bool) *bool {
	if override != nil || c == nil {
		return override
	}
	return c.ForceAppendix
}

type ThemeOverrides struct {
	N1  *string `json:"n1"`
	N2  *string `json:"n2"`
	N3  *string `json:"n3"`
	N4  *string `json:"n4"`
	N5  *string `json:"n5"`
	N6  *string `json:"n6"`
	N7  *string `json:"n7"`
	B1  *string `json:"b1"`
	B2  *string `json:"b2"`
	B3  *string `json:"b3"`
	B4  *string `json:"b4"`
	B5  *string `json:"b5"`
	B6  *string `json:"b6"`
	AA2 *string `json:"aa2"`
	AA4 *string `json:"aa4"`
	AA5 *string `json:"aa5"`
	AB4 *string `json:"ab4"`
	AB5 *string `json:"ab5"`
}

// Limits shared by D2 source compilation and callers supplying sampled targets.
const (
	MaxTweenDurationMS = 600_000
	MaxTweenFrames     = 120
)

// TweenFrameCount samples at 25 fps up to the per-transition frame limit.
func TweenFrameCount(durationMS int) int {
	return min(MaxTweenFrames, (durationMS+39)/40)
}

type Diagram struct {
	Name   string  `json:"name"`
	Config *Config `json:"config,omitempty"`
	// See docs on the same field in d2graph to understand what it means.
	IsFolderOnly   bool                `json:"isFolderOnly"`
	Description    string              `json:"description,omitempty"`
	FontFamily     *d2fonts.FontFamily `json:"fontFamily,omitempty"`
	MonoFontFamily *d2fonts.FontFamily `json:"monoFontFamily,omitempty"`

	Shapes      []Shape      `json:"shapes"`
	Connections []Connection `json:"connections"`

	Root   Shape   `json:"root"`
	Legend *Legend `json:"legend,omitempty"`
	// Maybe Icon can be used as a watermark in the root shape

	Layers    []*Diagram `json:"layers,omitempty"`
	Scenarios []*Diagram `json:"scenarios,omitempty"`
	Steps     []*Diagram `json:"steps,omitempty"`

	// TweenFrames are samples of this board's incoming transition,
	// starting at time zero. The board itself supplies the exact endpoint.
	TweenFrames     []*Diagram `json:"tweenFrames,omitempty"`
	TweenDurationMS int        `json:"tweenDurationMS,omitempty"`
	// TweenFrameEndsMS holds exclusive sample ends for nonuniform playback.
	// The authored endpoint takes over when the final sample ends.
	// Omit it for uniformly spaced samples.
	TweenFrameEndsMS []int `json:"tweenFrameEndsMS,omitempty"`
	// GridMotion keeps a common coordinate system for raster playback as cells reflow.
	GridMotion bool `json:"gridMotion,omitempty"`
	// LayoutContinuity means the board shares layout coordinates with its
	// parent scenario base or the preceding step. Independent layers start
	// their own coordinate system.
	LayoutContinuity bool `json:"layoutContinuity,omitempty"`
	// Canvas is a box the board's bounds always include. An animation with
	// annotations gives every board and frame the same canvas, so the picture
	// keeps one size while notes come and go.
	Canvas *MotionBox `json:"canvas,omitempty"`
}

type Legend struct {
	Label       string       `json:"label,omitempty"`
	Shapes      []Shape      `json:"shapes,omitempty"`
	Connections []Connection `json:"connections,omitempty"`
}

// renderChildren includes sampled frames for hashing, font subsetting and
// bounding boxes. Navigation only exposes the authored boards.
func (d *Diagram) renderChildren() [4][]*Diagram {
	return [4][]*Diagram{d.Layers, d.Scenarios, d.Steps, d.TweenFrames}
}

func (d *Diagram) GetBoard(boardPath []string) *Diagram {
	board, _ := d.ResolveBoard(boardPath)
	return board
}

// ResolveBoard also returns the fully qualified path relative to d. A target
// may omit board namespaces, while a compiled shape link always includes them.
func (d *Diagram) ResolveBoard(path []string) (*Diagram, []string) {
	var resolved []string
	for len(path) > 0 && !(len(path) == 1 && d.Name == path[0]) {
		var next *Diagram
		var kind string
		consumed := 1
		switch path[0] {
		case "layers", "scenarios", "steps":
			if len(path) < 2 {
				return nil, nil
			}
			next, kind = d.namedChild(path[0], path[1])
			consumed = 2
		}
		if next == nil {
			next, kind = d.namedChild("", path[0])
			consumed = 1
		}
		if next == nil {
			return nil, nil
		}
		resolved = append(resolved, kind, next.Name)
		d, path = next, path[consumed:]
	}
	return d, resolved
}

func (d *Diagram) namedChild(kind, name string) (*Diagram, string) {
	for _, group := range [...]struct {
		kind   string
		boards []*Diagram
	}{{"layers", d.Layers}, {"scenarios", d.Scenarios}, {"steps", d.Steps}} {
		if kind == "" || kind == group.kind {
			for _, board := range group.boards {
				if board.Name == name {
					return board, group.kind
				}
			}
		}
	}
	return nil, ""
}

func (diagram Diagram) Bytes() ([]byte, error) {
	b1, err := marshalHashJSON(diagram.Shapes)
	if err != nil {
		return nil, err
	}
	b2, err := marshalHashJSON(diagram.Connections)
	if err != nil {
		return nil, err
	}
	b3, err := marshalHashJSON(diagram.Root)
	if err != nil {
		return nil, err
	}
	base := append(append(b1, b2...), b3...)

	if diagram.Config != nil {
		b, err := json.Marshal(diagram.Config)
		if err != nil {
			return nil, err
		}
		base = append(base, b...)
	}

	if diagram.TweenDurationMS != 0 {
		base = append(base, "tween-ms:"...)
		base = strconv.AppendInt(base, int64(diagram.TweenDurationMS), 10)
		for _, ms := range diagram.TweenFrameEndsMS {
			base = append(base, ',')
			base = strconv.AppendInt(base, int64(ms), 10)
		}
	}
	for _, group := range diagram.renderChildren() {
		for _, child := range group {
			frameBytes, err := child.Bytes()
			if err != nil {
				return nil, err
			}
			base = append(base, frameBytes...)
		}
	}
	return base, nil
}

// json.Marshal follows the declaration order of struct fields. Go 1.26
// reorganized net/url.URL, which otherwise changes diagram hashes (and every
// SVG identifier derived from them) without changing the diagram. Keep URL
// values in the order used through Go 1.25 for stable hashes across toolchains.
// This is applied only to Shape, Connection, and Root JSON used by Bytes.
func marshalHashJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// The only URL values in these types are icon fields. json.Marshal emits
	// compact field names and escapes quotation marks inside string values, so
	// this marker cannot be hidden by whitespace or matched inside label text.
	// Most diagrams have no icons; avoid decoding their entire JSON again.
	if !bytes.Contains(b, []byte(`"icon":{`)) {
		return b, nil
	}
	return stabilizeHashURLs(b)
}

var legacyURLFieldOrder = [...]string{
	"Scheme",
	"Opaque",
	"User",
	"Host",
	"Path",
	"RawPath",
	"OmitHost",
	"ForceQuery",
	"RawQuery",
	"Fragment",
	"RawFragment",
}

func stabilizeHashURLs(b []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	var out bytes.Buffer
	lastWrite := 0

	var walkValue func() error
	walkValue = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}

		switch delim {
		case '{':
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("decode diagram hash JSON: object key has type %T", keyToken)
				}
				if key != "icon" {
					if err := walkValue(); err != nil {
						return err
					}
					continue
				}

				valueStart, err := hashJSONValueStart(b, int(dec.InputOffset()))
				if err != nil {
					return err
				}
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return fmt.Errorf("decode icon URL for stable diagram hash: %w", err)
				}
				if bytes.Equal(raw, []byte("null")) {
					continue
				}

				stable, err := stableHashURL(raw)
				if err != nil {
					return err
				}
				valueEnd := int(dec.InputOffset())
				if valueEnd < valueStart || valueEnd > len(b) {
					return fmt.Errorf("decode icon URL for stable diagram hash: invalid offsets %d:%d", valueStart, valueEnd)
				}
				out.Write(b[lastWrite:valueStart])
				out.Write(stable)
				lastWrite = valueEnd
			}
			closeToken, err := dec.Token()
			if err != nil {
				return err
			}
			if closeToken != json.Delim('}') {
				return fmt.Errorf("decode diagram hash JSON: got closing token %v, want }", closeToken)
			}
		case '[':
			for dec.More() {
				if err := walkValue(); err != nil {
					return err
				}
			}
			closeToken, err := dec.Token()
			if err != nil {
				return err
			}
			if closeToken != json.Delim(']') {
				return fmt.Errorf("decode diagram hash JSON: got closing token %v, want ]", closeToken)
			}
		default:
			return fmt.Errorf("decode diagram hash JSON: unexpected delimiter %q", delim)
		}
		return nil
	}

	if err := walkValue(); err != nil {
		return nil, err
	}
	out.Write(b[lastWrite:])
	return out.Bytes(), nil
}

func hashJSONValueStart(b []byte, keyEnd int) (int, error) {
	i := keyEnd
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	if i < len(b) && b[i] == ':' {
		i++
	}
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	if i >= len(b) {
		return 0, fmt.Errorf("decode icon URL for stable diagram hash: missing value")
	}
	return i, nil
}

func stableHashURL(raw json.RawMessage) ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(legacyURLFieldOrder))
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("decode icon URL for stable diagram hash: %w", err)
	}
	if len(fields) != len(legacyURLFieldOrder) {
		return nil, fmt.Errorf("decode icon URL for stable diagram hash: got %d fields, want %d", len(fields), len(legacyURLFieldOrder))
	}

	var out bytes.Buffer
	out.WriteByte('{')
	for i, name := range legacyURLFieldOrder {
		value, ok := fields[name]
		if !ok {
			return nil, fmt.Errorf("decode icon URL for stable diagram hash: missing field %q", name)
		}
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('"')
		out.WriteString(name)
		out.WriteString(`":`)
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func (diagram Diagram) HasShape(condition func(Shape) bool) bool {
	for _, group := range diagram.renderChildren() {
		for _, child := range group {
			if child.HasShape(condition) {
				return true
			}
		}
	}
	for _, s := range diagram.Shapes {
		if condition(s) {
			return true
		}
	}
	return false
}

func (diagram Diagram) HashID(salt *string) (string, error) {
	bytes, err := diagram.Bytes()
	if err != nil {
		return "", err
	}
	h := fnv.New32a()
	h.Write(bytes)
	if salt != nil {
		h.Write([]byte(*salt))
	}
	// CSS names can't start with numbers, so prepend a little something
	return fmt.Sprintf("d2-%d", h.Sum32()), nil
}

func (diagram Diagram) NestedBoundingBox() (topLeft, bottomRight Point) {
	tl, br := diagram.BoundingBox()
	for _, group := range diagram.renderChildren() {
		for _, child := range group {
			tl2, br2 := child.NestedBoundingBox()
			tl.X = go2.Min(tl.X, tl2.X)
			tl.Y = go2.Min(tl.Y, tl2.Y)
			br.X = go2.Max(br.X, br2.X)
			br.Y = go2.Max(br.Y, br2.Y)
		}
	}
	return tl, br
}

func (diagram Diagram) BoundingBox() (topLeft, bottomRight Point) {
	tl, br := diagram.InkBoundingBox()
	if caption := diagram.CaptionBox(tl, br); caption != nil {
		tl.X = min(tl.X, int(math.Floor(caption.TopLeft.X)))
		tl.Y = min(tl.Y, int(math.Floor(caption.TopLeft.Y)))
		br.X = max(br.X, int(math.Ceil(caption.TopLeft.X+caption.Width)))
		br.Y = max(br.Y, int(math.Ceil(caption.TopLeft.Y+caption.Height)))
	}
	if c := diagram.Canvas; c != nil {
		tl.X, tl.Y = min(tl.X, c.X), min(tl.Y, c.Y)
		br.X, br.Y = max(br.X, c.X+c.Width), max(br.Y, c.Y+c.Height)
	}
	return tl, br
}

// InkBoundingBox bounds everything drawn on the board except its caption,
// which is placed under this box.
func (diagram Diagram) InkBoundingBox() (topLeft, bottomRight Point) {
	diagram = *diagram.WithIconLabelInsets()
	if len(diagram.Shapes) == 0 {
		return Point{0, 0}, Point{0, 0}
	}
	x1 := int(math.MaxInt32)
	y1 := int(math.MaxInt32)
	x2 := int(math.MinInt32)
	y2 := int(math.MinInt32)

	for _, targetShape := range diagram.Shapes {
		stroke := StrokeWidthExtent(targetShape.StrokeWidth, targetShape.StrokeWidthOffset)
		halfStroke := stroke/2 + stroke%2
		x1 = go2.Min(x1, targetShape.Pos.X-halfStroke)
		y1 = go2.Min(y1, targetShape.Pos.Y-halfStroke)
		x2 = go2.Max(x2, targetShape.Pos.X+targetShape.Width+halfStroke)
		y2 = go2.Max(y2, targetShape.Pos.Y+targetShape.Height+halfStroke)

		if targetShape.Type == ShapeC4Person {
			headRadius := int(float64(targetShape.Width) * 0.22)
			headCenterY := int(float64(targetShape.Height) * 0.18)
			headTop := targetShape.Pos.Y + headCenterY - headRadius
			y1 = go2.Min(y1, headTop-stroke)
		}

		if targetShape.Tooltip != "" || targetShape.Link != "" {
			if targetShape.TooltipPosition != "" {
				tooltipMinX, tooltipMinY, tooltipMaxX, tooltipMaxY := calculateTooltipBounds(targetShape, diagram.FontFamily, diagram.MonoFontFamily)
				x1 = go2.Min(x1, tooltipMinX)
				y1 = go2.Min(y1, tooltipMinY)
				x2 = go2.Max(x2, tooltipMaxX)
				y2 = go2.Max(y2, tooltipMaxY)
			} else {
				y1 = go2.Min(y1, targetShape.Pos.Y-stroke-16)
				x2 = go2.Max(x2, targetShape.Pos.X+stroke+targetShape.Width+16)
			}
		}
		if targetShape.Shadow {
			y2 = go2.Max(y2, targetShape.Pos.Y+targetShape.Height+halfStroke+SHADOW_SIZE_Y)
			x2 = go2.Max(x2, targetShape.Pos.X+targetShape.Width+halfStroke+SHADOW_SIZE_X)
		}

		if targetShape.ThreeDee {
			offsetY := THREE_DEE_OFFSET
			if targetShape.Type == ShapeHexagon {
				offsetY /= 2
			}
			y1 = go2.Min(y1, targetShape.Pos.Y-offsetY-stroke)
			x2 = go2.Max(x2, targetShape.Pos.X+THREE_DEE_OFFSET+targetShape.Width+stroke)
		}
		if targetShape.Multiple {
			y1 = go2.Min(y1, targetShape.Pos.Y-MULTIPLE_OFFSET-stroke)
			x2 = go2.Max(x2, targetShape.Pos.X+MULTIPLE_OFFSET+targetShape.Width+stroke)
		}

		iconPosition := label.FromString(targetShape.IconPosition)
		if targetShape.Icon != nil && targetShape.Type != ShapeImage && (iconPosition.IsOutside() || iconPosition.IsBorder()) {
			box := geo.NewBox(geo.NewPoint(float64(targetShape.Pos.X), float64(targetShape.Pos.Y)), float64(targetShape.Width), float64(targetShape.Height))
			s := shape.NewShape(DSL_SHAPE_TO_SHAPE_TYPE[targetShape.Type], box)
			if targetShape.Type == ShapeCloud && targetShape.ContentAspectRatio != nil {
				s.SetInnerBoxAspectRatio(*targetShape.ContentAspectRatio)
			}
			icon := targetShape.IconBounds(s)
			x1 = min(x1, int(math.Floor(icon.TopLeft.X)))
			y1 = min(y1, int(math.Floor(icon.TopLeft.Y)))
			x2 = max(x2, int(math.Ceil(icon.TopLeft.X+icon.Width)))
			y2 = max(y2, int(math.Ceil(icon.TopLeft.Y+icon.Height)))
		}

		if targetShape.Label != "" {
			position := label.FromString(targetShape.LabelPosition)
			box := geo.NewBox(geo.NewPoint(float64(targetShape.Pos.X), float64(targetShape.Pos.Y)), float64(targetShape.Width), float64(targetShape.Height))
			if position.IsOutside() || position.IsBorder() {
				box = targetShape.OuterLabelBox(box)
			}
			labelTL := targetShape.LabelTopLeft(position, box)
			left, top := int(labelTL.X), int(labelTL.Y)
			right, bottom := left+targetShape.LabelWidth, top+targetShape.LabelHeight
			if position.IsBorder() {
				left, top = int(math.Floor(labelTL.X)), int(math.Floor(labelTL.Y))
				right = int(math.Ceil(labelTL.X + float64(targetShape.LabelWidth)))
				bottom = int(math.Ceil(labelTL.Y + float64(targetShape.LabelHeight)))
			}
			x1, y1 = min(x1, left), min(y1, top)
			x2, y2 = max(x2, right), max(y2, bottom)
		}
	}

	for _, connection := range diagram.Connections {
		stroke := StrokeWidthExtent(connection.StrokeWidth, connection.StrokeWidthOffset)
		halfStroke := stroke/2 + stroke%2
		if connection.IconAnimation != nil {
			// Reserve the entire trip so playback keeps the same viewport across emissions.
			halfStroke = max(halfStroke, connection.IconSize()/2)
		}
		for _, point := range connection.Route {
			x1 = go2.Min(x1, int(math.Floor(point.X))-halfStroke)
			y1 = go2.Min(y1, int(math.Floor(point.Y))-halfStroke)
			x2 = go2.Max(x2, int(math.Ceil(point.X))+halfStroke)
			y2 = go2.Max(y2, int(math.Ceil(point.Y))+halfStroke)
		}

		if connection.Label != "" {
			labelTL := connection.GetLabelTopLeft()
			x1 = go2.Min(x1, int(labelTL.X))
			y1 = go2.Min(y1, int(labelTL.Y))
			x2 = go2.Max(x2, int(labelTL.X)+connection.LabelWidth)
			y2 = go2.Max(y2, int(labelTL.Y)+connection.LabelHeight)
		}
		if icon := connection.GetIconPosition(); icon != nil {
			x1, y1 = min(x1, int(math.Floor(icon.X))), min(y1, int(math.Floor(icon.Y)))
			x2, y2 = max(x2, int(math.Ceil(icon.X))+connection.IconSize()), max(y2, int(math.Ceil(icon.Y))+connection.IconSize())
		}
		if connection.SrcLabel != nil && connection.SrcLabel.Label != "" {
			labelTL := connection.GetArrowheadLabelPosition(false)
			x1 = go2.Min(x1, int(labelTL.X))
			y1 = go2.Min(y1, int(labelTL.Y))
			x2 = go2.Max(x2, int(labelTL.X)+connection.SrcLabel.LabelWidth)
			y2 = go2.Max(y2, int(labelTL.Y)+connection.SrcLabel.LabelHeight)
		}
		if connection.DstLabel != nil && connection.DstLabel.Label != "" {
			labelTL := connection.GetArrowheadLabelPosition(true)
			x1 = go2.Min(x1, int(labelTL.X))
			y1 = go2.Min(y1, int(labelTL.Y))
			x2 = go2.Max(x2, int(labelTL.X)+connection.DstLabel.LabelWidth)
			y2 = go2.Max(y2, int(labelTL.Y)+connection.DstLabel.LabelHeight)
		}
	}

	// Annotations sit outside layout, so the viewport grows to hold them. A
	// frame that has drawn only part of a mark still reserves all of it.
	for _, mark := range diagram.annotationMarks(false) {
		if len(mark.Strokes) != 0 {
			mx1, my1, mx2, my2 := mark.Bounds()
			x1, y1 = min(x1, int(math.Floor(mx1))), min(y1, int(math.Floor(my1)))
			x2, y2 = max(x2, int(math.Ceil(mx2))), max(y2, int(math.Ceil(my2)))
		}
		if box := mark.LabelBox; box != nil {
			x1, y1 = min(x1, int(math.Floor(box.TopLeft.X))), min(y1, int(math.Floor(box.TopLeft.Y)))
			x2, y2 = max(x2, int(math.Ceil(box.TopLeft.X+box.Width))), max(y2, int(math.Ceil(box.TopLeft.Y+box.Height)))
		}
	}

	return Point{x1, y1}, Point{x2, y2}
}

func CalculateTooltipPosition(shapeX, shapeY, shapeWidth, shapeHeight float64, tooltipWidth, tooltipHeight int, position string) (x, y float64) {
	padding := 10.0

	switch position {
	case "top-left":
		x = shapeX
		y = shapeY - float64(tooltipHeight) - padding
	case "top-center":
		x = shapeX + shapeWidth/2 - float64(tooltipWidth)/2
		y = shapeY - float64(tooltipHeight) - padding
	case "top-right":
		x = shapeX + shapeWidth - float64(tooltipWidth)
		y = shapeY - float64(tooltipHeight) - padding
	case "center-left":
		x = shapeX - float64(tooltipWidth) - padding
		y = shapeY + shapeHeight/2 - float64(tooltipHeight)/2
	case "center-right":
		x = shapeX + shapeWidth + padding
		y = shapeY + shapeHeight/2 - float64(tooltipHeight)/2
	case "bottom-left":
		x = shapeX
		y = shapeY + shapeHeight + padding
	case "bottom-center":
		x = shapeX + shapeWidth/2 - float64(tooltipWidth)/2
		y = shapeY + shapeHeight + padding
	case "bottom-right":
		x = shapeX + shapeWidth - float64(tooltipWidth)
		y = shapeY + shapeHeight + padding
	default:
		x = shapeX + shapeWidth/2 - float64(tooltipWidth)/2
		y = shapeY - float64(tooltipHeight) - padding
	}

	return x, y
}

func calculateTooltipBounds(targetShape Shape, diagramFontFamily, diagramMonoFontFamily *d2fonts.FontFamily) (minX, minY, maxX, maxY int) {
	if targetShape.Tooltip == "" || targetShape.TooltipPosition == "" {
		return 0, 0, 0, 0
	}

	var tooltipWidth, tooltipHeight int

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		textLength := len(targetShape.Tooltip)
		tooltipWidth = go2.Min(textLength*8+20, 200)
		tooltipHeight = 30
	} else {
		fontFamily := diagramFontFamily
		if fontFamily == nil {
			family := d2fonts.SourceSansPro
			fontFamily = &family
		}
		monoFontFamily := diagramMonoFontFamily
		if monoFontFamily == nil {
			family := d2fonts.SourceCodePro
			monoFontFamily = &family
		}
		if strings.EqualFold(targetShape.FontFamily, "mono") {
			fontFamily = monoFontFamily
		} else if targetShape.FontFamily != "" && !strings.EqualFold(targetShape.FontFamily, "default") {
			if family, ok := d2fonts.D2_FONT_TO_FAMILY[strings.ToLower(targetShape.FontFamily)]; ok {
				fontFamily = &family
			}
		}

		width, height, err := textmeasure.MeasureMarkdown(targetShape.Tooltip, ruler, fontFamily, monoFontFamily, d2fonts.FONT_SIZE_M)
		if err != nil {
			textLength := len(targetShape.Tooltip)
			tooltipWidth = go2.Min(textLength*8+20, 200)
			tooltipHeight = 30
		} else {
			tooltipWidth = width + 20
			tooltipHeight = height + 20
		}
	}

	shapeX := float64(targetShape.Pos.X)
	shapeY := float64(targetShape.Pos.Y)
	shapeWidth := float64(targetShape.Width)
	shapeHeight := float64(targetShape.Height)

	x, y := CalculateTooltipPosition(shapeX, shapeY, shapeWidth, shapeHeight, tooltipWidth, tooltipHeight, targetShape.TooltipPosition)

	minX = int(math.Floor(x))
	minY = int(math.Floor(y))
	maxX = int(math.Ceil(x)) + tooltipWidth
	maxY = int(math.Ceil(y)) + tooltipHeight

	return minX, minY, maxX, maxY
}

func (diagram Diagram) GetNestedCorpus() string {
	var corpus strings.Builder
	diagram.appendNestedCorpus(&corpus)
	return corpus.String()
}

func (diagram Diagram) appendNestedCorpus(corpus *strings.Builder) {
	diagram.appendCorpus(corpus)
	for _, group := range diagram.renderChildren() {
		for _, child := range group {
			child.appendNestedCorpus(corpus)
		}
	}
}

func (diagram Diagram) GetCorpus() string {
	var corpus strings.Builder
	diagram.appendCorpus(&corpus)
	return corpus.String()
}

func (diagram Diagram) appendCorpus(corpus *strings.Builder) {
	appendixCount := 0
	if a := diagram.Root.Annotation; a != nil {
		corpus.WriteString(a.Label)
	}
	if a := diagram.Root.LeavingAnnotation; a != nil {
		corpus.WriteString(a.Label)
	}
	for _, s := range diagram.Shapes {
		corpus.WriteString(s.Label)
		if s.Annotation != nil {
			corpus.WriteString(s.Annotation.Label)
		}
		if s.LeavingAnnotation != nil {
			corpus.WriteString(s.LeavingAnnotation.Label)
		}
		if s.Tooltip != "" {
			corpus.WriteString(s.Tooltip)
			appendixCount++
			corpus.WriteString(strconv.Itoa(appendixCount))
		}
		if s.Link != "" {
			corpus.WriteString(s.Link)
			appendixCount++
			corpus.WriteString(strconv.Itoa(appendixCount))
		}
		corpus.WriteString(s.PrettyLink)
		if s.Type == ShapeClass {
			for _, cf := range s.Fields {
				corpus.WriteString(cf.Text(0).Text)
				corpus.WriteString(cf.VisibilityToken())
			}
			for _, cm := range s.Methods {
				corpus.WriteString(cm.Text(0).Text)
				corpus.WriteString(cm.VisibilityToken())
			}
		}
		if s.Type == ShapeSQLTable {
			for _, c := range s.Columns {
				for _, t := range c.Texts(0) {
					corpus.WriteString(t.Text)
				}
				corpus.WriteString(c.ConstraintAbbr())
			}
		}
	}
	for _, c := range diagram.Connections {
		corpus.WriteString(c.Label)
		if c.Annotation != nil {
			corpus.WriteString(c.Annotation.Label)
		}
		if c.LeavingAnnotation != nil {
			corpus.WriteString(c.LeavingAnnotation.Label)
		}
		if c.SrcLabel != nil {
			corpus.WriteString(c.SrcLabel.Label)
		}
		if c.DstLabel != nil {
			corpus.WriteString(c.DstLabel.Label)
		}
	}

	if diagram.Legend != nil {
		if diagram.Legend.Label != "" {
			corpus.WriteString(diagram.Legend.Label)
		} else {
			corpus.WriteString("Legend")
		}
		for _, s := range diagram.Legend.Shapes {
			corpus.WriteString(s.Label)
		}
		for _, c := range diagram.Legend.Connections {
			corpus.WriteString(c.Label)
		}
	}
}

func NewDiagram() *Diagram {
	return &Diagram{
		Root: Shape{
			Fill: BG_COLOR,
		},
	}
}

// MotionClip is the visible rectangle of an unfolding grid cell, in diagram coordinates.
type MotionClip struct {
	Point
	Width  int `json:"width"`
	Height int `json:"height"`
}

// MotionBox holds reference bounds in diagram coordinates during an animation.
type MotionBox = MotionClip

// SketchFillTransition describes the pencil shading on one sampled frame.
type SketchFillTransition struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Progress float64 `json:"progress"`
}

type Shape struct {
	ID   string `json:"id"`
	Type string `json:"type"`

	Classes []string `json:"classes,omitempty"`

	// Only intermediate grid frames carry a viewport, caption anchor or LaTeX scale.
	MotionScale        float64     `json:"motionScale,omitempty"`
	MotionClip         *MotionClip `json:"motionClip,omitempty"`
	MotionLabelTopLeft *Point      `json:"motionLabelTopLeft,omitempty"`
	// MotionLabelBox holds the authored body bounds for a stationary inside label.
	MotionLabelBox *MotionBox    `json:"motionLabelBox,omitempty"`
	RowReveal      *SQLRowReveal `json:"rowReveal,omitempty"`
	// OutlineProgress traces an incoming container frame in a transition sample.
	OutlineProgress *float64 `json:"outlineProgress,omitempty"`

	Pos    Point `json:"pos"`
	Width  int   `json:"width"`
	Height int   `json:"height"`

	Opacity     float64 `json:"opacity"`
	StrokeDash  float64 `json:"strokeDash"`
	StrokeWidth int     `json:"strokeWidth"`
	// Tween frames retain the subpixel difference from the integer layout width.
	StrokeWidthOffset float64 `json:"strokeWidthOffset,omitempty"`

	BorderRadius int `json:"borderRadius"`

	Fill                 string                `json:"fill"`
	FillPattern          string                `json:"fillPattern,omitempty"`
	SketchFillTransition *SketchFillTransition `json:"sketchFillTransition,omitempty"`
	Stroke               string                `json:"stroke"`

	Animated     bool `json:"animated"`
	Shadow       bool `json:"shadow"`
	ThreeDee     bool `json:"3d"`
	Multiple     bool `json:"multiple"`
	DoubleBorder bool `json:"double-border"`

	Tooltip          string   `json:"tooltip"`
	Link             string   `json:"link"`
	PrettyLink       string   `json:"prettyLink,omitempty"`
	Icon             *url.URL `json:"icon"`
	IconBorderRadius int      `json:"iconBorderRadius,omitempty"`
	IconPosition     string   `json:"iconPosition"`

	// Whether the shape should allow shapes behind it to bleed through
	// Currently just used for sequence diagram groups
	Blend bool `json:"blend"`

	Class
	SQLTable

	ContentAspectRatio *float64 `json:"contentAspectRatio,omitempty"`

	Text

	LabelPosition   string `json:"labelPosition,omitempty"`
	TooltipPosition string `json:"tooltipPosition,omitempty"`

	// Annotation is drawn over the shape without taking part in layout. On a
	// diagram's root it is the board's caption.
	Annotation *Annotation `json:"annotation,omitempty"`
	// LeavingAnnotation is the previous board's annotation while a sampled
	// frame erases it.
	LeavingAnnotation *Annotation `json:"leavingAnnotation,omitempty"`

	ZIndex int `json:"zIndex"`
	Level  int `json:"level"`

	// These are used for special shapes, sql_table and class
	PrimaryAccentColor   string `json:"primaryAccentColor,omitempty"`
	SecondaryAccentColor string `json:"secondaryAccentColor,omitempty"`
	NeutralAccentColor   string `json:"neutralAccentColor,omitempty"`
}

// Geometry returns the same outline and inner content box used for rendering.
func (s *Shape) Geometry() shape.Shape {
	return s.geometry(s.Pos, s.Width, s.Height)
}

func (s *Shape) geometry(pos Point, width, height int) shape.Shape {
	g := shape.NewShape(DSL_SHAPE_TO_SHAPE_TYPE[s.Type], geo.NewBox(
		geo.NewPoint(float64(pos.X), float64(pos.Y)), float64(width), float64(height)))
	if s.Type == ShapeCloud && s.ContentAspectRatio != nil {
		g.SetInnerBoxAspectRatio(*s.ContentAspectRatio)
	}
	return g
}

// LabelTopLeft resolves both ordinary captions and intermediate animation anchors.
func (s *Shape) LabelTopLeft(position label.Position, box *geo.Box) *geo.Point {
	if p := s.MotionLabelTopLeft; p != nil {
		return geo.NewPoint(float64(p.X), float64(p.Y))
	}
	if b := s.MotionLabelBox; b != nil {
		box = s.geometry(b.Point, b.Width, b.Height).GetInnerBox()
	}
	return position.GetPointOnBox(box, label.PADDING, float64(s.LabelWidth), float64(s.LabelHeight))
}

// ResolveGradientAngles applies the paint box for each CSS degree angle.
// Renderers call it on their local shape before creating paint resources.
func (s *Shape) ResolveGradientAngles() {
	w, h := float64(s.Width), float64(s.Height)
	s.Fill, s.Stroke = color.GradientForBox(s.Fill, w, h), color.GradientForBox(s.Stroke, w, h)
	lw, lh := float64(s.LabelWidth), float64(s.LabelHeight)
	s.Color, s.LabelFill = color.GradientForBox(s.Color, lw, lh), color.GradientForBox(s.LabelFill, lw, lh)
}

// ResolveGradientAngles uses the route box shared by the shaft and markers,
// and the label box for foreground and background text paints.
func (c *Connection) ResolveGradientAngles() {
	if strings.HasPrefix(c.Stroke, "linear-gradient(") && strings.Contains(c.Stroke, "deg") {
		box := c.GradientBounds()
		c.Stroke = color.GradientForBox(c.Stroke, box.Width, box.Height)
	}
	lw, lh := float64(c.LabelWidth), float64(c.LabelHeight)
	c.Color, c.Fill = color.GradientForBox(c.Color, lw, lh), color.GradientForBox(c.Fill, lw, lh)
}

func (s Shape) RenderedStrokeWidth() float64 {
	return float64(s.StrokeWidth) + s.StrokeWidthOffset
}

// CanShadeSketchFill reports whether the shape has a single solid sketch surface.
func (s *Shape) CanShadeSketchFill() bool {
	if s.ThreeDee || s.Multiple || (s.FillPattern != "" && s.FillPattern != "none") {
		return false
	}
	switch s.Type {
	case ShapeText, ShapeCode, ShapeImage, ShapeClass, ShapeSQLTable:
		return false
	}
	return validSketchFillPaint(s.Fill)
}

func validSketchFillPaint(paint string) bool {
	return color.IsThemeColor(paint) || (!color.IsGradient(paint) && color.ValidColor(paint))
}

func (s Shape) GetFontColor() string {
	if s.Type == ShapeClass || s.Type == ShapeSQLTable {
		if !color.IsThemeColor(s.Color) {
			return s.Color
		}
		return s.Stroke
	}
	if s.Color != color.Empty {
		return s.Color
	}
	return color.N1
}

// TODO remove this function, just set fields on themeable
func (s Shape) CSSStyle() string {
	out := ""

	out += "stroke-width:" + svg.FormatFloat(s.RenderedStrokeWidth()) + ";"
	if s.StrokeDash != 0 {
		dashSize, gapSize := svg.GetStrokeDashAttributes(s.RenderedStrokeWidth(), s.StrokeDash)
		out += "stroke-dasharray:" + svg.FormatFloat(dashSize) + "," + svg.FormatFloat(gapSize) + ";"
	}

	return out
}

func (s *Shape) SetType(t string) {
	// Some types are synonyms of other types, but with hinting for autolayout
	// They should only have one representation in the final export
	if strings.EqualFold(t, ShapeCircle) {
		t = ShapeOval
	} else if strings.EqualFold(t, ShapeSquare) {
		t = ShapeRectangle
	}
	s.Type = strings.ToLower(t)
}

func (s Shape) GetZIndex() int {
	return s.ZIndex
}

func (s Shape) GetID() string {
	return s.ID
}

// AppendixLinkText returns the text listed in a static export's appendix for
// the shape's link, or "" when the shape has no link. Every shape with a Link
// gets a numbered link marker, so it must also get an appendix entry: when no
// board label could be derived for PrettyLink (e.g. a link to the parent
// board), the link itself is listed so the marker and appendix numbers stay in
// one sequence.
func (s Shape) AppendixLinkText() string {
	if s.Link == "" {
		return ""
	}
	if s.PrettyLink != "" {
		return s.PrettyLink
	}
	return s.Link
}

// MarkdownStrikeTransition retains the previous markup when only strikethrough spans change.
type MarkdownStrikeTransition struct {
	From     string  `json:"from"`
	Progress float64 `json:"progress"`
	// On a plain destination these are the struck ranges of its shaped line,
	// in fractions of the total advance. Its own renderer paints the glyphs.
	PlainRanges [][2]float64 `json:"plainRanges,omitempty"`
}

func (t *MarkdownStrikeTransition) PlainRules(left, baseline, width, fontSize float64, draw func(x, y, width, height float64)) {
	if t == nil || t.Progress >= 1 {
		return
	}
	for _, span := range t.PlainRanges {
		draw(left+span[0]*width, baseline-.3*fontSize, (span[1]-span[0])*width*(1-t.Progress), math.Max(1, fontSize/16))
	}
}

type Text struct {
	Label      string `json:"label"`
	FontSize   int    `json:"fontSize"`
	FontFamily string `json:"fontFamily"`
	Language   string `json:"language"`
	Color      string `json:"color"`

	Italic    bool `json:"italic"`
	Bold      bool `json:"bold"`
	Underline bool `json:"underline"`
	// UnderlineProgress is the visible fraction of the underline on a sampled frame.
	UnderlineProgress *float64                  `json:"underlineProgress,omitempty"`
	MarkdownStrike    *MarkdownStrikeTransition `json:"markdownStrike,omitempty"`

	LabelWidth  int    `json:"labelWidth"`
	LabelHeight int    `json:"labelHeight"`
	LabelFill   string `json:"labelFill,omitempty"`
}

func BaseShape() *Shape {
	return &Shape{
		Opacity:     1,
		StrokeDash:  0,
		StrokeWidth: DEFAULT_SHAPE_STROKE_WIDTH,
		Text: Text{
			Bold:       true,
			FontFamily: "DEFAULT",
		},
	}
}

// MotionLabel anchors the caption and its mask while a partial route is drawn.
type MotionLabel struct {
	Point
	Opacity float64 `json:"opacity"`
}

// TraceEndpoint identifies which end of an animated polyline is still being drawn.
type TraceEndpoint string

const (
	TraceSource      TraceEndpoint = "source"
	TraceDestination TraceEndpoint = "destination"
)

// A nil Rate repeats one icon. An explicit rate emits icons per second;
// zero stops new emissions while icons already in flight finish their trips.
type ConnectionIconAnimation struct {
	Rate *float64 `json:"rate,omitempty"`
	// DurationMS sets the trip time of a single repeating icon. A positive duration
	// is mutually exclusive with Rate. Zero uses the diagram's default travel speed.
	DurationMS int64 `json:"durationMs,omitempty"`
	// A parallel edge may be renumbered in an inherited step. An empty
	// predecessor marks a replacement that reuses a deleted connection's ID.
	PreviousID *string `json:"previousID,omitempty"`
}

type Connection struct {
	ID string `json:"id"`

	MotionClip  *MotionClip  `json:"motionClip,omitempty"`
	MotionLabel *MotionLabel `json:"motionLabel,omitempty"`
	// MotionTip marks the free end of a partially drawn connection.
	MotionTip TraceEndpoint `json:"motionTip,omitempty"`
	// MotionDashOffset pins a backward trace’s dashes to its stationary end.
	MotionDashOffset float64 `json:"motionDashOffset,omitempty"`

	Classes []string `json:"classes,omitempty"`

	Src      string    `json:"src"`
	SrcArrow Arrowhead `json:"srcArrow"`
	SrcLabel *Text     `json:"srcLabel,omitempty"`

	Dst      string    `json:"dst"`
	DstArrow Arrowhead `json:"dstArrow"`
	DstLabel *Text     `json:"dstLabel,omitempty"`

	Opacity     float64 `json:"opacity"`
	StrokeDash  float64 `json:"strokeDash"`
	StrokeWidth int     `json:"strokeWidth"`
	// Tween frames retain the subpixel difference from the integer layout width.
	StrokeWidthOffset float64 `json:"strokeWidthOffset,omitempty"`
	Stroke            string  `json:"stroke"`
	Fill              string  `json:"fill,omitempty"`
	BorderRadius      float64 `json:"borderRadius,omitempty"`

	Text
	LabelPosition   string  `json:"labelPosition"`
	LabelPercentage float64 `json:"labelPercentage"`

	Link       string `json:"link"`
	PrettyLink string `json:"prettyLink,omitempty"`

	Route   []*geo.Point `json:"route"`
	IsCurve bool         `json:"isCurve,omitempty"`

	Animated         bool                     `json:"animated"`
	Tooltip          string                   `json:"tooltip"`
	Icon             *url.URL                 `json:"icon"`
	IconPosition     string                   `json:"iconPosition,omitempty"`
	IconBorderRadius float64                  `json:"iconBorderRadius,omitempty"`
	IconShape        string                   `json:"iconShape,omitempty"`
	IconAnimation    *ConnectionIconAnimation `json:"iconAnimation,omitempty"`
	// IconLabelInsets is derived from the current board's endpoint stroke widths.
	IconLabelInsets [2]float64 `json:"-"`
	// IconPositions holds sampled centers. Nil leaves playback to the renderer.
	IconPositions []geo.Point             `json:"-"`
	IconSchedule  *ConnectionIconSchedule `json:"-"`

	// Annotation is drawn over the connection without taking part in layout.
	Annotation *Annotation `json:"annotation,omitempty"`
	// LeavingAnnotation is the previous board's annotation while a sampled
	// frame erases it.
	LeavingAnnotation *Annotation `json:"leavingAnnotation,omitempty"`

	ZIndex int `json:"zIndex"`
}

const ConnectionIconTriangle = "triangle"

var ConnectionIconShapes = []string{ShapeCircle, ShapeSquare, ConnectionIconTriangle, ShapeDiamond}

func (c *Connection) HasStaticIcon() bool {
	return c.IconAnimation == nil && (c.Icon != nil || c.IconShape != "")
}

func (c *Connection) IconSize() int {
	return c.iconFootprint().size()
}

func (c *Connection) GetIconPosition() *geo.Point {
	if !c.HasStaticIcon() {
		return nil
	}

	size := float64(c.IconSize())
	if c.Label != "" {
		labelTL := c.GetLabelTopLeft()
		if labelTL != nil {
			// Position icon to the left of the label with a small gap
			return &geo.Point{
				X: labelTL.X - CONNECTION_ICON_LABEL_GAP - size,
				Y: labelTL.Y + float64(c.LabelHeight)/2 - size/2,
			}
		}
	}

	point, _ := label.FromString(c.IconPosition).GetPointOnRoute(
		c.Route,
		c.RenderedStrokeWidth(),
		-1,
		size,
		size,
	)
	return point
}

func BaseConnection() *Connection {
	return &Connection{
		SrcArrow:     NoArrowhead,
		DstArrow:     NoArrowhead,
		Route:        make([]*geo.Point, 0),
		Opacity:      1,
		StrokeDash:   0,
		StrokeWidth:  2,
		BorderRadius: 10,
		Text: Text{
			Italic:     true,
			FontFamily: "DEFAULT",
		},
	}
}

func (c Connection) RenderedStrokeWidth() float64 {
	return float64(c.StrokeWidth) + c.StrokeWidthOffset
}

func (c Connection) GetFontColor() string {
	if c.Color != color.Empty {
		return c.Color
	}
	return color.N1
}

func (c Connection) CSSStyle() string {
	out := ""

	out += "stroke-width:" + svg.FormatFloat(c.RenderedStrokeWidth()) + ";"
	strokeDash := c.StrokeDash
	if strokeDash == 0 && c.Animated {
		strokeDash = 5
	}
	if strokeDash != 0 {
		dashSize, gapSize := svg.GetStrokeDashAttributes(c.RenderedStrokeWidth(), strokeDash)
		out += "stroke-dasharray:" + svg.FormatFloat(dashSize) + "," + svg.FormatFloat(gapSize) + ";"

		if c.MotionDashOffset != 0 {
			out += "stroke-dashoffset:" + svg.FormatFloat(c.MotionDashOffset) + ";"
		}

		if c.Animated {
			dashOffset := -10
			if c.SrcArrow != NoArrowhead && c.DstArrow == NoArrowhead {
				dashOffset = 10
			}
			out += "stroke-dashoffset:" + svg.FormatFloat(float64(dashOffset)*(dashSize+gapSize)) + ";"
			out += "animation: dashdraw " + svg.FormatFloat(gapSize*0.5) + "s linear infinite;"
		}
	}
	return out
}

func (c *Connection) GetLabelTopLeft() *geo.Point {
	if p := c.MotionLabel; p != nil {
		return geo.NewPoint(float64(p.X), float64(p.Y))
	}
	return c.MovingIconFootprint().LabelTopLeft(c, label.FromString(c.LabelPosition), c.LabelPercentage, float64(c.LabelWidth), float64(c.LabelHeight), nil)
}

func (connection *Connection) GetArrowheadLabelPosition(isDst bool) *geo.Point {
	var width, height float64
	if isDst {
		width = float64(connection.DstLabel.LabelWidth)
		height = float64(connection.DstLabel.LabelHeight)
	} else {
		width = float64(connection.SrcLabel.LabelWidth)
		height = float64(connection.SrcLabel.LabelHeight)
	}

	// get the start/end points of edge segment with arrowhead
	index := 0
	if isDst {
		index = len(connection.Route) - 2
	}
	start, end := connection.Route[index], connection.Route[index+1]
	// Note: end to start to get normal towards unlocked top position
	normalX, normalY := geo.GetUnitNormalVector(end.X, end.Y, start.X, start.Y)

	// determine how much to move the label back from the very end of the edge
	// e.g. if normal points up {x: 0, y:1}, shift width/2 + padding to fit
	shift := math.Abs(normalX)*(height/2.+label.PADDING) +
		math.Abs(normalY)*(width/2.+label.PADDING)

	length := geo.Route(connection.Route).Length()
	var position float64
	if isDst {
		position = 1.
		if length > 0 {
			position -= shift / length
		}
	} else {
		position = 0.
		if length > 0 {
			position = shift / length
		}
	}

	strokeWidth := connection.RenderedStrokeWidth()

	labelTL, _ := label.UnlockedTop.GetPointOnRoute(connection.Route, strokeWidth, position, width, height)

	var arrowSize float64
	if isDst && connection.DstArrow != NoArrowhead {
		// Note: these dimensions are for rendering arrowheads on their side so we want the height
		_, arrowSize = connection.DstArrow.Dimensions(strokeWidth)
	} else if connection.SrcArrow != NoArrowhead {
		_, arrowSize = connection.SrcArrow.Dimensions(strokeWidth)
	}

	if arrowSize > 0 {
		// labelTL already accounts for strokeWidth and padding, we only want to shift further if the arrow is larger than this
		offset := (arrowSize/2 + ARROWHEAD_PADDING) - strokeWidth/2 - label.PADDING
		if offset > 0 {
			labelTL.X += normalX * offset
			labelTL.Y += normalY * offset
		}
	}

	return labelTL
}

func (c Connection) GetZIndex() int {
	return c.ZIndex
}

func (c Connection) GetID() string {
	return c.ID
}

type Arrowhead string

const (
	NoArrowhead               Arrowhead = "none"
	ArrowArrowhead            Arrowhead = "arrow"
	UnfilledTriangleArrowhead Arrowhead = "unfilled-triangle"
	TriangleArrowhead         Arrowhead = "triangle"
	DiamondArrowhead          Arrowhead = "diamond"
	FilledDiamondArrowhead    Arrowhead = "filled-diamond"
	CircleArrowhead           Arrowhead = "circle"
	FilledCircleArrowhead     Arrowhead = "filled-circle"
	CrossArrowhead            Arrowhead = "cross"
	BoxArrowhead              Arrowhead = "box"
	FilledBoxArrowhead        Arrowhead = "filled-box"

	// For fat arrows
	LineArrowhead Arrowhead = "line"

	// Crows feet notation
	CfOne          Arrowhead = "cf-one"
	CfMany         Arrowhead = "cf-many"
	CfOneRequired  Arrowhead = "cf-one-required"
	CfManyRequired Arrowhead = "cf-many-required"

	DefaultArrowhead Arrowhead = TriangleArrowhead
)

// valid values for arrowhead.shape
var Arrowheads = map[string]struct{}{
	string(NoArrowhead):       {},
	string(ArrowArrowhead):    {},
	string(TriangleArrowhead): {},
	string(DiamondArrowhead):  {},
	string(CircleArrowhead):   {},
	string(BoxArrowhead):      {},
	string(CfOne):             {},
	string(CfMany):            {},
	string(CfOneRequired):     {},
	string(CfManyRequired):    {},
	string(CrossArrowhead):    {},
}

func ToArrowhead(arrowheadType string, filled *bool) Arrowhead {
	switch arrowheadType {
	case string(DiamondArrowhead):
		if filled != nil && *filled {
			return FilledDiamondArrowhead
		}
		return DiamondArrowhead
	case string(CircleArrowhead):
		if filled != nil && *filled {
			return FilledCircleArrowhead
		}
		return CircleArrowhead
	case string(NoArrowhead):
		return NoArrowhead
	case string(ArrowArrowhead):
		return ArrowArrowhead
	case string(TriangleArrowhead):
		if filled != nil && !(*filled) {
			return UnfilledTriangleArrowhead
		}
		return TriangleArrowhead
	case string(CrossArrowhead):
		return CrossArrowhead
	case string(BoxArrowhead):
		if filled != nil && *filled {
			return FilledBoxArrowhead
		}
		return BoxArrowhead
	case string(CfOne):
		return CfOne
	case string(CfMany):
		return CfMany
	case string(CfOneRequired):
		return CfOneRequired
	case string(CfManyRequired):
		return CfManyRequired
	default:
		if DefaultArrowhead == TriangleArrowhead &&
			filled != nil && !(*filled) {
			return UnfilledTriangleArrowhead
		}
		return DefaultArrowhead
	}
}

func (arrowhead Arrowhead) Dimensions(strokeWidth float64) (width, height float64) {
	var baseWidth, baseHeight float64
	var widthMultiplier, heightMultiplier float64
	switch arrowhead {
	case ArrowArrowhead:
		baseWidth = 4
		baseHeight = 4
		widthMultiplier = 4
		heightMultiplier = 4
	case TriangleArrowhead:
		baseWidth = 4
		baseHeight = 4
		widthMultiplier = 3
		heightMultiplier = 4
	case UnfilledTriangleArrowhead:
		baseWidth = 7
		baseHeight = 7
		widthMultiplier = 3
		heightMultiplier = 4
	case LineArrowhead:
		widthMultiplier = 5
		heightMultiplier = 8
	case FilledDiamondArrowhead:
		baseWidth = 11
		baseHeight = 7
		widthMultiplier = 5.5
		heightMultiplier = 3.5
	case DiamondArrowhead:
		baseWidth = 11
		baseHeight = 9
		widthMultiplier = 5.5
		heightMultiplier = 4.5
	case CrossArrowhead:
		baseWidth = 7
		baseHeight = 7
		widthMultiplier = 5
		heightMultiplier = 5
	case FilledCircleArrowhead, CircleArrowhead:
		baseWidth = 8
		baseHeight = 8
		widthMultiplier = 5
		heightMultiplier = 5
	case FilledBoxArrowhead, BoxArrowhead:
		baseWidth = 6
		baseHeight = 6
		widthMultiplier = 5
		heightMultiplier = 5
	case CfOne, CfMany, CfOneRequired, CfManyRequired:
		baseWidth = 9
		baseHeight = 9
		widthMultiplier = 4.5
		heightMultiplier = 4.5
	}

	clippedStrokeWidth := go2.Max(MIN_ARROWHEAD_STROKE_WIDTH, strokeWidth)
	return baseWidth + clippedStrokeWidth*widthMultiplier, baseHeight + clippedStrokeWidth*heightMultiplier
}

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func NewPoint(x, y int) Point {
	return Point{X: x, Y: y}
}

const (
	ShapeRectangle       = "rectangle"
	ShapeSquare          = "square"
	ShapePage            = "page"
	ShapeParallelogram   = "parallelogram"
	ShapeDocument        = "document"
	ShapeCylinder        = "cylinder"
	ShapeQueue           = "queue"
	ShapePackage         = "package"
	ShapeStep            = "step"
	ShapeCallout         = "callout"
	ShapeStoredData      = "stored_data"
	ShapePerson          = "person"
	ShapeC4Person        = "c4-person"
	ShapeDiamond         = "diamond"
	ShapeOval            = "oval"
	ShapeCircle          = "circle"
	ShapeHexagon         = "hexagon"
	ShapeCloud           = "cloud"
	ShapeText            = "text"
	ShapeCode            = "code"
	ShapeClass           = "class"
	ShapeSQLTable        = "sql_table"
	ShapeImage           = "image"
	ShapeSequenceDiagram = "sequence_diagram"
	ShapeHierarchy       = "hierarchy"
)

var Shapes = []string{
	ShapeRectangle,
	ShapeSquare,
	ShapePage,
	ShapeParallelogram,
	ShapeDocument,
	ShapeCylinder,
	ShapeQueue,
	ShapePackage,
	ShapeStep,
	ShapeCallout,
	ShapeStoredData,
	ShapePerson,
	ShapeC4Person,
	ShapeDiamond,
	ShapeOval,
	ShapeCircle,
	ShapeHexagon,
	ShapeCloud,
	ShapeText,
	ShapeCode,
	ShapeClass,
	ShapeSQLTable,
	ShapeImage,
	ShapeSequenceDiagram,
	ShapeHierarchy,
}

func IsShape(s string) bool {
	if s == "" {
		// Default shape is rectangle.
		return true
	}
	for _, s2 := range Shapes {
		if strings.EqualFold(s, s2) {
			return true
		}
	}
	return false
}

type MText struct {
	Text        string `json:"text"`
	FontSize    int    `json:"fontSize"`
	IsBold      bool   `json:"isBold"`
	IsItalic    bool   `json:"isItalic"`
	IsUnderline bool   `json:"isUnderline"`
	Language    string `json:"language"`
	Shape       string `json:"shape"`

	Dimensions TextDimensions `json:"dimensions,omitempty"`
}

type TextDimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func NewTextDimensions(w, h int) *TextDimensions {
	return &TextDimensions{Width: w, Height: h}
}

func (text MText) GetColor(isItalic bool) string {
	if isItalic {
		return color.N2
	}
	return color.N1
}

var DSL_SHAPE_TO_SHAPE_TYPE = map[string]string{
	"":                   shape.SQUARE_TYPE,
	ShapeRectangle:       shape.SQUARE_TYPE,
	ShapeSquare:          shape.REAL_SQUARE_TYPE,
	ShapePage:            shape.PAGE_TYPE,
	ShapeParallelogram:   shape.PARALLELOGRAM_TYPE,
	ShapeDocument:        shape.DOCUMENT_TYPE,
	ShapeCylinder:        shape.CYLINDER_TYPE,
	ShapeQueue:           shape.QUEUE_TYPE,
	ShapePackage:         shape.PACKAGE_TYPE,
	ShapeStep:            shape.STEP_TYPE,
	ShapeCallout:         shape.CALLOUT_TYPE,
	ShapeStoredData:      shape.STORED_DATA_TYPE,
	ShapePerson:          shape.PERSON_TYPE,
	ShapeC4Person:        shape.C4_PERSON_TYPE,
	ShapeDiamond:         shape.DIAMOND_TYPE,
	ShapeOval:            shape.OVAL_TYPE,
	ShapeCircle:          shape.CIRCLE_TYPE,
	ShapeHexagon:         shape.HEXAGON_TYPE,
	ShapeCloud:           shape.CLOUD_TYPE,
	ShapeText:            shape.TEXT_TYPE,
	ShapeCode:            shape.CODE_TYPE,
	ShapeClass:           shape.CLASS_TYPE,
	ShapeSQLTable:        shape.TABLE_TYPE,
	ShapeImage:           shape.IMAGE_TYPE,
	ShapeSequenceDiagram: shape.SQUARE_TYPE,
	ShapeHierarchy:       shape.SQUARE_TYPE,
}

var SHAPE_TYPE_TO_DSL_SHAPE map[string]string

func init() {
	SHAPE_TYPE_TO_DSL_SHAPE = make(map[string]string, len(DSL_SHAPE_TO_SHAPE_TYPE))
	for k, v := range DSL_SHAPE_TO_SHAPE_TYPE {
		SHAPE_TYPE_TO_DSL_SHAPE[v] = k
	}
	// SQUARE_TYPE is defined twice in the map, make sure it doesn't get set to the empty string one
	SHAPE_TYPE_TO_DSL_SHAPE[shape.SQUARE_TYPE] = ShapeRectangle
}

// OuterLabelBox is the painted outline used to place outside and border labels.
func (s *Shape) OuterLabelBox(box *geo.Box) *geo.Box {
	var dx, dy int
	if s.ThreeDee {
		dx, dy = THREE_DEE_OFFSET, THREE_DEE_OFFSET
		if s.Type == ShapeHexagon {
			dy /= 2
		}
	} else if s.Multiple {
		dx, dy = MULTIPLE_OFFSET, MULTIPLE_OFFSET
	} else {
		return box
	}
	return geo.NewBox(geo.NewPoint(box.TopLeft.X, box.TopLeft.Y-float64(dy)), box.Width+float64(dx), box.Height+float64(dy))
}

// IconBounds returns the rendered icon rectangle for a shape with an icon.
func (s *Shape) IconBounds(outline shape.Shape) *geo.Box {
	position := label.FromString(s.IconPosition)
	var box *geo.Box
	if position.IsOutside() {
		box = outline.GetBox()
	} else {
		box = outline.GetInnerBox()
	}
	size := float64(GetIconSize(box, s.IconPosition))
	return geo.NewBox(position.GetPointOnBox(box, label.PADDING, size, size), size, size)
}

func GetIconSize(box *geo.Box, position string) int {
	iconPosition := label.FromString(position)

	minDimension := int(math.Min(box.Width, box.Height))
	// A tightly sized shape can have no usable interior for an icon.
	if minDimension <= 0 {
		return 0
	}
	halfMinDimension := int(math.Ceil(0.5 * float64(minDimension)))

	var size int

	if iconPosition == label.InsideMiddleCenter {
		size = halfMinDimension
	} else {
		size = go2.Min(
			minDimension,
			go2.Max(DEFAULT_ICON_SIZE, halfMinDimension),
		)
	}

	size = go2.Min(size, MAX_ICON_SIZE)

	if !iconPosition.IsOutside() {
		size = go2.Min(size,
			go2.Min(
				go2.Max(int(box.Width)-2*label.PADDING, 0),
				go2.Max(int(box.Height)-2*label.PADDING, 0),
			),
		)
	}

	return size
}
