package d2cli

import (
	"encoding/xml"
	"fmt"
	"image"
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
	var doc struct {
		Width  int `xml:"width,attr"`
		Height int `xml:"height,attr"`
		Canvas struct {
			ViewBox string `xml:"viewBox,attr"`
			Frames  []struct {
				ViewBox    string  `xml:"viewBox,attr"`
				Groups     []group `xml:"g"`
				Background struct {
					X      int `xml:"x,attr"`
					Y      int `xml:"y,attr"`
					Width  int `xml:"width,attr"`
					Height int `xml:"height,attr"`
				} `xml:"rect"`
			} `xml:"g"`
		} `xml:"svg"`
	}
	require.NoError(t, xml.Unmarshal(out, &doc))
	parseBox := func(viewBox string) image.Rectangle {
		t.Helper()
		var x, y, w, h int
		_, err := fmt.Sscan(viewBox, &x, &y, &w, &h)
		require.NoError(t, err)
		return image.Rect(x, y, x+w, y+h)
	}

	canvas := parseBox(doc.Canvas.ViewBox)
	require.Equal(t, (canvas.Dx()+1)/2, doc.Width)
	require.Equal(t, (canvas.Dy()+1)/2, doc.Height)
	want := [][]string{nil, {"root"}, {tooltip, link}}
	require.Len(t, doc.Canvas.Frames, len(want))
	for i, frame := range doc.Canvas.Frames {
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
	require.Greater(t, parseBox(doc.Canvas.Frames[2].ViewBox).Dx(), parseBox(doc.Canvas.Frames[1].ViewBox).Dx())
}
