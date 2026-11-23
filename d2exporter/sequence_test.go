package d2exporter_test

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/go2"
	"github.com/stretchr/testify/require"
)

func TestSequenceTimelines(t *testing.T) {
	const source = `
outer: {
  checkout: {
    shape: sequence_diagram
    client
    api
    client -> api.request.inner: Place order
    group: {
      api.request.inner -> api.request.inner: Validate
      client <- api.request: Order confirmed
    }
  }
  audit: {
    shape: sequence_diagram
    sender -> journal: Record order
  }
}
outside -> outer.checkout
`
	ruler, err := textmeasure.NewRuler()
	require.NoError(t, err)
	target, graph, err := d2lib.Compile(context.Background(), source, &d2lib.CompileOptions{
		Ruler: ruler, Layout: go2.Pointer("dagre"), LayoutResolver: layoutResolver,
	}, nil)
	require.NoError(t, err)
	require.Empty(t, target.SequenceTimelines, "static export must preserve the existing target")
	timelines := d2exporter.SequenceTimelines(graph)
	require.Len(t, timelines, 2)
	labels := map[string]string{}
	for _, c := range target.Connections {
		labels[c.ID] = c.Label
	}
	require.Len(t, timelines[0].Messages, 3)
	require.Len(t, timelines[1].Messages, 1)
	var ordered []string
	for _, timeline := range timelines {
		for _, m := range timeline.Messages {
			ordered = append(ordered, labels[m.ConnectionID])
		}
	}
	require.Equal(t, []string{"Place order", "Validate", "Order confirmed", "Record order"}, ordered)
	require.True(t, timelines[0].Messages[2].Reverse)
	require.ElementsMatch(t, []d2target.SequenceSpan{
		{ShapeID: "outer.checkout.api.request", Start: d2target.SequenceMoment{Message: 0, Arrival: true}, End: d2target.SequenceMoment{Message: 2}},
		{ShapeID: "outer.checkout.api.request.inner", Start: d2target.SequenceMoment{Message: 0, Arrival: true}, End: d2target.SequenceMoment{Message: 1, Arrival: true}},
	}, timelines[0].Spans)
	target.SequenceTimelines = timelines
	require.NoError(t, d2target.ValidateRenderTarget(target))
	target.SequenceTimelines[0].Spans[0].End.Message = -1
	require.Error(t, d2target.ValidateRenderTarget(target))
}
