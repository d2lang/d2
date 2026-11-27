package d2svgimport

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"math/big"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2scene"
)

var stopPresentationProperties = map[string]struct{}{
	"stop-color":   {},
	"stop-opacity": {},
}

func isGradientResourceElement(name string) bool {
	return name == "linearGradient" || name == "radialGradient" || name == "stop"
}

// Gradients are definitions even when placed directly in a graphics container.
func (i *svgImporter) validateGradientTree(root *svgElement) error {
	return i.walkElements(root, func(element, parent *svgElement) error {
		switch element.name {
		case "linearGradient", "radialGradient":
			if parent == nil || (parent.name != "defs" && parent.name != "g" && parent.name != "svg") {
				return i.errorf("element <%s> must be inside <svg>, <g> or <defs>", element.name)
			}
			for _, child := range element.children {
				if child.name != "stop" {
					return i.errorf("element <%s> has unsupported child <%s>", element.name, displayXMLName(child.name))
				}
			}
		case "stop":
			if parent == nil || (parent.name != "linearGradient" && parent.name != "radialGradient") {
				return i.errorf("element <stop> is only supported inside a gradient")
			}
			if len(element.children) != 0 {
				return i.errorf("element <stop> cannot contain child elements")
			}
		}
		return nil
	})
}

func (i *svgImporter) compileGradientElement(element *svgElement) error {
	allowed := elementAttributes[element.name]
	for _, name := range element.attrOrder {
		if err := i.ctx.Err(); err != nil {
			return err
		}
		if _, ok := allowed[name]; !ok {
			return i.errorf("element <%s> has unsupported attribute %q", element.name, displayXMLName(name))
		}
	}

	switch element.name {
	case "linearGradient", "radialGradient":
		return i.compileGradient(element)
	case "stop":
		return i.compileGradientStop(element)
	default:
		return i.errorf("internal unsupported gradient element <%s>", element.name)
	}
}

// Percentages in user space are relative to the viewport of the element
// using the paint. Keep them unresolved until that element is instantiated.
type gradientLength struct {
	value   float64
	percent bool
}

func (length gradientLength) resolve(extent float64) float64 {
	if length.percent {
		return length.value * extent
	}
	return length.value
}

func parseLengthOrPercentage(raw string, units d2scene.PaintUnits) (gradientLength, error) {
	raw = strings.TrimSpace(raw)
	number, percent := strings.CutSuffix(raw, "%")
	if percent && strings.TrimSpace(number) != number {
		return gradientLength{}, fmt.Errorf("percentage sign must follow its number")
	}
	value, err := parseSVGLength(number, !percent && units == d2scene.UserSpaceOnUse)
	if percent {
		value /= 100
	}
	return gradientLength{value: value, percent: percent}, err
}

type svgGradient struct {
	units     d2scene.PaintUnits
	transform d2scene.Matrix
	spread    d2scene.SpreadMethod
	stops     []d2scene.GradientStop
	radial    bool
	// A radial gradient runs from the focal circle to the outer circle.
	startX, startY, endX, endY gradientLength
	innerRadius, outerRadius   gradientLength
}

