package d2target

import (
	"fmt"
	"math"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/svg"
)

const (
	// Bound raw render targets before they reach recursive hashing and
	// multi-board rendering. Validation itself remains iterative.
	maxRenderTargetBoardDepth = 1_024
	maxRenderTargetBoardCount = 4_096
)

// ValidateRenderTarget checks the invariants required by renderers which
// consume Diagram JSON directly. Compiler output already satisfies these
// invariants, but callers of the public render API can construct a Diagram
// without going through the compiler.
func ValidateRenderTarget(root *Diagram) error {
	if root == nil {
		return fmt.Errorf("render target is nil")
	}

	type boardEntry struct {
		diagram *Diagram
		path    string
		depth   int
	}

	stack := []boardEntry{{diagram: root, path: "root"}}
	seen := map[*Diagram]string{root: "root"}
	boardCount := 1

	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if err := validateRenderBoard(entry.diagram, entry.path); err != nil {
			return err
		}

		groups := []struct {
			name     string
			children []*Diagram
		}{
			{name: "layers", children: entry.diagram.Layers},
			{name: "scenarios", children: entry.diagram.Scenarios},
			{name: "steps", children: entry.diagram.Steps},
		}
		// Push in reverse so errors follow the renderer's layers, scenarios,
		// steps traversal order without using recursive calls.
		for groupIndex := len(groups) - 1; groupIndex >= 0; groupIndex-- {
			group := groups[groupIndex]
			for childIndex := len(group.children) - 1; childIndex >= 0; childIndex-- {
				child := group.children[childIndex]
				childPath := fmt.Sprintf("%s.%s[%d]", entry.path, group.name, childIndex)
				if child == nil {
					return fmt.Errorf("render target board %s is nil", childPath)
				}
				childDepth := entry.depth + 1
				if childDepth > maxRenderTargetBoardDepth {
					return fmt.Errorf("render target board tree exceeds depth %d at %s", maxRenderTargetBoardDepth, childPath)
				}
				if firstPath, ok := seen[child]; ok {
					return fmt.Errorf("render target board %s reuses board %s", childPath, firstPath)
				}
				boardCount++
				if boardCount > maxRenderTargetBoardCount {
					return fmt.Errorf("render target board tree exceeds %d total boards", maxRenderTargetBoardCount)
				}
				seen[child] = childPath
				stack = append(stack, boardEntry{diagram: child, path: childPath, depth: childDepth})
			}
		}
	}

	return nil
}

