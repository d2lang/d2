package color

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type Gradient struct {
	Type       string
	Direction  string
	ColorStops []ColorStop
	ID         string
}

type ColorStop struct {
	Color    string
	Position string
}

// Offset returns the rendered stop position, clamped to [previous, 1] so
// authored stops cannot move backwards along the gradient.
func (stop ColorStop) Offset(index, total int, previous float64) (float64, error) {
	if stop.Position == "" {
		if total == 1 {
			return 0, nil
		}
		return max(previous, float64(index)/float64(total-1)), nil
	}
	position := strings.TrimSpace(stop.Position)
	divisor := 1.0
	if strings.HasSuffix(position, "%") {
		position = strings.TrimSpace(strings.TrimSuffix(position, "%"))
		divisor = 100
	}
	value, err := strconv.ParseFloat(position, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("must be a finite number or percentage")
	}
	return max(previous, min(1, value/divisor)), nil
}

func ParseGradient(cssGradient string) (Gradient, error) {
	cssGradient = strings.TrimSpace(cssGradient)

	re := regexp.MustCompile(`^(linear-gradient|radial-gradient)\((.*)\)$`)
	matches := re.FindStringSubmatch(cssGradient)
	if matches == nil {
		return Gradient{}, errors.New("invalid gradient syntax")
	}

	gradientType := matches[1]
	params := matches[2]

	gradient := Gradient{
		Type: strings.TrimSuffix(gradientType, "-gradient"),
	}

	paramList := splitParams(params)

	if len(paramList) == 0 {
		return Gradient{}, errors.New("no parameters in gradient")
	}

	firstParam := strings.TrimSpace(paramList[0])
	var err error

	if gradient.Type == "linear" && (strings.HasSuffix(firstParam, "deg") || strings.HasPrefix(firstParam, "to ")) {
		gradient.Direction = firstParam
		colorStops := paramList[1:]
		if len(colorStops) == 0 {
			return Gradient{}, errors.New("no color stops in gradient")
		}
		gradient.ColorStops, err = parseColorStops(colorStops)
	} else if gradient.Type == "radial" && (firstParam == "circle" || firstParam == "ellipse") {
		gradient.Direction = firstParam
		colorStops := paramList[1:]
		if len(colorStops) == 0 {
			return Gradient{}, errors.New("no color stops in gradient")
		}
		gradient.ColorStops, err = parseColorStops(colorStops)
	} else {
		gradient.ColorStops, err = parseColorStops(paramList)
	}
	if err != nil {
		return Gradient{}, err
	}
	gradient.ID = UniqueGradientID(cssGradient)

	return gradient, nil
}

func splitParams(params string) []string {
	var parts []string
	var buf strings.Builder
	nesting := 0

	for _, r := range params {
		switch r {
		case ',':
			if nesting == 0 {
				parts = append(parts, buf.String())
				buf.Reset()
				continue
			}
		case '(':
			nesting++
		case ')':
			if nesting > 0 {
				nesting--
			}
		}
		buf.WriteRune(r)
	}
	if buf.Len() > 0 {
		parts = append(parts, buf.String())
	}
	return parts
}

func parseColorStops(params []string) ([]ColorStop, error) {
	var colorStops []ColorStop
	for i, p := range params {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("invalid color stop %d %q", i, p)
		}
		stop := ColorStop{Color: p}
		// Only whitespace outside a color function separates its offset.
		depth := 0
		for j, r := range p {
			if depth == 0 && unicode.IsSpace(r) {
				position := strings.TrimSpace(p[j:])
				canonical, err := canonicalGradientStopPosition(position)
				if err != nil {
					return nil, fmt.Errorf("color stop %d position %q: %w", i, position, err)
				}
				stop.Color, stop.Position = p[:j], canonical
				break
			}
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth < 0 {
					return nil, fmt.Errorf("unbalanced color function in stop %d %q", i, p)
				}
			}
		}
		if depth != 0 {
			return nil, fmt.Errorf("unbalanced color function in stop %d %q", i, p)
		}
		colorStops = append(colorStops, stop)
	}
	return colorStops, nil
}

func canonicalGradientStopPosition(position string) (string, error) {
	percentage := strings.HasSuffix(position, "%")
	number := strings.TrimSuffix(position, "%")
	value, err := strconv.ParseFloat(number, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", errors.New("must be a finite number or percentage")
	}
	canonical := strconv.FormatFloat(value, 'g', -1, 64)
	if percentage {
		canonical += "%"
	}
	return canonical, nil
}

func GradientToSVG(gradient Gradient) string {
	switch gradient.Type {
	case "linear":
		return LinearGradientToSVG(gradient)
	case "radial":
		return RadialGradientToSVG(gradient)
	default:
		return ""
	}
}

