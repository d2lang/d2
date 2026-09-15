package d2svg

import (
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/textmeasure"
)

// renderMeasurements belongs to one render operation. Only the ruler is shared
// between boards; generated Markdown corpora and legend dimensions remain local
// to each board. Initialization stays lazy for diagrams that need no ruler.
type renderMeasurements struct {
	ruler *textmeasure.Ruler
	err   error
}

func newRenderMeasurements(opts *RenderOpts) *renderMeasurements {
	m := &renderMeasurements{}
	if opts != nil {
		m.ruler = opts.Ruler
	}
	return m
}

func (m *renderMeasurements) getRuler() (*textmeasure.Ruler, error) {
	if m.ruler == nil && m.err == nil {
		m.ruler, m.err = textmeasure.NewRuler()
	}
	return m.ruler, m.err
}

type legendMeasurements struct {
	shapes        []d2target.TextDimensions
	connections   []d2target.TextDimensions
	totalHeight   int
	maxLabelWidth int
	itemCount     int
}

func measureLegend(legend *d2target.Legend, measurements *renderMeasurements) (*legendMeasurements, error) {
	ruler, err := measurements.getRuler()
	if err != nil {
		return nil, err
	}
	m := &legendMeasurements{
		shapes:      make([]d2target.TextDimensions, len(legend.Shapes)),
		connections: make([]d2target.TextDimensions, len(legend.Connections)),
		totalHeight: LEGEND_PADDING + LEGEND_FONT_SIZE + LEGEND_ITEM_SPACING,
	}
	measure := func(text, family string) d2target.TextDimensions {
		if text == "" {
			return d2target.TextDimensions{}
		}
		dims := d2graph.GetTextDimensions(nil, ruler, &d2target.MText{
			Text: text, FontSize: LEGEND_FONT_SIZE,
		}, fontToFamily(family))
		m.maxLabelWidth = max(m.maxLabelWidth, dims.Width)
		m.totalHeight += max(dims.Height, LEGEND_ICON_SIZE) + LEGEND_ITEM_SPACING
		m.itemCount++
		return *dims
	}
	for i, s := range legend.Shapes {
		m.shapes[i] = measure(s.Label, s.FontFamily)
	}
	for i, c := range legend.Connections {
		m.connections[i] = measure(c.Label, c.FontFamily)
	}
	if m.itemCount > 0 {
		m.totalHeight -= LEGEND_ITEM_SPACING / 2
	}
	return m, nil
}

func (m *legendMeasurements) height(hasConnections bool) int {
	height := m.totalHeight
	if m.itemCount > 0 && hasConnections {
		height += LEGEND_PADDING * 1.5
	} else {
		height += LEGEND_PADDING * 1.2
	}
	return height
}
