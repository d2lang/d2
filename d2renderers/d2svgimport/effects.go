package d2svgimport

import (
	"fmt"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2scene"
)

// SVG mask and filter regions default to the target's geometric bounding box
// with a ten percent margin. Their contents have a separate coordinate system.
type svgEffectRegion struct {
	units, contentUnits d2scene.PaintUnits
	x, y, width, height gradientLength
}

func paintUnits(raw string, fallback d2scene.PaintUnits) (d2scene.PaintUnits, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return fallback, nil
	case "objectBoundingBox":
		return d2scene.ObjectBoundingBox, nil
	case "userSpaceOnUse":
		return d2scene.UserSpaceOnUse, nil
	default:
		return fallback, fmt.Errorf("expected objectBoundingBox or userSpaceOnUse")
	}
}

func (i *svgImporter) effectRegion(el *svgElement, unitsName, contentName string) (*svgEffectRegion, error) {
	r := &svgEffectRegion{
		x: gradientLength{-0.1, true}, y: gradientLength{-0.1, true},
		width: gradientLength{1.2, true}, height: gradientLength{1.2, true},
	}
	var err error
	r.units, err = paintUnits(el.attrs[unitsName], d2scene.ObjectBoundingBox)
	if err != nil {
		return nil, i.propertyError(el, unitsName, err)
	}
	r.contentUnits, err = paintUnits(el.attrs[contentName], d2scene.UserSpaceOnUse)
	if err != nil {
		return nil, i.propertyError(el, contentName, err)
	}
	for _, field := range []struct {
		name string
		dst  *gradientLength
	}{{"x", &r.x}, {"y", &r.y}, {"width", &r.width}, {"height", &r.height}} {
		if raw, ok := el.attrs[field.name]; ok {
			*field.dst, err = parseLengthOrPercentage(raw, r.units)
			if err != nil {
				return nil, i.propertyError(el, field.name, err)
			}
		}
	}
	if r.width.value < 0 || r.height.value < 0 {
		return nil, i.errorf("element <%s> has negative region dimensions", el.name)
	}
	return r, nil
}

func (r *svgEffectRegion) box(object, viewport d2scene.Box) d2scene.Box {
	if r.units == d2scene.ObjectBoundingBox {
		return d2scene.Box{X: object.X + r.x.value*object.Width, Y: object.Y + r.y.value*object.Height,
			Width: r.width.value * object.Width, Height: r.height.value * object.Height}
	}
	return d2scene.Box{X: r.x.resolve(viewport.Width), Y: r.y.resolve(viewport.Height),
		Width: r.width.resolve(viewport.Width), Height: r.height.resolve(viewport.Height)}
}

func (i *svgImporter) compileMask(element *svgElement) error {
	switch strings.TrimSpace(element.attrs["mask-type"]) {
	case "", "luminance", "alpha":
	default:
		return i.errorf("element <mask> has unsupported mask-type")
	}
	region, err := i.effectRegion(element, "maskUnits", "maskContentUnits")
	if err == nil {
		element.maskRegion = region
	}
	return err
}

func isFilterElement(name string) bool {
	return name == "filter" || name == "feGaussianBlur"
}

// Lower a single Gaussian blur of SourceGraphic to the scene filter. Other
// filter graphs need distinct intermediate inputs and outputs.
func (i *svgImporter) compileFilterElement(el *svgElement, allowed map[string]struct{}) error {
	for _, name := range el.attrOrder {
		if _, ok := allowed[name]; !ok {
			return i.errorf("element <%s> has unsupported attribute %q", el.name, name)
		}
	}
	if el.name == "feGaussianBlur" {
		if el.parent == nil || el.parent.name != "filter" || len(el.children) != 0 {
			return i.errorf("<feGaussianBlur> must be a leaf inside a filter")
		}
		return nil
	}
	if el.parent == nil || (el.parent.name != "svg" && el.parent.name != "g" && el.parent.name != "defs") {
		return i.errorf("element <filter> must be inside <svg>, <g> or <defs>")
	}
	if len(el.children) != 1 || el.children[0].name != "feGaussianBlur" {
		return i.errorf("element <filter> supports one feGaussianBlur of SourceGraphic")
	}
	blur := el.children[0]
	if source, ok := blur.attrs["in"]; ok && source != "SourceGraphic" {
		return i.errorf("element <feGaussianBlur> supports only SourceGraphic")
	}
	el.blur.LinearRGB = true
	for _, resource := range []*svgElement{el, blur} {
		if value, ok := resource.attrs["color-interpolation-filters"]; ok {
			switch value {
			case "linearRGB":
				el.blur.LinearRGB = true
			case "sRGB":
				el.blur.LinearRGB = false
			case "inherit": // Keep the enclosing filter's value.
			default:
				return i.errorf("unsupported color-interpolation-filters on <%s>", resource.name)
			}
		}
	}
	if raw, ok := blur.attrs["stdDeviation"]; ok {
		values, err := splitSVGNumberList(i.ctx, raw, 2)
		if err != nil || len(values) == 0 {
			return i.errorf("feGaussianBlur requires one or two deviations")
		}
		for n, value := range values {
			sigma, err := parseSVGNumber(value)
			if err != nil || sigma < 0 {
				return i.errorf("feGaussianBlur requires nonnegative finite deviations")
			}
			if n == 0 {
				el.blur.SigmaX = sigma
			}
			el.blur.SigmaY = sigma
		}
	}
	region, err := i.effectRegion(el, "filterUnits", "primitiveUnits")
	el.filterRegion = region
	return err
}

