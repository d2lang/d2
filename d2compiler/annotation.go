package d2compiler

import (
	"encoding/xml"
	"regexp"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/textmeasure"
)

// annotationNameHint is appended to errors that a shape named "annotation"
// runs into, since the name was an ordinary one before it became a keyword.
const annotationNameHint = `. If "annotation" is the name of a shape here, quote it`

// compileAnnotation reads `annotation: text {shape: circle}` and the dotted
// form `annotation.shape: circle`. A class and the object using it can each
// set part of the same annotation, so later fields refine earlier ones.
func (c *compiler) compileAnnotation(attrs *d2graph.Attributes, f *d2ir.Field) {
	if _, list := f.Composite.(*d2ir.Array); list {
		c.errorf(fieldErrorNode(f), "annotation takes text, a map of shape, near and style, or both; not a list")
		return
	}
	annotation := attrs.Annotation
	if annotation == nil {
		annotation = &d2graph.Annotation{}
		attrs.Annotation = annotation
	}

	if primary := f.Primary(); primary != nil {
		text := primary.Value.ScalarString()
		if block, ok := primary.Value.(*d2ast.BlockString); ok {
			tag := block.Tag
			if full, ok := ShortToFullLanguageAliases[tag]; ok {
				tag = full
			}
			if tag != "markdown" {
				c.errorf(f.LastPrimaryKey(), "annotation text is Markdown, not %q", block.Tag)
				return
			}
			if strings.TrimSpace(text) == "" {
				c.errorf(f.LastPrimaryKey(), "block string cannot be empty")
				return
			}
		}
		rendered, err := textmeasure.RenderMarkdown(text)
		if err != nil {
			c.errorf(f.LastPrimaryKey(), "malformed Markdown")
			return
		}
		var parsed interface{}
		if err := xml.Unmarshal([]byte("<div>"+rendered+"</div>"), &parsed); err != nil {
			if syntax, ok := err.(*xml.SyntaxError); ok {
				c.errorf(f.LastPrimaryKey(), "malformed Markdown: %s", syntax.Msg)
			} else {
				c.errorf(f.LastPrimaryKey(), "malformed Markdown: %s", err.Error())
			}
			return
		}
		annotation.Label.Value = text
		annotation.Label.MapKey = f.LastPrimaryKey()
	}

	m := f.Map()
	if m == nil {
		return
	}
	for _, child := range m.Fields {
		if c.stopped() {
			return
		}
		keyword := d2ast.Keyword(child.Name)
		switch keyword {
		case "shape", "near":
			if child.Primary() == nil || child.Composite != nil {
				c.errorf(fieldErrorNode(child), `annotation %s must be a single value`, keyword)
				continue
			}
			scalar := child.Primary().Value
			if _, null := scalar.(*d2ast.Null); null {
				if keyword == "shape" {
					annotation.Shape = nil
				} else {
					annotation.Position = nil
				}
				continue
			}
			value := strings.ToLower(scalar.ScalarString())
			set := &d2graph.Scalar{Value: value, MapKey: child.LastPrimaryKey()}
			if keyword == "shape" {
				if _, ok := d2ast.AnnotationShapes[value]; !ok {
					hint := ""
					if d2target.IsShape(value) {
						hint = annotationNameHint
					}
					c.errorf(scalar, "unknown annotation shape %q, expected one of: %s%s", scalar.ScalarString(), strings.Join(d2ast.AnnotationShapesArray, ", "), hint)
					continue
				}
				annotation.Shape = set
				annotation.Group = ""
				if value == d2target.AnnotationLasso {
					annotation.Group = c.lassoGroup(f, child)
				}
			} else {
				if _, ok := d2ast.AnnotationPositions[value]; !ok {
					c.errorf(scalar, "invalid annotation near %q, expected one of: %s%s", scalar.ScalarString(), strings.Join(d2ast.AnnotationPositionsArray, ", "), annotationNameHint)
					continue
				}
				annotation.Position = set
			}
		case "style":
			if child.Map() == nil || len(child.Map().Fields) == 0 {
				c.errorf(child.LastRef().AST(), `"style" expected to be set to a map of key-values, or contain an additional keyword like "style.stroke: red"`)
				continue
			}
			for _, style := range child.Map().Fields {
				name := d2ast.Keyword(style.Name)
				if name != "stroke" && name != "font-color" {
					c.errorf(style.LastRef().AST(), `an annotation can only style its "stroke", the color of the pen, and its "font-color", the color of the text alone`)
					continue
				}
				if style.Primary() == nil {
					continue
				}
				set := &annotation.Stroke
				if name == "font-color" {
					set = &annotation.FontColor
				}
				scalar := style.Primary().Value
				if _, null := scalar.(*d2ast.Null); null {
					*set = nil
					continue
				}
				// A pen is one color. Both renderers draw a mark point by point,
				// where a gradient has no box to run across.
				if !color.ValidColor(scalar.ScalarString()) || color.IsGradient(scalar.ScalarString()) {
					c.errorf(scalar, `expected %q to be a valid named color ("orange") or a hex code ("#f0ff3a")`, name)
					continue
				}
				*set = &d2graph.Scalar{Value: scalar.ScalarString(), MapKey: style.LastPrimaryKey()}
			}
		default:
			var node d2ast.Node = fieldErrorNode(child)
			if child.LastPrimaryKey() != nil {
				node = child.LastPrimaryKey()
			}
			c.errorf(node, `unexpected field %s in annotation, expected shape, near or style%s`, child.Name.ScalarString(), annotationNameHint)
		}
	}
	if len(m.Edges) > 0 {
		c.errorf(fieldErrorNode(f), "unexpected edges in map")
	}
}