func (g *svgGradient) paint(ctx context.Context, viewport d2scene.Box, opacity float64) (d2scene.Paint, error) {
	if len(g.stops) == 0 {
		return nil, nil
	}
	w, h := viewport.Width, viewport.Height
	if g.units == d2scene.ObjectBoundingBox {
		w, h = 1, 1
	}
	start := d2scene.Point{X: g.startX.resolve(w), Y: g.startY.resolve(h)}
	end := d2scene.Point{X: g.endX.resolve(w), Y: g.endY.resolve(h)}
	stops, err := stopsWithOpacity(ctx, g.stops, opacity)
	if err != nil {
		return nil, err
	}
	if !d2scene.BoundsFromPoints(start, end).IsFinite() {
		return nil, fmt.Errorf("invalid gradient coordinates")
	}
	if g.radial {
		diagonal := math.Hypot(w/math.Sqrt2, h/math.Sqrt2)
		radius, focalRadius := g.outerRadius.resolve(diagonal), g.innerRadius.resolve(diagonal)
		if math.IsInf(radius, 0) || math.IsInf(focalRadius, 0) {
			return nil, fmt.Errorf("invalid radial gradient radius")
		}
		return d2scene.RadialGradient{Center: end, Focal: start, Radius: radius, FocalRadius: focalRadius, Units: g.units, Transform: g.transform, Spread: g.spread, Stops: stops}, nil
	}
	direction := g.transform.Vector(d2scene.Point{X: end.X - start.X, Y: end.Y - start.Y})
	if math.IsNaN(direction.X) || math.IsInf(direction.X, 0) || math.IsNaN(direction.Y) || math.IsInf(direction.Y, 0) {
		return nil, fmt.Errorf("invalid transformed gradient geometry")
	}
	return d2scene.LinearGradient{Start: start, End: end, Units: g.units, Transform: g.transform, Spread: g.spread, Stops: stops}, nil
}

func (i *svgImporter) compileGradient(element *svgElement) error {
	g := &svgGradient{radial: element.name == "radialGradient"}
	switch strings.TrimSpace(element.attrs["gradientUnits"]) {
	case "", "objectBoundingBox":
		g.units = d2scene.ObjectBoundingBox
	case "userSpaceOnUse":
		g.units = d2scene.UserSpaceOnUse
	default:
		return i.errorf("element <%s> has invalid gradientUnits", element.name)
	}
	switch strings.TrimSpace(element.attrs["spreadMethod"]) {
	case "", "pad":
		g.spread = d2scene.SpreadPad
	case "reflect":
		g.spread = d2scene.SpreadReflect
	case "repeat":
		g.spread = d2scene.SpreadRepeat
	default:
		return i.errorf("element <%s> has invalid spreadMethod", element.name)
	}
	type coordinate struct {
		name   string
		length *gradientLength
	}
	var coords []coordinate
	if g.radial {
		half := gradientLength{value: 0.5, percent: true}
		g.endX, g.endY, g.outerRadius = half, half, half
		coords = []coordinate{{"cx", &g.endX}, {"cy", &g.endY}, {"r", &g.outerRadius}, {"fx", &g.startX}, {"fy", &g.startY}, {"fr", &g.innerRadius}}
	} else {
		g.endX = gradientLength{value: 1, percent: true}
		coords = []coordinate{{"x1", &g.startX}, {"y1", &g.startY}, {"x2", &g.endX}, {"y2", &g.endY}}
	}
	for _, coord := range coords {
		raw, ok := element.attrs[coord.name]
		if !ok {
			continue
		}
		length, err := parseLengthOrPercentage(raw, g.units)
		if err != nil {
			return i.propertyError(element, coord.name, err)
		}
		*coord.length = length
	}
	if g.radial {
		if _, ok := element.attrs["fx"]; !ok {
			g.startX = g.endX
		}
		if _, ok := element.attrs["fy"]; !ok {
			g.startY = g.endY
		}
		if g.innerRadius.value < 0 || g.outerRadius.value < 0 {
			return i.errorf("element <radialGradient> has a negative radius")
		}
	}
	transform := d2scene.Identity()
	if raw, ok := element.attrs["gradientTransform"]; ok {
		trimmed, err := trimSVGSpace(i.ctx, raw)
		if err != nil {
			return err
		}
		if trimmed == "" {
			return i.errorf("element <%s> has an empty gradientTransform", element.name)
		}
		remaining := i.limits.MaxTransformFunctions - i.parsedTransforms
		if remaining <= 0 {
			return i.errorf("transform function count exceeds limit %d", i.limits.MaxTransformFunctions)
		}
		var functions int
		transform, functions, err = parseTransformWithCount(i.ctx, i.source, trimmed, transformLimits{
			MaxBytes: i.limits.MaxAttributeBytes, MaxFunctions: remaining,
		})
		if err != nil {
			return err
		}
		i.parsedTransforms += functions
	}
	if !affineInverseIsFinite(transform) {
		return i.errorf("element <%s> has a singular or unrepresentable gradientTransform", element.name)
	}
	g.transform = transform
	element.gradient = g
	return nil
}

