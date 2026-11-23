package d2cli

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestSourceRenderOptionsPrecedence(t *testing.T) {
	const source = "vars: {d2-config: {scale: 0.5; force-appendix: true}}\na: {tooltip: First release}"
	for _, tc := range []struct {
		name          string
		env, args     []string
		scale         float64
		forceAppendix bool
	}{
		{"source", nil, nil, .5, true},
		{"environment", []string{"SCALE=2", "D2_FORCE_APPENDIX=false"}, nil, 2, false},
		{"flag over environment", []string{"SCALE=2", "D2_FORCE_APPENDIX=false"}, []string{"--scale=.25", "--force-appendix"}, .25, true},
		{"flag resets source", nil, []string{"--scale=-1", "--force-appendix=false"}, -1, false},
		{"environment resets source scale", []string{"SCALE=-1"}, nil, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := isometricCLI(t, source, tc.env, append(tc.args, "--stdout-format=svg", "-")...)
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Contains(out, []byte(`class="appendix"`)); got != tc.forceAppendix {
				t.Fatalf("appendix = %t, want %t", got, tc.forceAppendix)
			}
			var svg struct {
				ViewBox string `xml:"viewBox,attr"`
				Width   int    `xml:"width,attr"`
				Height  int    `xml:"height,attr"`
			}
			if err := xml.Unmarshal(out, &svg); err != nil {
				t.Fatal(err)
			}
			if tc.scale == -1 {
				if svg.Width != 0 || svg.Height != 0 {
					t.Fatalf("fit-to-screen SVG has explicit size %dx%d", svg.Width, svg.Height)
				}
				return
			}
			var width, height int
			if _, err := fmt.Sscanf(svg.ViewBox, "0 0 %d %d", &width, &height); err != nil {
				t.Fatal(err)
			}
			if svg.Width != int(math.Ceil(float64(width)*tc.scale)) || svg.Height != int(math.Ceil(float64(height)*tc.scale)) {
				t.Fatalf("outer SVG %dx%d does not match viewBox %q at scale %v", svg.Width, svg.Height, svg.ViewBox, tc.scale)
			}
		})
	}
}

func TestSourceRenderOptionsPNG(t *testing.T) {
	const shape = "a: {tooltip: First release}"
	for _, tc := range []struct {
		name   string
		config string
		flags  []string
	}{
		{"ordinary", "scale: 0.2", nil},
		{"isometric", "scale: 0.2; isometric: true", []string{"--isometric"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "vars: {d2-config: {" + tc.config + "}}\n" + shape
			fromFile, err := imageCLI(t, t.TempDir(), source, "--stdout-format=png", "-")
			if err != nil {
				t.Fatal(err)
			}
			fromFlag, err := imageCLI(t, t.TempDir(), shape, append(tc.flags, "--scale=.2", "--stdout-format=png", "-")...)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(fromFile, fromFlag) {
				t.Fatal("source scaling differs from --scale in PNG")
			}
		})
	}
}

func TestIsometricSourceAppendix(t *testing.T) {
	const source = "vars: {d2-config: {isometric: true; force-appendix: true}}\na"
	if _, err := imageCLI(t, t.TempDir(), source, "--stdout-format=svg", "-"); err == nil || !strings.Contains(err.Error(), "--force-appendix") {
		t.Fatalf("got %v, want isometric appendix error", err)
	}
	if _, err := imageCLI(t, t.TempDir(), source, "--force-appendix=false", "--stdout-format=svg", "-"); err != nil {
		t.Fatalf("explicitly disabling the source appendix: %v", err)
	}
}