func validateRenderBoard(diagram *Diagram, path string) error {
	if err := validateRenderFontFamily(diagram.FontFamily, path+".fontFamily"); err != nil {
		return err
	}
	if err := validateRenderFontFamily(diagram.MonoFontFamily, path+".monoFontFamily"); err != nil {
		return err
	}
	if err := validateRenderShapeNumbers(diagram.Root, path+".root"); err != nil {
		return err
	}
	for i, targetShape := range diagram.Shapes {
		if err := validateRenderShape(targetShape, fmt.Sprintf("%s.shapes[%d]", path, i)); err != nil {
			return err
		}
	}

	for i, connection := range diagram.Connections {
		if err := validateRenderConnection(connection, fmt.Sprintf("%s.connections[%d]", path, i)); err != nil {
			return err
		}
	}

	if diagram.Legend != nil {
		for i, targetShape := range diagram.Legend.Shapes {
			if err := validateRenderLegendShape(targetShape, fmt.Sprintf("%s.legend.shapes[%d]", path, i)); err != nil {
				return err
			}
		}
		for i, connection := range diagram.Legend.Connections {
			connectionPath := fmt.Sprintf("%s.legend.connections[%d]", path, i)
			if err := validateRenderLegendConnection(connection, connectionPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateRenderLegendShape(targetShape Shape, path string) error {
	// Legend rendering replaces the icon geometry and suppresses the shape's
	// own label. Validate only the numeric fields which survive that projection.
	targetShape.Pos = Point{}
	targetShape.Width = 1
	targetShape.Height = 1
	targetShape.Text.Label = ""
	targetShape.Text.FontSize = 0
	targetShape.Text.LabelWidth = 0
	targetShape.Text.LabelHeight = 0
	return validateRenderShape(targetShape, path)
}

func validateRenderLegendConnection(connection Connection, path string) error {
	// Legend rendering synthesizes its own two-point route and suppresses the
	// connection label. Project the fields it copies before applying numeric
	// validation so harmless unused JSON fields remain compatible.
	legendConnection := *BaseConnection()
	legendConnection.SrcArrow = connection.SrcArrow
	legendConnection.DstArrow = connection.DstArrow
	legendConnection.StrokeDash = connection.StrokeDash
	legendConnection.StrokeWidth = connection.StrokeWidth
	legendConnection.BorderRadius = connection.BorderRadius
	legendConnection.Opacity = connection.Opacity
	if err := validateRenderConnectionNumbers(legendConnection, path); err != nil {
		return err
	}
	if err := validateRenderArrowhead(connection.SrcArrow, path+".srcArrow"); err != nil {
		return err
	}
	return validateRenderArrowhead(connection.DstArrow, path+".dstArrow")
}

func validateRenderShape(targetShape Shape, path string) error {
	if err := validateRenderShapeNumbers(targetShape, path); err != nil {
		return err
	}
	if targetShape.Type == ShapeImage && targetShape.Icon == nil {
		return fmt.Errorf("render target %s image is missing an icon", path)
	}
	return nil
}

func validateRenderConnection(connection Connection, path string) error {
	if err := validateRenderConnectionNumbers(connection, path); err != nil {
		return err
	}
	if err := validateRenderArrowhead(connection.SrcArrow, path+".srcArrow"); err != nil {
		return err
	}
	if err := validateRenderArrowhead(connection.DstArrow, path+".dstArrow"); err != nil {
		return err
	}
	if len(connection.Route) < 2 {
		return fmt.Errorf("render target %s.route must contain at least two points", path)
	}
	if connection.IsCurve && (len(connection.Route)-1)%3 != 0 {
		return fmt.Errorf("render target %s.route must contain 1+3n points for a cubic connection", path)
	}
	for i, point := range connection.Route {
		if point == nil {
			return fmt.Errorf("render target %s.route[%d] is nil", path, i)
		}
		if !finiteRenderNumber(point.X) || !finiteRenderNumber(point.Y) {
			return fmt.Errorf("render target %s.route[%d] must contain finite coordinates", path, i)
		}
		pointPath := fmt.Sprintf("%s.route[%d]", path, i)
		if err := validateRenderRouteCoordinate(pointPath, "x", point.X, connection.StrokeWidth); err != nil {
			return err
		}
		if err := validateRenderRouteCoordinate(pointPath, "y", point.Y, connection.StrokeWidth); err != nil {
			return err
		}
		if i > 0 {
			previous := connection.Route[i-1]
			dx, dy := point.X-previous.X, point.Y-previous.Y
			lengthSquared := dx*dx + dy*dy
			if !finiteRenderNumber(lengthSquared) || lengthSquared == 0 {
				return fmt.Errorf("render target %s.route[%d:%d] must have a finite, non-zero segment length", path, i-1, i)
			}
		}
	}

	if connection.Label != "" {
		position := label.FromString(connection.LabelPosition)
		if !position.IsEdgePosition() {
			return fmt.Errorf("render target %s.labelPosition %q is not a connection label position", path, connection.LabelPosition)
		}
		if !finiteRenderNumber(connection.LabelPercentage) {
			return fmt.Errorf("render target %s.labelPercentage must be finite", path)
		}
		labelTopLeft := connection.GetLabelTopLeft()
		if labelTopLeft == nil || !finiteRenderNumber(labelTopLeft.X) || !finiteRenderNumber(labelTopLeft.Y) {
			return fmt.Errorf("render target %s.labelPosition does not resolve to a finite point on the route", path)
		}
	}

	for _, endpoint := range []struct {
		name  string
		label *Text
		isDst bool
	}{
		{name: "srcLabel", label: connection.SrcLabel},
		{name: "dstLabel", label: connection.DstLabel, isDst: true},
	} {
		if endpoint.label == nil || endpoint.label.Label == "" {
			continue
		}
		labelTopLeft := connection.GetArrowheadLabelPosition(endpoint.isDst)
		if labelTopLeft == nil || !finiteRenderNumber(labelTopLeft.X) || !finiteRenderNumber(labelTopLeft.Y) {
			return fmt.Errorf("render target %s.%s does not resolve to a finite point on the route", path, endpoint.name)
		}
	}

	return nil
}

func validateRenderArrowhead(arrowhead Arrowhead, path string) error {
	switch arrowhead {
	case NoArrowhead,
		ArrowArrowhead,
		UnfilledTriangleArrowhead,
		TriangleArrowhead,
		LineArrowhead,
		FilledDiamondArrowhead,
		DiamondArrowhead,
		FilledCircleArrowhead,
		CircleArrowhead,
		CrossArrowhead,
		FilledBoxArrowhead,
		BoxArrowhead,
		CfOne,
		CfMany,
		CfOneRequired,
		CfManyRequired:
		return nil
	default:
		return fmt.Errorf("render target %s uses unsupported arrowhead %q", path, arrowhead)
	}
}

func validateRenderFontFamily(family *d2fonts.FontFamily, path string) error {
	if family == nil || *family == "" {
		return nil
	}
	for _, style := range d2fonts.FontStyles {
		font := d2fonts.Font{Family: *family, Style: style}
		face, ok := d2fonts.FontFaces.Lookup(font)
		if !ok || len(face) == 0 {
			return fmt.Errorf("render target %s %q is not a registered font family", path, *family)
		}
	}
	return nil
}

func validateRenderShapeNumbers(targetShape Shape, path string) error {
	for _, field := range []struct {
		name  string
		value int
	}{
		{name: "width", value: targetShape.Width},
		{name: "height", value: targetShape.Height},
		{name: "strokeWidth", value: targetShape.StrokeWidth},
		{name: "borderRadius", value: targetShape.BorderRadius},
		{name: "iconBorderRadius", value: targetShape.IconBorderRadius},
	} {
		if field.value < 0 {
			return invalidRenderField(path, field.name, field.value, "must be non-negative")
		}
	}
	if err := validateRenderOpacity(path, targetShape.Opacity); err != nil {
		return err
	}
	if err := validateRenderDash(path, targetShape.StrokeDash, targetShape.StrokeWidth); err != nil {
		return err
	}
	if targetShape.ContentAspectRatio != nil && (!finiteRenderNumber(*targetShape.ContentAspectRatio) || *targetShape.ContentAspectRatio <= 0) {
		return invalidRenderField(path, "contentAspectRatio", *targetShape.ContentAspectRatio, "must be finite and greater than zero")
	}
	if err := validateRenderTextNumbers(path, "", targetShape.Text); err != nil {
		return err
	}
	return validateRenderShapeIntegerBounds(path, targetShape)
}

func validateRenderConnectionNumbers(connection Connection, path string) error {
	if connection.StrokeWidth < 0 {
		return invalidRenderField(path, "strokeWidth", connection.StrokeWidth, "must be non-negative")
	}
	if err := validateRenderOpacity(path, connection.Opacity); err != nil {
		return err
	}
	if err := validateRenderDash(path, connection.StrokeDash, connection.StrokeWidth); err != nil {
		return err
	}
	if !finiteRenderNumber(connection.BorderRadius) || connection.BorderRadius < 0 {
		return invalidRenderField(path, "borderRadius", connection.BorderRadius, "must be finite and non-negative")
	}
	if !finiteRenderNumber(connection.IconBorderRadius) || connection.IconBorderRadius < 0 {
		return invalidRenderField(path, "iconBorderRadius", connection.IconBorderRadius, "must be finite and non-negative")
	}
	if !finiteRenderNumber(connection.LabelPercentage) || connection.LabelPercentage < 0 || connection.LabelPercentage > 1 {
		return invalidRenderField(path, "labelPercentage", connection.LabelPercentage, "must be finite and within [0,1]")
	}
	if err := validateRenderTextNumbers(path, "", connection.Text); err != nil {
		return err
	}
	if connection.SrcLabel != nil {
		if err := validateRenderTextNumbers(path, "srcLabel.", *connection.SrcLabel); err != nil {
			return err
		}
	}
	if connection.DstLabel != nil {
		if err := validateRenderTextNumbers(path, "dstLabel.", *connection.DstLabel); err != nil {
			return err
		}
	}
	return nil
}

func validateRenderOpacity(path string, opacity float64) error {
	if !finiteRenderNumber(opacity) || opacity < 0 || opacity > 1 {
		return invalidRenderField(path, "opacity", opacity, "must be finite and within [0,1]")
	}
	return nil
}

func validateRenderDash(path string, dash float64, strokeWidth int) error {
	if !finiteRenderNumber(dash) || dash < 0 {
		return invalidRenderField(path, "strokeDash", dash, "must be finite and non-negative")
	}
	if dash > 0 && strokeWidth > 0 {
		dashSize, gapSize := svg.GetStrokeDashAttributes(float64(strokeWidth), dash)
		if !finiteRenderNumber(dashSize) || !finiteRenderNumber(gapSize) || dashSize < 0 || gapSize < 0 {
			return invalidRenderField(path, "strokeDash", dash, "must produce finite non-negative dash lengths with strokeWidth")
		}
	}
	return nil
}

func validateRenderTextNumbers(path, prefix string, text Text) error {
	for _, field := range []struct {
		name  string
		value int
	}{
		{name: prefix + "fontSize", value: text.FontSize},
		{name: prefix + "labelWidth", value: text.LabelWidth},
		{name: prefix + "labelHeight", value: text.LabelHeight},
	} {
		if field.value < 0 {
			return invalidRenderField(path, field.name, field.value, "must be non-negative")
		}
	}
	return nil
}

func validateRenderShapeIntegerBounds(path string, targetShape Shape) error {
	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1
	halfStroke := int64(targetShape.StrokeWidth/2 + targetShape.StrokeWidth%2)
	for _, axis := range []struct {
		name      string
		position  int
		dimension int
	}{
		{name: "pos.x", position: targetShape.Pos.X, dimension: targetShape.Width},
		{name: "pos.y", position: targetShape.Pos.Y, dimension: targetShape.Height},
	} {
		if _, ok := checkedRenderSub(int64(axis.position), halfStroke, minInt, maxInt); !ok {
			return invalidRenderField(path, axis.name, axis.position, "must fit bounds arithmetic with strokeWidth")
		}
		end, ok := checkedRenderAdd(int64(axis.position), int64(axis.dimension), minInt, maxInt)
		if !ok {
			return invalidRenderField(path, axis.name, axis.position, "must fit bounds arithmetic with its dimension")
		}
		if _, ok := checkedRenderAdd(end, halfStroke, minInt, maxInt); !ok {
			return invalidRenderField(path, axis.name, axis.position, "must fit bounds arithmetic with strokeWidth")
		}
	}
	return nil
}

func validateRenderRouteCoordinate(path, field string, coordinate float64, strokeWidth int) error {
	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1
	halfStroke := math.Ceil(float64(strokeWidth) / 2)
	// BoundingBox floors/ceils route coordinates and converts them to int.
	// Strict comparisons avoid the rounded float64 representation of MaxInt.
	if math.Floor(coordinate)-halfStroke <= float64(minInt) || math.Ceil(coordinate)+halfStroke >= float64(maxInt) {
		return invalidRenderField(path, field, coordinate, "must fit integer bounds arithmetic")
	}
	return nil
}

func checkedRenderAdd(a, value, minInt, maxInt int64) (int64, bool) {
	if value > 0 && a > maxInt-value || value < 0 && a < minInt-value {
		return 0, false
	}
	return a + value, true
}

func checkedRenderSub(a, value, minInt, maxInt int64) (int64, bool) {
	if value > 0 && a < minInt+value || value < 0 && a > maxInt+value {
		return 0, false
	}
	return a - value, true
}

func invalidRenderField(path, field string, value any, requirement string) error {
	return fmt.Errorf("render target %s.%s has invalid value %v: %s", path, field, value, requirement)
}

func finiteRenderNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