func (i *svgImporter) compileGradientStop(element *svgElement) error {
	classes, classSet, err := i.classTokens(element)
	if err != nil {
		return err
	}
	element.classes = classes
	element.classSet = classSet
	declarations, err := i.declarationsFor(element)
	if err != nil {
		return err
	}
	element.declarations = declarations
	for property := range declarations {
		if _, ok := stopPresentationProperties[property]; !ok {
			return i.errorf("element <stop> has unsupported style property %q", property)
		}
	}

	rawOffset := strings.TrimSpace(element.attrs["offset"])
	offset := 0.0
	if rawOffset != "" {
		number, percent := strings.CutSuffix(rawOffset, "%")
		if strings.TrimSpace(number) != number {
			return i.propertyError(element, "offset", fmt.Errorf("percentage sign must follow its number"))
		}
		offset, err = parseSVGNumber(number)
		if err != nil {
			return i.propertyError(element, "offset", err)
		}
		if percent {
			offset /= 100
		}
		offset = min(1, max(0, offset))
	}
	stop, err := i.stopFromDeclarations(element)
	if err != nil {
		return err
	}
	stop.Offset = offset
	element.gradientStop = &stop
	return nil
}

func (i *svgImporter) stopFromDeclarations(element *svgElement) (d2scene.GradientStop, error) {
	stopColor := color.NRGBA{A: 255}
	if raw, ok := element.declarations["stop-color"]; ok {
		if equalASCIIEqualFold(raw, "inherit") || equalASCIIEqualFold(raw, "currentColor") || strings.Contains(strings.ToLower(raw), "url(") {
			return d2scene.GradientStop{}, i.propertyError(element, "stop-color", fmt.Errorf("only an explicit solid color is supported"))
		}
		parsed, err := parseSolidColor(raw)
		if err != nil {
			return d2scene.GradientStop{}, i.propertyError(element, "stop-color", err)
		}
		stopColor = parsed
	}
	opacity := 1.0
	if raw, ok := element.declarations["stop-opacity"]; ok {
		if equalASCIIEqualFold(raw, "inherit") {
			return d2scene.GradientStop{}, i.propertyError(element, "stop-opacity", fmt.Errorf("inheritance is unsupported"))
		}
		parsed, err := parseUnitInterval(raw)
		if err != nil {
			return d2scene.GradientStop{}, i.propertyError(element, "stop-opacity", err)
		}
		opacity = parsed
	}
	stopColor.A = uint8(math.Round(float64(stopColor.A) * opacity))
	return d2scene.GradientStop{Color: stopColor}, nil
}

func (i *svgImporter) compileGradientResources(root *svgElement) error {
	return i.walkElements(root, func(element, _ *svgElement) error {
		if element.gradient != nil {
			stops := make([]d2scene.GradientStop, len(element.children))
			previous := -1.0
			for index, child := range element.children {
				if err := i.ctx.Err(); err != nil {
					return err
				}
				if child.gradientStop == nil {
					return i.errorf("internal uncompiled gradient stop")
				}
				stop := *child.gradientStop
				stop.Offset = max(previous, stop.Offset)
				stops[index] = stop
				previous = stop.Offset
			}
			element.gradient.stops = stops
		}
		return nil
	})
}

func (i *svgImporter) validateRegularDeclarations(element *svgElement) error {
	for property := range element.declarations {
		if _, stopOnly := stopPresentationProperties[property]; stopOnly {
			return i.errorf("element <%s> has unsupported style property %q", element.name, property)
		}
		if element.name == "defs" && property == "clip-path" {
			return i.errorf("element <defs> cannot carry clip-path because it is not an emitted element")
		}
	}
	return nil
}

