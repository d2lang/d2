// Package svgframe reads the outer group emitted by d2svg for an animation.
package svgframe

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"math"
	"strings"
)

type Frame struct {
	MasterID string
	ViewBox  image.Rectangle
}

// Read inspects only the opening group. Icons inside a frame can have their
// own SVG viewports and identifiers.
func Read(fragment []byte) (Frame, bool) {
	token, err := xml.NewDecoder(bytes.NewReader(fragment)).Token()
	group, ok := token.(xml.StartElement)
	if err != nil || !ok || group.Name.Local != "g" {
		return Frame{}, false
	}
	var frame Frame
	var viewBox string
	for _, attr := range group.Attr {
		switch attr.Name.Local {
		case "class":
			frame.MasterID, _, _ = strings.Cut(attr.Value, " ")
		case "viewBox":
			viewBox = attr.Value
		}
	}
	var x, y, w, h int
	_, err = fmt.Sscanf(viewBox, "%d %d %d %d", &x, &y, &w, &h)
	if err != nil || w < 0 || h < 0 || x > math.MaxInt-w || y > math.MaxInt-h {
		return Frame{}, false
	}
	frame.ViewBox = image.Rect(x, y, x+w, y+h)
	return frame, true
}