func LinearGradientToSVG(gradient Gradient) string {
	x1, y1, x2, y2 := parseLinearGradientDirection(gradient.Direction)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<linearGradient id="%s" `, html.EscapeString(gradient.ID)))
	sb.WriteString(fmt.Sprintf(`x1="%s" y1="%s" x2="%s" y2="%s">`, html.EscapeString(x1), html.EscapeString(y1), html.EscapeString(x2), html.EscapeString(y2)))
	sb.WriteString("\n")

	totalStops := len(gradient.ColorStops)
	for i, cs := range gradient.ColorStops {
		offset := gradientStopOffset(cs.Position, i, totalStops)
		sb.WriteString(fmt.Sprintf(`<stop offset="%s" stop-color="%s" />`, html.EscapeString(offset), html.EscapeString(cs.Color)))
		sb.WriteString("\n")
	}
	sb.WriteString(`</linearGradient>`)
	return sb.String()
}

// LinearGradientVector returns endpoints in object-bounding-box coordinates.
// For malformed directions it also reports an error; the coordinates retain
// the historical SVG fallback for unrecognized direction words.
func LinearGradientVector(direction string) (x1, y1, x2, y2 float64, err error) {
	direction = strings.TrimSpace(direction)
	if strings.HasPrefix(direction, "to ") {
		x1, y1, x2, y2 = .5, .5, .5, .5
		for _, part := range strings.Fields(strings.TrimPrefix(direction, "to ")) {
			switch part {
			case "left":
				x1, x2 = 1, 0
			case "right":
				x1, x2 = 0, 1
			case "top":
				y1, y2 = 1, 0
			case "bottom":
				y1, y2 = 0, 1
			default:
				err = fmt.Errorf("unsupported linear-gradient direction %q", direction)
			}
		}
		return x1, y1, x2, y2, err
	}
	if strings.HasSuffix(direction, "deg") {
		degrees, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(direction, "deg")), 64)
		if err != nil || math.IsNaN(degrees) || math.IsInf(degrees, 0) {
			return 0, 0, 0, 1, fmt.Errorf("invalid linear-gradient angle %q", direction)
		}
		// CSS measures clockwise from upward. Project the bounding box onto
		// that axis to place the end stops at opposite corners' projections.
		radians := math.Mod(degrees, 360) * (math.Pi / 180)
		dx, cos := math.Sincos(radians)
		dy := -cos
		halfLength := (math.Abs(dx) + math.Abs(dy)) / 2
		return .5 - dx*halfLength, .5 - dy*halfLength, .5 + dx*halfLength, .5 + dy*halfLength, nil
	}
	if direction != "" {
		err = fmt.Errorf("unsupported linear-gradient direction %q", direction)
	}
	return 0, 0, 0, 1, err
}

// GradientForBox expresses a CSS angle in SVG object-bounding-box coordinates.
// In pixels the color varies along (sin θ, -cos θ). In the unit box those
// two coefficients are scaled by width and height. The converted spelling
// also gives differently proportioned paint boxes distinct SVG resources.
func GradientForBox(paint string, width, height float64) string {
	if width == height || width <= 0 || height <= 0 {
		return paint
	}
	params, ok := strings.CutPrefix(paint, "linear-gradient(")
	if !ok {
		return paint
	}
	direction, stops, ok := strings.Cut(params, ",")
	if !ok {
		return paint
	}
	angle, ok := strings.CutSuffix(strings.TrimSpace(direction), "deg")
	if !ok {
		return paint
	}
	degrees, err := strconv.ParseFloat(strings.TrimSpace(angle), 64)
	if err != nil || math.IsNaN(degrees) || math.IsInf(degrees, 0) || math.Mod(degrees, 90) == 0 {
		return paint
	}
	sin, cos := math.Sincos(math.Mod(degrees, 360) * (math.Pi / 180))
	unitDegrees := math.Atan2(width*sin, height*cos) * (180 / math.Pi)
	return "linear-gradient(" + strconv.FormatFloat(unitDegrees, 'g', -1, 64) + "deg," + stops
}

func parseLinearGradientDirection(direction string) (x1, y1, x2, y2 string) {
	ax, ay, bx, by, err := LinearGradientVector(direction)
	precision := 0
	if err == nil && strings.HasSuffix(strings.TrimSpace(direction), "deg") {
		precision = 2
	}
	percent := func(v float64) string { return strconv.FormatFloat(v*100, 'f', precision, 64) + "%" }
	return percent(ax), percent(ay), percent(bx), percent(by)
}

func RadialGradientToSVG(gradient Gradient) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<radialGradient id="%s">`, html.EscapeString(gradient.ID)))
	sb.WriteString("\n")
	totalStops := len(gradient.ColorStops)
	for i, cs := range gradient.ColorStops {
		offset := gradientStopOffset(cs.Position, i, totalStops)
		sb.WriteString(fmt.Sprintf(`<stop offset="%s" stop-color="%s" />`, html.EscapeString(offset), html.EscapeString(cs.Color)))
		sb.WriteString("\n")
	}
	sb.WriteString(`</radialGradient>`)
	return sb.String()
}

func gradientStopOffset(position string, index, totalStops int) string {
	if position != "" {
		return position
	}
	if totalStops == 1 {
		return "0.00%"
	}
	return fmt.Sprintf("%.2f%%", float64(index)/float64(totalStops-1)*100)
}

func UniqueGradientID(cssGradient string) string {
	h := sha1.New()
	h.Write([]byte(cssGradient))
	hash := hex.EncodeToString(h.Sum(nil))
	return "grad-" + hash
}

var GradientRegex = regexp.MustCompile(`^(linear|radial)-gradient\((.+)\)$`)

func IsGradient(color string) bool {
	return GradientRegex.MatchString(color)
}

var URLGradientID = regexp.MustCompile(`^url\('#grad-[a-f0-9]{40}'\)$`)

func IsURLGradientID(color string) bool {
	return URLGradientID.MatchString(color)
}