// validateStylesheetRules validates every class rule, including unmatched
// rules. Stop declarations and ordinary rendering declarations cannot be
// mixed because no supported element accepts both.
func (i *svgImporter) validateStylesheetRules() error {
	for _, rule := range i.stylesheetRules {
		if err := i.ctx.Err(); err != nil {
			return err
		}
		regular := make(map[string]string)
		stops := make(map[string]string)
		for property, value := range rule.declarations {
			if _, stopOnly := stopPresentationProperties[property]; stopOnly {
				stops[property] = value
			} else {
				regular[property] = value
			}
		}
		if len(regular) != 0 && len(stops) != 0 {
			return i.errorf("stylesheet rule .%s mixes stop-only and rendering properties", strings.Join(rule.classes, ", ."))
		}
		if len(stops) != 0 {
			probe := &svgElement{name: "stop", declarations: stops}
			if _, err := i.stopFromDeclarations(probe); err != nil {
				return err
			}
			continue
		}
		probe := &svgElement{name: "style", declarations: regular}
		if _, err := i.computeStyle(defaultSVGStyle(), probe); err != nil {
			return err
		}
	}
	return nil
}

func (i *svgImporter) localGradient(value string) (*svgGradient, error) {
	if !strings.HasPrefix(value, "url(#") || !strings.HasSuffix(value, ")") {
		return nil, fmt.Errorf("paint servers require one local url(#id) reference; external references and fallbacks are forbidden")
	}
	id := value[len("url(#") : len(value)-1]
	valid, err := i.validLocalPaintID(id)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, fmt.Errorf("paint servers require one local url(#id) reference; external references and fallbacks are forbidden")
	}
	target, declared := i.ids[id]
	if declared && target == nil {
		return nil, fmt.Errorf("paint server references an ambiguous local id")
	}
	if target == nil {
		return nil, fmt.Errorf("paint server references an unknown local id")
	}
	if target.gradient == nil {
		return nil, fmt.Errorf("paint server local id does not name a supported gradient")
	}
	return target.gradient, i.ctx.Err()
}

// validLocalPaintID accepts the bounded unescaped identifier spelling used by
// the corpus. Broader SVG IDs would require a real CSS token/escape parser and
// are rejected instead of being reinterpreted by string slicing.
func (i *svgImporter) validLocalPaintID(id string) (bool, error) {
	if id == "" {
		return false, nil
	}
	for index := 0; index < len(id); index++ {
		if index&4095 == 0 {
			if err := i.ctx.Err(); err != nil {
				return false, err
			}
		}
		character := id[index]
		if index == 0 {
			if !isASCIIAlpha(character) && character != '_' {
				return false, nil
			}
			continue
		}
		if !isASCIIAlpha(character) && (character < '0' || character > '9') &&
			character != '_' && character != '-' && character != '.' && character != ':' {
			return false, nil
		}
	}
	return true, i.ctx.Err()
}

// affineInverseIsFinite uses exact rational arithmetic for the determinant and
// inverse coefficients. Raw float64 determinants can underflow or overflow for
// finite, invertible SVG matrices such as scale(1e300,1e-300).
func affineInverseIsFinite(matrix d2scene.Matrix) bool {
	if !matrix.IsFinite() {
		return false
	}
	rat := func(value float64) *big.Rat {
		return new(big.Rat).SetFloat64(value)
	}
	mul := func(left, right *big.Rat) *big.Rat {
		return new(big.Rat).Mul(left, right)
	}
	sub := func(left, right *big.Rat) *big.Rat {
		return new(big.Rat).Sub(left, right)
	}
	a, b, c, d := rat(matrix.A), rat(matrix.B), rat(matrix.C), rat(matrix.D)
	e, f := rat(matrix.E), rat(matrix.F)
	determinant := sub(mul(a, d), mul(b, c))
	if determinant.Sign() == 0 {
		return false
	}
	numerators := []*big.Rat{
		d,
		new(big.Rat).Neg(b),
		new(big.Rat).Neg(c),
		a,
		sub(mul(c, f), mul(d, e)),
		sub(mul(b, e), mul(a, f)),
	}
	for _, numerator := range numerators {
		value, _ := new(big.Rat).Quo(numerator, determinant).Float64()
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}
