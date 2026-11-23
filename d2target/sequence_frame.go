package d2target

import "github.com/d2lang/d2/lib/geo"

// SequenceFrame returns an empty frame on this board's laid-out canvas.
// Sampled frames are rendered individually; their board hierarchy and sequence
// schedule remain on the original diagram.
func (d Diagram) SequenceFrame() Diagram {
	topLeft, bottomRight := d.BoundingBox()
	d.frameBounds = &[2]Point{topLeft, bottomRight}
	d.Shapes, d.Connections = nil, nil
	d.Layers, d.Scenarios, d.Steps = nil, nil, nil
	d.TweenFrames, d.TweenDurationMS = nil, 0
	d.SequenceTimelines = nil
	return d
}

type sequenceTrace struct {
	labelTopLeft *geo.Point
}

// WithTracedRoute shortens a sequence message without moving its caption.
func (c Connection) WithTracedRoute(route geo.Route) Connection {
	trace := &sequenceTrace{}
	if c.Label != "" {
		trace.labelTopLeft = c.GetLabelTopLeft()
	}
	c.sequenceTrace = trace
	c.Route = route
	return c
}

// CopySequenceTraceFrom preserves the transient trace metadata when a renderer
// deep-copies a connection through JSON. A trace can be in flight to a span
// that has yet to appear, even when the message has no label.
func (c *Connection) CopySequenceTraceFrom(src *Connection) bool {
	if src.sequenceTrace == nil {
		return false
	}
	c.sequenceTrace = &sequenceTrace{}
	if p := src.sequenceTrace.labelTopLeft; p != nil {
		c.sequenceTrace.labelTopLeft = p.Copy()
	}
	return true
}
