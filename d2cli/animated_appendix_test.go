package d2cli

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnimatedSVGAppendix(t *testing.T) {
	const tooltip = "A tooltip long enough to make this appendix wider than the diagram"
	const link = "https://example.com"
	source := `home: Home
scenarios: {
  detail: {back: Back {link: _}}
  notes: {note: Notes {tooltip: ` + tooltip + `; link: ` + link + `}}
}`
	dir := t.TempDir()
	_, err := imageCLI(t, dir, source, "--force-appendix", "--animate-interval=1000", "--pad=0", "--scale=.5", "animation.svg")
	require.NoError(t, err)
	out, err := os.ReadFile(filepath.Join(dir, "animation.svg"))
	require.NoError(t, err)

	type group struct {
		Class      string   `xml:"class,attr"`
		RowNumbers []string `xml:"g>text"`
		Texts      []struct {
			Y     float64 `xml:"y,attr"`
			Value string  `xml:",chardata"`
		} `xml:"text"`
	}
	type frame struct {
		ViewBox    string  `xml:"viewBox,attr"`
		Groups     []group `xml:"g"`
		Background struct {
			X      int `xml:"x,attr"`
			Y      int `xml:"y,attr"`
			Width  int `xml:"width,attr"`
			Height int `xml:"height,attr"`
		} `xml:"rect"`
	}
	var doc struct {
		Width  int `xml:"width,attr"`
		Height int `xml:"height,attr"`
		Canvas struct {
			ViewBox string `xml:"viewBox,attr"`
			Height  int    `xml:"height,attr"`
		} `xml:"svg"`
		Progress struct {
			Y      int `xml:"y,attr"`
			Height int `xml:"height,attr"`
		} `xml:"rect"`
	}
	require.NoError(t, xml.Unmarshal(out, &doc))
	// A navigable board has camera and placement groups around its artwork.
	// Locate the rendered frame by its viewBox in either SVG structure.
	var frames []frame
	decoder := xml.NewDecoder(bytes.NewReader(out))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if el, ok := token.(xml.StartElement); ok && el.Name.Local == "g" {
			for _, attr := range el.Attr {
				if attr.Name.Local == "viewBox" {
					var f frame
					require.NoError(t, decoder.DecodeElement(&f, &el))
					frames = append(frames, f)
					break
				}
			}
		}
	}
	parseBox := func(viewBox string) image.Rectangle {
		t.Helper()
		var x, y, w, h int
		_, err := fmt.Sscan(viewBox, &x, &y, &w, &h)
		require.NoError(t, err)
		return image.Rect(x, y, x+w, y+h)
	}

	canvas := parseBox(doc.Canvas.ViewBox)
	require.Equal(t, (canvas.Dx()+1)/2, doc.Width)
	require.Equal(t, (canvas.Dy()+1)/2, doc.Canvas.Height)
	require.Equal(t, doc.Canvas.Height+8, doc.Progress.Y)
	require.Equal(t, doc.Progress.Y+doc.Progress.Height, doc.Height)
	want := [][]string{nil, {"root"}, {tooltip, link}}
	require.Len(t, frames, len(want))
	for i, frame := range frames {
		box := parseBox(frame.ViewBox)
		require.True(t, box.In(canvas), "frame %d extends beyond the canvas", i)
		bg := frame.Background
		require.Equal(t, box, image.Rect(bg.X, bg.Y, bg.X+bg.Width, bg.Y+bg.Height), "frame %d background", i)
		var badges, rowNumbers, labels []string
		for _, g := range frame.Groups {
			switch g.Class {
			case "appendix-icon":
				require.Len(t, g.Texts, 1)
				badges = append(badges, g.Texts[0].Value)
			case "appendix":
				rowNumbers = append(rowNumbers, g.RowNumbers...)
				for _, text := range g.Texts {
					require.Less(t, text.Y, float64(box.Max.Y), "frame %d clips appendix text", i)
					labels = append(labels, text.Value)
				}
			}
		}
		require.Equal(t, want[i], labels, "frame %d appendix", i)
		var numbers []string
		for j := range want[i] {
			numbers = append(numbers, strconv.Itoa(j+1))
		}
		require.Equal(t, numbers, badges, "frame %d diagram badges", i)
		require.Equal(t, numbers, rowNumbers, "frame %d appendix numbers", i)
	}
	require.Greater(t, parseBox(frames[2].ViewBox).Dx(), parseBox(frames[1].ViewBox).Dx())
}

func TestLayerZoomPinnedSVGAppendix(t *testing.T) {
	const source = `vars: {d2-config: {layout-engine: tala; animate-interval: 1s; animate-duration: 500ms}}
portal: Gateway {left: 500; top: 600; link: layers.inner}
layers: {inner: {back: Back {left: 9000; top: 6400; link: root}}}`
	out, err := imageCLI(t, t.TempDir(), source, "--force-appendix=true", "--stdout-format=svg", "-")
	require.NoError(t, err)
	var doc struct {
		ViewBox string `xml:"viewBox,attr"`
	}
	require.NoError(t, xml.Unmarshal(out, &doc))
	var x, y, w, h int
	_, err = fmt.Sscan(doc.ViewBox, &x, &y, &w, &h)
	require.NoError(t, err)
	require.Less(t, w, 700, "rows pinned to world X=0 expand each layer by thousands of pixels")
	require.Less(t, h, 700)
	require.Equal(t, 2, bytes.Count(out, []byte(`<g class="appendix" transform="translate(`)))
}
