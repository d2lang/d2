package d2compiler

import (
	"net/url"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2target"
)

func (c *compiler) compileIcon(attrs *d2graph.Attributes, edge *d2graph.Edge, f *d2ir.Field) {
	if f.Primary() != nil {
		icon, err := url.Parse(f.Primary().Value.ScalarString())
		if err != nil {
			c.errorf(f.Primary().Value, "bad icon url %#v: %s", f.Primary().Value.ScalarString(), err)
			return
		}
		attrs.Icon = icon
		if edge != nil {
			edge.IconShape = nil
		}
	}
	m := f.Map()
	if m == nil {
		if f.Primary() == nil || f.Composite != nil {
			c.errorf(fieldErrorNode(f), "icon requires an image URL or a map of icon properties")
		}
		return
	}
	if len(m.Edges) > 0 {
		c.errorf(fieldErrorNode(f), "unexpected edges in icon map")
	}
	for _, child := range m.Fields {
		if c.stopped() {
			return
		}
		switch d2ast.Keyword(child.Name) {
		case "near":
			c.compileNear(attrs, "icon", child)
		case "shape":
			if edge == nil {
				c.errorf(fieldErrorNode(child), "icon.shape is only supported on connections")
				continue
			}
			if f.Primary() != nil {
				c.errorf(fieldErrorNode(child), "connection icon cannot specify both an image URL and a shape")
				continue
			}
			if child.Primary() == nil || child.Composite != nil || !strings.EqualFold(child.Primary().Value.ScalarString(), d2target.ShapeCircle) {
				c.errorf(fieldErrorNode(child), "connection icon.shape must be circle")
				continue
			}
			attrs.Icon = nil
			edge.IconShape = &d2graph.Scalar{Value: d2target.ShapeCircle, MapKey: child.LastPrimaryKey()}
		case "style":
			if child.Map() == nil || len(child.Map().Fields) == 0 {
				c.errorf(fieldErrorNode(child), "icon.style requires a map of styles")
				continue
			}
			imageStyle := &attrs.IconStyle
			if f.Primary() == nil {
				// Image styles belong to the field that supplies the image URL.
				// A map-only icon may add motion to artwork supplied by a class.
				imageStyle = &d2graph.Style{}
			}
			for _, style := range child.Map().Fields {
				if c.stopped() {
					return
				}
				if edge == nil || d2ast.Keyword(style.Name) != "animated" {
					c.compileStyleField(imageStyle, style)
					continue
				}
				if style.Primary() == nil || style.Composite != nil {
					c.errorf(fieldErrorNode(style), "connection icon.style.animated requires a single value")
					continue
				}
				value := style.Primary().Value.ScalarString()
				if _, err := d2graph.ParseConnectionIconAnimation(value); err != nil {
					c.errorf(style.Primary().Value, "%s", err)
					continue
				}
				edge.IconStyle.Animated = &d2graph.Scalar{Value: value, MapKey: style.LastPrimaryKey()}
			}
		default:
			c.errorf(fieldErrorNode(child), "unexpected field %s in icon", child.Name.ScalarString())
		}
	}
}
