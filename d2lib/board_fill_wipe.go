package d2lib

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2grid"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
)

// A hard stop in a native gradient moves the fill beneath the existing label
// and outline. Adjacent grid cells share a front measured in layout pixels.
type boardFillWipes struct {
	from  *d2target.Diagram
	cells map[string]*fillWipe
}

type fillWipe struct {
	from, to, direction string
	start, end          int // Coordinates along the direction of travel.
	bandStart, bandEnd  int
}

func planBoardFillWipes(prev, next *d2target.Diagram, graph *d2graph.Graph) *boardFillWipes {
	if prev == nil || prev.IsFolderOnly || next.IsFolderOnly || graph == nil {
		return nil
	}
	var oldShapes, newShapes map[string]*d2target.Shape
	var wipes *boardFillWipes
	type band struct {
		parent      *d2graph.Object
		direction   string
		cross, size int // Only cells sharing both side edges are aligned.
	}
	var bands map[band][]*fillWipe
	for _, obj := range graph.Objects {
		style := obj.Style.FillTransition
		if style == nil || style.Value == "blend" {
			continue
		}
		if newShapes == nil {
			oldShapes, newShapes = gridEdgeShapeMap(prev.Shapes), gridEdgeShapeMap(next.Shapes)
		}
		id := obj.AbsID()
		a, b := oldShapes[id], newShapes[id]
		if a == nil || b == nil || a.Type != b.Type || !sameSlot(a, b) || b.Width <= 0 || b.Height <= 0 ||
			!a.CanShadeSketchFill() || !b.CanShadeSketchFill() || a.Fill == b.Fill {
			continue
		}
		// Theme tokens are resolved at playback (including dark mode), and a
		// gradient color stop must be an explicit CSS color.
		oldColor, oldOK := color.ParseRGBA(a.Fill)
		newColor, newOK := color.ParseRGBA(b.Fill)
		if !oldOK || !newOK || oldColor == newColor {
			continue
		}
		start, length, cross, size := b.Pos.X, b.Width, b.Pos.Y, b.Height
		if style.Value == "wipe-up" || style.Value == "wipe-down" {
			start, length, cross, size = cross, size, start, length
		}
		if style.Value == "wipe-left" || style.Value == "wipe-up" {
			start = -start - length
		}
		w := &fillWipe{from: a.Fill, to: b.Fill, direction: style.Value,
			start: start, end: start + length, bandStart: start, bandEnd: start + length}
		if wipes == nil {
			wipes = &boardFillWipes{from: prev, cells: make(map[string]*fillWipe)}
		}
		wipes.cells[id] = w
		if obj.Parent.IsGridDiagram() {
			if bands == nil {
				bands = make(map[band][]*fillWipe)
			}
			key := band{obj.Parent, style.Value, cross, size}
			bands[key] = append(bands[key], w)
		}
	}
	for key, cells := range bands {
		if len(cells) < 2 {
			continue
		}
		gap := key.parent.HorizontalGap
		if key.direction == "wipe-up" || key.direction == "wipe-down" {
			gap = key.parent.VerticalGap
		}
		if gap == nil {
			gap = key.parent.GridGap
		}
		spacing := d2grid.DEFAULT_GAP
		if gap != nil {
			spacing, _ = strconv.Atoi(gap.Value)
		}
		slices.SortFunc(cells, func(a, b *fillWipe) int { return cmp.Compare(a.start, b.start) })
		for first := 0; first < len(cells); {
			end := first + 1
			for ; end < len(cells); end++ {
				a, b := cells[end-1], cells[end]
				// Allow integer rounding of layout coordinates. An intervening
				// cell, an overlap or a deliberately wide gap starts a new run.
				distance := b.start - a.end
				if distance < 0 || distance > min(spacing+1, a.end-a.start, b.end-b.start) {
					break
				}
			}
			for _, w := range cells[first:end] {
				w.bandStart, w.bandEnd = cells[first].start, cells[end-1].end
			}
			first = end
		}
	}
	return wipes
}

func (p *boardFillWipes) apply(frame *d2target.Diagram, t float64) {
	// A roller accelerates and slows once across the whole row.
	t = t * t * (3 - 2*t)
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		w := p.cells[s.ID]
		if w == nil {
			continue
		}
		front := float64(w.bandStart) + t*float64(w.bandEnd-w.bandStart)
		switch {
		case front <= float64(w.start):
			s.Fill = w.from
		case front >= float64(w.end):
			s.Fill = w.to
		default:
			offset := 100 * (front - float64(w.start)) / float64(w.end-w.start)
			direction := "right"
			switch w.direction {
			case "wipe-up":
				direction = "top"
			case "wipe-down":
				direction = "bottom"
			case "wipe-left":
				direction = "left"
			}
			s.Fill = fmt.Sprintf("linear-gradient(to %s, %s %.4f%%, %s %.4f%%)", direction, w.to, offset, w.from, offset)
		}
	}
}
