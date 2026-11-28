//go:build !d2nomcp

package d2cli

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type guideInput struct {
	Topic string `json:"topic,omitempty"`
}

type checkInput struct {
	Source *string `json:"source,omitempty"`
	Path   *string `json:"path,omitempty"`
	Fmt    bool    `json:"fmt,omitempty"`
}

type renderInput struct {
	Source       *string `json:"source,omitempty"`
	Path         *string `json:"path,omitempty"`
	Format       string  `json:"format,omitempty"`
	Layout       string  `json:"layout,omitempty"`
	Theme        *int64  `json:"theme,omitempty"`
	Sketch       *bool   `json:"sketch,omitempty"`
	Target       string  `json:"target,omitempty"`
	MaxDimension *int    `json:"max_dimension,omitempty"`
}

func mustSchema(s string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic("d2cli: invalid MCP schema: " + err.Error())
	}
	return m
}

const guideInputSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "topic": {
      "type": "string",
      "enum": ["core", "reference"],
      "description": "core: syntax, house style and common mistakes. reference: every shape, style keyword, arrowhead, theme and layout engine. Default core."
    }
  }
}`

func guideTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "guide",
		Title:       "D2 language guide",
		Description: `Return the D2 language guide for the D2 version this server runs. Call it once before writing D2 if you have not read it in this conversation. Topic "core" covers syntax, house style and common mistakes. Topic "reference" lists every shape, style keyword, arrowhead, theme and layout engine.`,
		InputSchema: mustSchema(guideInputSchema),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)},
	}
}

const mcpSourceProperties = `    "source": {
      "type": "string",
      "description": "D2 source text. Pass exactly one of source and path."
    },
    "path": {
      "type": "string",
      "description": "Path to a .d2 file, absolute or relative to the server root. Available only when the server was started with --root. Pass exactly one of source and path."
    }`

const mcpDrawingProperties = `    "layout": {
      "type": "string",
      "enum": ["dagre", "elk", "tala"],
      "description": "Layout engine. Omit to use the diagram's own setting, which defaults to dagre. Use tala for software architecture diagrams with containers."
    },
    "theme": {
      "type": "integer",
      "description": "Theme ID. Omit to use the diagram's own setting. Call guide with topic reference for the list."
    },
    "sketch": {
      "type": "boolean",
      "description": "Hand-drawn look."
    }`

const checkInputSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
` + mcpSourceProperties + `,
    "fmt": {
      "type": "boolean",
      "description": "Also return the canonically formatted source. Default false."
    }
  }
}`

func checkTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "check",
		Title:       "Check D2 source",
		Description: `Check D2 source for errors without rendering. Fast. Returns each error with its line and column. Set fmt to true to also get the canonically formatted source.`,
		InputSchema: mustSchema(checkInputSchema),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)},
	}
}

const renderInputSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
` + mcpSourceProperties + `,
    "format": {
      "type": "string",
      "enum": ["png", "svg", "txt"],
      "description": "png (default) returns an image you can look at. txt returns a text drawing, the cheapest way to check structure. svg returns the full SVG source, which is large: ask for it only to save a file."
    },
` + mcpDrawingProperties + `,
    "target": {
      "type": "string",
      "description": "Board to render, for example layers.api or scenarios.failover. Omit for the root board."
    },
    "max_dimension": {
      "type": "integer",
      "description": "png only. Longest side of the image in pixels, from 256 to 4096. Default 1568."
    }
  }
}`

func renderTool(ui bool) *mcp.Tool {
	tool := &mcp.Tool{
		Name:        "render",
		Title:       "Render D2 diagram",
		Description: `Compile D2 source and return the rendered diagram. By default returns a PNG image you can look at. If the source has errors, returns them with line and column so you can fix the source and call render again. Always look at the result before telling the user the diagram is done: check that nothing overlaps, that text is readable, and that the flow reads in the direction you intended.`,
		InputSchema: mustSchema(renderInputSchema),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(true)},
	}
	if ui {
		tool.Meta = mcp.Meta{"ui": map[string]any{"resourceUri": mcpViewerURI}}
	}
	return tool
}

type motionInput struct {
	Source          *string `json:"source,omitempty"`
	Path            *string `json:"path,omitempty"`
	Layout          string  `json:"layout,omitempty"`
	Theme           *int64  `json:"theme,omitempty"`
	Sketch          *bool   `json:"sketch,omitempty"`
	AnimateInterval *int64  `json:"animate_interval,omitempty"`
	AnimateDuration *int64  `json:"animate_duration,omitempty"`
}

const motionInputSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
` + mcpSourceProperties + `,
` + mcpDrawingProperties + `,
    "animate_interval": {
      "type": "integer",
      "minimum": 0,
      "description": "Minimum milliseconds per board. Omit to use the source setting. The default is 1000 ms."
    },
    "animate_duration": {
      "type": "integer",
      "minimum": 0,
      "description": "Step transition duration in milliseconds. Omit to use the source setting. Zero disables interpolation."
    }
  }
}`

func storyboardTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "storyboard",
		Title:       "Review a D2 animation",
		Description: "Return one PNG contact sheet sampled from the animation timeline. Inspect the labeled frames in playback order. Frame metadata gives the board path and timestamp in milliseconds for each panel.",
		InputSchema: mustSchema(motionInputSchema),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(true)},
	}
}

func lintMotionTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "lint_motion",
		Title:       "Lint D2 motion",
		Description: "Review the animation timeline for motion and readability issues. Returns advisory diagnostics with board paths, source positions, rule codes, and suggestions. Use storyboard to inspect the result after editing.",
		InputSchema: mustSchema(motionInputSchema),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)},
	}
}

// The SDK lists tools alphabetically. Present the language guide first so
// discovery follows the same order as the server's workflow.
func mcpToolOrder(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		if list, ok := result.(*mcp.ListToolsResult); ok && err == nil {
			for i, tool := range list.Tools {
				if tool.Name == "guide" {
					copy(list.Tools[1:i+1], list.Tools[:i])
					list.Tools[0] = tool
					break
				}
			}
		}
		return result, err
	}
}
