//go:build js && wasm

package codestyle

import (
	_ "embed"
	"strings"

	"github.com/alecthomas/chroma/v2"
)

// Chroma's styles package embeds and parses its entire registry at startup.
// D2.js only selects these two palettes for code labels.

//go:embed github.xml
var lightXML string

//go:embed catppuccin-mocha.xml
var darkXML string

var (
	lightStyle = chroma.MustNewXMLStyle(strings.NewReader(lightXML))
	darkStyle  = chroma.MustNewXMLStyle(strings.NewReader(darkXML))
)

func Get(name string) *chroma.Style {
	switch name {
	case Light:
		return lightStyle
	case Dark:
		return darkStyle
	default:
		return nil
	}
}
