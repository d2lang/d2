//go:build js && wasm

package d2wasm

import (
	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
)

type WASMResponse struct {
	Data  interface{} `json:"data,omitempty"`
	Error *WASMError  `json:"error,omitempty"`
}

type WASMError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *WASMError) Error() string {
	return e.Message
}

type RefRangesResponse struct {
	Ranges       []d2ast.Range `json:"ranges"`
	ImportRanges []d2ast.Range `json:"importRanges"`
}

type BoardPositionResponse struct {
	BoardPath []string `json:"boardPath"`
	Err       string   `json:"err"`
}

type CompileRequest struct {
	FS        map[string]string `json:"fs"`
	InputPath *string           `json:"inputPath"`
	Opts      *CompileOptions   `json:"options"`
}

type RenderOptions struct {
	Pad                *int64                   `json:"pad"`
	Sketch             *bool                    `json:"sketch"`
	Center             *bool                    `json:"center"`
	ThemeID            *int64                   `json:"themeID"`
	DarkThemeID        *int64                   `json:"darkThemeID"`
	ThemeOverrides     *d2target.ThemeOverrides `json:"themeOverrides"`
	DarkThemeOverrides *d2target.ThemeOverrides `json:"darkThemeOverrides"`
	Scale              *float64                 `json:"scale"`
	ForceAppendix      *bool                    `json:"forceAppendix"`
	Target             *string                  `json:"target"`
	AnimateInterval    *int64                   `json:"animateInterval"`
	Salt               *string                  `json:"salt"`
	NoXMLTag           *bool                    `json:"noXMLTag"`
	ASCII              *bool                    `json:"ascii"`
	ASCIIMode          *string                  `json:"asciiMode"`
}

type CompileOptions struct {
	RenderOptions
	AnimateDuration *int64  `json:"animateDuration"`
	Layout          *string `json:"layout"`
	FontRegular     *[]byte `json:"FontRegular"`
	FontItalic      *[]byte `json:"FontItalic"`
	FontBold        *[]byte `json:"FontBold"`
	FontSemibold    *[]byte `json:"FontSemibold"`
}

type CompileResponse struct {
	FS            map[string]string `json:"fs"`
	InputPath     string            `json:"inputPath"`
	Diagram       d2target.Diagram  `json:"diagram"`
	Graph         d2graph.Graph     `json:"graph"`
	RenderOptions RenderOptions     `json:"renderOptions"`
}

type CompletionResponse struct {
	Items []map[string]interface{} `json:"items"`
}

type RenderRequest struct {
	fonts   *customFonts
	Diagram *d2target.Diagram `json:"diagram"`
	Opts    *RenderOptions    `json:"options"`
}

// compiledDiagram carries the source font faces once at the root of the WASM
// response. The underlying target and its SVG hash contain only the family name.
type compiledDiagram struct {
	d2target.Diagram
	CustomFonts *customFonts `json:"customFonts,omitempty"`
}

type customFonts struct {
	Regular  []byte `json:"regular,omitempty"`
	Italic   []byte `json:"italic,omitempty"`
	Bold     []byte `json:"bold,omitempty"`
	Semibold []byte `json:"semibold,omitempty"`
}
