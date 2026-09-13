package d2svg

import (
	"fmt"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/util-go/go2"
)

// validateRenderPaints keeps raw render targets within the same paint grammar
// as compiler-produced targets. In particular, arbitrary CSS paint servers such
// as url(https://example.com/paint.svg) must not reach an SVG attribute.
func validateRenderPaints(diagram *d2target.Diagram, opts *RenderOpts) error {
	if err := validatePaint(diagram.Root.Fill, "root fill"); err != nil {
		return err
	}
	if err := validatePaint(diagram.Root.Stroke, "root stroke"); err != nil {
		return err
	}
	if err := validateFillPattern(diagram.Root.FillPattern, "root fill pattern"); err != nil {
		return err
	}

	for i := range diagram.Shapes {
		if err := validateShapePaints(&diagram.Shapes[i], fmt.Sprintf("shape %q", diagram.Shapes[i].ID)); err != nil {
			return err
		}
	}
	for i := range diagram.Connections {
		if err := validateConnectionPaints(&diagram.Connections[i], fmt.Sprintf("connection %q", diagram.Connections[i].ID)); err != nil {
			return err
		}
	}
	if diagram.Legend != nil {
		for i := range diagram.Legend.Shapes {
			if err := validateShapePaints(&diagram.Legend.Shapes[i], fmt.Sprintf("legend shape %q", diagram.Legend.Shapes[i].ID)); err != nil {
				return err
			}
		}
		for i := range diagram.Legend.Connections {
			connection := &diagram.Legend.Connections[i]
			object := fmt.Sprintf("legend connection %q", connection.ID)
			if err := validatePaint(connection.Stroke, object+" stroke"); err != nil {
				return err
			}
			if err := validatePaint(connection.Fill, object+" fill"); err != nil {
				return err
			}
		}
	}

	if opts != nil {
		if err := validateThemeOverrides(opts.ThemeOverrides, "theme override"); err != nil {
			return err
		}
		if err := validateThemeOverrides(opts.DarkThemeOverrides, "dark theme override"); err != nil {
			return err
		}
	}
	return nil
}

func validateShapePaints(shape *d2target.Shape, object string) error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "fill", value: shape.Fill},
		{name: "stroke", value: shape.Stroke},
		{name: "font color", value: shape.Color},
		{name: "label fill", value: shape.LabelFill},
		{name: "primary accent color", value: shape.PrimaryAccentColor},
		{name: "secondary accent color", value: shape.SecondaryAccentColor},
		{name: "neutral accent color", value: shape.NeutralAccentColor},
	} {
		if err := validatePaint(field.value, object+" "+field.name); err != nil {
			return err
		}
	}
	return validateFillPattern(shape.FillPattern, object+" fill pattern")
}

func validateConnectionPaints(connection *d2target.Connection, object string) error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "stroke", value: connection.Stroke},
		{name: "fill", value: connection.Fill},
		{name: "font color", value: connection.Color},
	} {
		if err := validatePaint(field.value, object+" "+field.name); err != nil {
			return err
		}
	}
	if connection.SrcLabel != nil {
		if err := validatePaint(connection.SrcLabel.Color, object+" source label color"); err != nil {
			return err
		}
	}
	if connection.DstLabel != nil {
		if err := validatePaint(connection.DstLabel.Color, object+" destination label color"); err != nil {
			return err
		}
	}
	return nil
}

func validatePaint(value, field string) error {
	if value == "" || value == color.None || color.IsThemeColor(value) || color.ValidColor(value) {
		return nil
	}
	return fmt.Errorf("%s uses unsupported paint %q", field, value)
}

func validateFillPattern(value, field string) error {
	if value == "" || go2.Contains(d2ast.FillPatterns, strings.ToLower(value)) {
		return nil
	}
	return fmt.Errorf("%s uses unsupported value %q", field, value)
}

func validateThemeOverrides(overrides *d2target.ThemeOverrides, object string) error {
	if overrides == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{name: "N1", value: overrides.N1},
		{name: "N2", value: overrides.N2},
		{name: "N3", value: overrides.N3},
		{name: "N4", value: overrides.N4},
		{name: "N5", value: overrides.N5},
		{name: "N6", value: overrides.N6},
		{name: "N7", value: overrides.N7},
		{name: "B1", value: overrides.B1},
		{name: "B2", value: overrides.B2},
		{name: "B3", value: overrides.B3},
		{name: "B4", value: overrides.B4},
		{name: "B5", value: overrides.B5},
		{name: "B6", value: overrides.B6},
		{name: "AA2", value: overrides.AA2},
		{name: "AA4", value: overrides.AA4},
		{name: "AA5", value: overrides.AA5},
		{name: "AB4", value: overrides.AB4},
		{name: "AB5", value: overrides.AB5},
	} {
		if field.value == nil {
			continue
		}
		if !go2.Contains(color.NamedColors, strings.ToLower(*field.value)) && !color.ColorHexRegex.MatchString(*field.value) {
			return fmt.Errorf("%s %s uses unsupported color %q", object, field.name, *field.value)
		}
	}
	return nil
}