func (i *svgImporter) applySVGEffects(node *d2scene.Node, style svgStyle) (*d2scene.Node, error) {
	if style.filterID != "" {
		resource := i.ids[style.filterID]
		bounds, err := i.objectBounds(node)
		if err != nil {
			return nil, err
		}
		region := resource.filterRegion.box(bounds, style.viewport)
		if region.Width <= 0 || region.Height <= 0 {
			node.Opacity = 0
			return node, nil
		}
		blur := resource.blur
		if resource.filterRegion.contentUnits == d2scene.ObjectBoundingBox {
			blur.SigmaX *= bounds.Width
			blur.SigmaY *= bounds.Height
		}
		// Apply the filter before its region, element clip, mask and opacity.
		wrapper, err := i.newNode(nil)
		if err != nil {
			return nil, err
		}
		wrapper.Clip, err = i.regionClip(region)
		if err != nil {
			return nil, err
		}
		wrapper.Opacity, node.Opacity = node.Opacity, 1
		wrapper.ID, node.ID = node.ID, ""
		wrapper.Transform, node.Transform = node.Transform, d2scene.Identity()
		node.Filters = []d2scene.Filter{blur}
		wrapper.Children = []*d2scene.Node{node}
		node = wrapper
	}
	if style.maskID != "" {
		if i.buildingMask {
			return nil, i.errorf("nested mask references are unsupported")
		}
		mask := i.ids[style.maskID]
		box, err := i.objectBounds(node)
		if err != nil {
			return nil, err
		}
		region := mask.maskRegion.box(box, style.viewport)
		if region.Width <= 0 || region.Height <= 0 {
			node.Opacity = 0
			return node, nil
		}
		// Mask content inherits from its definition, independently of the
		// painted element. Percentage lengths use the referencing viewport.
		inherited := defaultSVGStyleWithColor(i.rootCurrentColor)
		inherited.viewport = style.viewport
		var ancestors []*svgElement
		for parent := mask.parent; parent != nil; parent = parent.parent {
			ancestors = append(ancestors, parent)
		}
		for n := len(ancestors) - 1; n >= 0; n-- {
			inherited, err = i.computeStyle(inherited, ancestors[n])
			if err != nil {
				return nil, err
			}
		}
		inherited.viewport = style.viewport
		if mask.maskRegion.contentUnits == d2scene.ObjectBoundingBox {
			inherited.viewport = d2scene.Box{Width: 1, Height: 1}
		}
		i.buildingMask = true
		content, err := i.instantiate(mask, inherited, "", 0, true)
		i.buildingMask = false
		if err != nil {
			return nil, err
		}
		if content == nil {
			node.Opacity = 0
			return node, nil
		}
		if mask.maskRegion.contentUnits == d2scene.ObjectBoundingBox {
			content.Transform = d2scene.Translate(box.X, box.Y).Mul(d2scene.Scale(box.Width, box.Height)).Mul(content.Transform)
		}
		wrapper, err := i.newNode(nil)
		if err != nil {
			return nil, err
		}
		wrapper.Children = []*d2scene.Node{content}
		wrapper.Clip, err = i.regionClip(region)
		if err != nil {
			return nil, err
		}
		kind := d2scene.MaskLuminance
		if strings.TrimSpace(mask.attrs["mask-type"]) == "alpha" {
			kind = d2scene.MaskAlpha
		}
		node.Mask = &d2scene.Mask{Type: kind, Transform: d2scene.Identity(), Root: wrapper}
	}
	return i.applyClipPaths(node, style.clipPathID)
}

func (i *svgImporter) regionClip(region d2scene.Box) (*d2scene.Clip, error) {
	clip := nestedViewportClip(region.Width, region.Height)
	if len(clip.Path.Commands) > i.limits.MaxPathCommands-i.emittedCommands {
		return nil, i.errorf("emitted path command count exceeds limit %d", i.limits.MaxPathCommands)
	}
	i.emittedCommands += len(clip.Path.Commands)
	clip.Transform = d2scene.Translate(region.X, region.Y)
	return clip, nil
}

// SVG object bounding boxes exclude stroke, clipping, masks and filters. The
// node's own transform maps this entire coordinate space into its parent.
func (i *svgImporter) objectBounds(node *d2scene.Node) (d2scene.Box, error) {
	type entry struct {
		node      *d2scene.Node
		transform d2scene.Matrix
	}
	stack := []entry{{node, d2scene.Identity()}}
	var bounds d2scene.Bounds
	for len(stack) > 0 {
		if err := i.ctx.Err(); err != nil {
			return d2scene.Box{}, err
		}
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		primitive := current.node.Primitive
		switch p := primitive.(type) {
		case d2scene.Rect:
			p.Fill, p.Stroke = d2scene.SolidPaint{}, nil
			primitive = p
		case d2scene.Path:
			p.Fill, p.Stroke = d2scene.SolidPaint{}, nil
			primitive = p
		case d2scene.Ellipse:
			p.Fill, p.Stroke = d2scene.SolidPaint{}, nil
			primitive = p
		}
		if primitive != nil {
			b, err := d2scene.PrimitiveBounds(primitive, current.transform)
			if err != nil || !b.IsFinite() {
				return d2scene.Box{}, i.errorf("invalid effect geometry bounds")
			}
			bounds = bounds.Union(b)
		}
		for _, child := range current.node.Children {
			stack = append(stack, entry{child, current.transform.Mul(child.Transform)})
		}
	}
	return bounds.Box(), nil
}
