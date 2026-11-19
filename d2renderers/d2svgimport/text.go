package d2svgimport

import (
	"fmt"
	"math"
	"strings"

	"golang.org/x/image/math/fixed"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/internal/fontface"
)

type svgTextFonts struct {
	faces   map[d2fonts.FontFamily]*fontface.ParsedFace
	shaping fontface.ShapingWorkspace
}

func (i *svgImporter) frozenText(el *svgElement) bool {
	return i.mathJaxRoot != nil || el.metadata[frozenMathJaxVariantMetadata] != ""
}

// Text with explicitly positioned spans needs no shared cursor between runs.
// Reject flowing spans until their SVG positioning can be preserved.
func (i *svgImporter) validatePositionedText(el *svgElement) error {
	if el.name == "tspan" && (el.parent == nil || el.parent.name != "text" || el.attrs["x"] == "" || el.attrs["y"] == "") {
		return i.errorf("a <tspan> must be an explicitly positioned child of <text>")
	}
	for _, child := range el.children {
		if isNonRenderingElement(child.name) {
			continue
		}
		if el.name != "text" || child.name != "tspan" || strings.TrimSpace(string(el.text)) != "" {
			return i.errorf("mixed or flowing SVG text is unsupported")
		}
	}
	return nil
}

func (fonts *svgTextFonts) face(families string) (*fontface.ParsedFace, error) {
	family := d2fonts.SourceSansPro
	for part := range strings.SplitSeq(families, ",") {
		name := strings.Trim(strings.TrimSpace(part), "\"'")
		switch strings.ToLower(name) {
		case "sourcesanspro", "source sans pro", "sans-serif":
			family = d2fonts.SourceSansPro
		case "sourcecodepro", "source code pro", "monospace":
			family = d2fonts.SourceCodePro
		case "handdrawn", "hand drawn", "cursive":
			family = d2fonts.HandDrawn
		default:
			continue
		}
		break
	}
	if face := fonts.faces[family]; face != nil {
		return face, nil
	}
	data, ok := d2fonts.FontFaces.Lookup(d2fonts.Font{Family: family, Style: d2fonts.FONT_STYLE_REGULAR})
	if !ok {
		return nil, fmt.Errorf("bundled SVG text face is unavailable")
	}
	source, ok, err := fontface.RegisteredBundledFace(data, 0)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("bundled SVG text face is unavailable")
	}
	face, err := source.CloneReadOnly()
	if err == nil {
		fonts.faces[family] = face
	}
	return face, err
}

func (i *svgImporter) textGeometry(el *svgElement, style svgStyle) (svgGeometry, error) {
	if !style.visible || style.fontSize == 0 || len(el.text) == 0 {
		return svgGeometry{}, nil
	}
	raw := string(el.text)
	preserve := false
	for parent := el; parent != nil; parent = parent.parent {
		if value, ok := parent.metadata["xml:space"]; ok {
			preserve = value == "preserve"
			break
		}
	}
	if preserve {
		raw = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(raw)
	} else {
		raw = strings.Join(strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }), " ")
	}
	if raw == "" {
		return svgGeometry{}, nil
	}
	x, err := i.lengthAttribute(el, "x", 0, false)
	if err != nil {
		return svgGeometry{}, err
	}
	y, err := i.lengthAttribute(el, "y", 0, false)
	if err != nil {
		return svgGeometry{}, err
	}
	if i.textFonts == nil {
		i.textFonts = &svgTextFonts{faces: make(map[d2fonts.FontFamily]*fontface.ParsedFace)}
	}
	face, err := i.textFonts.face(style.fontFamily)
	if err != nil {
		return svgGeometry{}, err
	}
	ppem := fixed.Int26_6(math.Round(style.fontSize * 64))
	if ppem == 0 {
		return svgGeometry{}, nil
	}
	limit := min(4096, i.limits.MaxPathCommands-i.shapedRunes)
	if limit <= 0 {
		return svgGeometry{}, i.errorf("text shaping exceeds import budget")
	}
	shaped, err := i.textFonts.shaping.ShapeTextTransient(i.ctx, raw, ppem, []fontface.ShapeFace{{ID: "SVG text", Face: face}}, fontface.ShapeLimits{Runes: limit, Faces: 1, Runs: limit, Glyphs: limit, CoverageChecks: int64(limit) * 2})
	if err != nil {
		return svgGeometry{}, i.errorf("SVG text shaping: %v", err)
	}
	i.shapedRunes += shaped.Runes
	var commands []d2scene.PathCommand
	for _, glyph := range shaped.Glyphs {
		if err := i.ctx.Err(); err != nil {
			return svgGeometry{}, err
		}
		if glyph.Empty || !glyph.HasInk {
			continue
		}
		path, err := fontface.GlyphPath(face, glyph.ID, ppem, d2scene.Point{X: x + glyph.PositionX, Y: y + glyph.PositionY})
		if err != nil {
			return svgGeometry{}, err
		}
		if len(path.Commands) > i.limits.MaxPathCommands-i.emittedCommands-len(commands) {
			return svgGeometry{}, i.errorf("glyph outlines exceed import path command limit")
		}
		commands = append(commands, path.Commands...)
	}
	return svgGeometry{kind: geometryPath, commands: commands}, nil
}