// validateAnnotations checks each mark against what it is drawn over. The
// same field is compiled for shapes, connections and the board, so the rules
// that differ between them are applied once the board is whole.
func (c *compiler) validateAnnotations(g *d2graph.Graph) {
	for _, obj := range g.Objects {
		if c.stopped() {
			return
		}
		annotation := obj.Annotation
		if annotation == nil {
			continue
		}
		if annotation.Shape != nil && annotation.Shape.Value == "trace" {
			c.errorf(annotation.Shape.MapKey, `annotation shape "trace" follows a connection; on a shape use circle, box, underline, brace, cross, highlight, arrow or lasso`)
		}
	}
	for _, edge := range g.Edges {
		if c.stopped() {
			return
		}
		annotation := edge.Annotation
		if annotation == nil || annotation.Shape == nil {
			continue
		}
		switch annotation.Shape.Value {
		case "trace", "cross", "arrow":
		case "highlight":
			if edge.Label.Value == "" {
				c.errorf(annotation.Shape.MapKey, `annotation shape "highlight" runs over a label, and this connection has none; use trace to mark its route`)
			}
		default:
			c.errorf(annotation.Shape.MapKey, `annotation shape %q marks a shape; on a connection use trace, cross, highlight or arrow`, annotation.Shape.Value)
		}
	}

	caption := g.Root.Annotation
	if caption == nil {
		return
	}
	if caption.Shape != nil {
		c.errorf(caption.Shape.MapKey, "a board's annotation is its caption and has no shape")
	}
	if caption.Position != nil {
		c.errorf(caption.Position.MapKey, "a board's annotation is its caption and is always placed below the diagram")
	}
	caption.Text, caption.Cues = resolveCaptionCues(g, caption.Label.Value)
}

// lassoGroup names what gave a shape its lasso when that was a class or a
// glob, so that every shape the same one reaches shares a loop. A lasso
// declared on the shape, imported, or spread from a variable is its own.
func (c *compiler) lassoGroup(annotation, shape *d2ir.Field) string {
	if n := len(c.classNames); n > 0 {
		return "class " + c.classNames[n-1]
	}
	for _, f := range []*d2ir.Field{shape, annotation} {
		if ref := f.LastRef(); ref != nil && ref.DueToGlob() {
			return "glob " + ref.AST().GetRange().String()
		}
	}
	return ""
}

// A caption points at an object by linking to its key: [the replica](db.r2).
// A key with a space in it goes in angle brackets, as Markdown asks of any
// destination: [the balancer](<load balancer>). Destinations that are not an
// object on this board stay ordinary links.
var captionLink = regexp.MustCompile(`\[([^\]\n]+)\]\((?:<([^<>\n]+)>|([^()\s<>]+))\)`)

func resolveCaptionCues(g *d2graph.Graph, markdown string) (text string, cues []string) {
	seen := make(map[string]struct{})
	text = captionLink.ReplaceAllStringFunc(markdown, func(link string) string {
		parts := captionLink.FindStringSubmatch(link)
		key, err := d2parser.ParseKey(parts[2] + parts[3])
		if err != nil {
			return link
		}
		obj, ok := g.Root.HasChild(d2graph.Key(key))
		if !ok {
			return link
		}
		id := obj.AbsID()
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			cues = append(cues, id)
		}
		return "**" + parts[1] + "**"
	})
	if len(cues) == 0 {
		return "", nil
	}
	return text, cues
}
