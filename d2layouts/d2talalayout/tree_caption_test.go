package d2talalayout_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestTreeCaptionsClearNeighboringRanksAndBranches(t *testing.T) {
	for _, direction := range []string{"down", "up", "left", "right"} {
		for _, side := range []string{"outside-right-center", "outside-left-top", "outside-bottom-center", "outside-top-right"} {
			t.Run(direction+"_"+side, func(t *testing.T) {
				d := compileRoutingDiagram(t, t.Context(), fmt.Sprintf(`direction: %s
Root: Intake schedule {width: 95; height: 64; label.near: %[2]s}
A: Main authorization decision {width: 50; height: 63; label.near: %[2]s}
B: Manual exception handling {width: 76; height: 53; label.near: %[2]s}
C: Confirm receipt {width: 62; height: 69; label.near: %[2]s}
D: Record exception {width: 58; height: 58; label.near: %[2]s}
E: Dispatch workflow {width: 87; height: 50; label.near: %[2]s}
Root -> A
Root -> B
Root -> C
A -> D
A -> E`, direction, side), 1, &d2svg.RenderOpts{})

				if len(d.Shapes) != 6 || len(d.Connections) != 5 {
					t.Fatalf("got %d nodes and %d edges", len(d.Shapes), len(d.Connections))
				}
				type bounds struct{ body, caption geo.Box }
				var boxes []bounds
				for _, s := range d.Shapes {
					body := geo.Box{TopLeft: geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), Width: float64(s.Width), Height: float64(s.Height)}
					pos := label.FromString(s.LabelPosition)
					if !pos.IsOutside() {
						t.Fatalf("lost explicit label on %s: %s", s.ID, s.LabelPosition)
					}
					w, h := float64(s.LabelWidth), float64(s.LabelHeight)
					caption := geo.Box{TopLeft: pos.GetPointOnBox(&body, label.PADDING, w, h), Width: w, Height: h}
					for i, other := range boxes {
						if caption.Overlaps(other.body) || body.Overlaps(other.caption) || caption.Overlaps(other.caption) {
							t.Fatalf("label collision between %s and %s", s.ID, d.Shapes[i].ID)
						}
					}
					boxes = append(boxes, bounds{body, caption})
				}
			})
		}
	}
}

func TestTreeSpacingClearsOpposingOutsideIcons(t *testing.T) {
	for _, direction := range []string{"down", "up", "right", "left"} {
		first, second := "right-center", "left-center"
		if direction == "right" || direction == "left" {
			first, second = "bottom-center", "top-center"
		}
		source := fmt.Sprintf(`direction: %s
Review -> Approve
Review -> Reject
Review -> Hold
Approve: {width: 96; height: 96; icon: status.svg; icon.near: outside-%s}
Reject: {width: 96; height: 96; icon: status.svg; icon.near: outside-%s}
Hold: {width: 96; height: 96; icon: status.svg; icon.near: outside-%s}`, direction, first, second, second)
		d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		var bodies, icons []geo.Box
		for _, s := range d.Shapes {
			body := geo.Box{TopLeft: geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), Width: float64(s.Width), Height: float64(s.Height)}
			bodies = append(bodies, body)
			if s.Icon != nil {
				size := float64(d2target.GetIconSize(&body, s.IconPosition))
				topLeft := label.FromString(s.IconPosition).GetPointOnBox(&body, label.PADDING, size, size)
				icons = append(icons, geo.Box{TopLeft: topLeft, Width: size, Height: size})
			}
		}
		if len(icons) != 3 {
			t.Fatalf("%s: got %d icons", direction, len(icons))
		}
		for i, icon := range icons {
			for _, body := range bodies {
				if icon.Overlaps(body) {
					t.Fatalf("%s: exterior icon overlaps a node", direction)
				}
			}
			for _, other := range icons[i+1:] {
				if icon.Overlaps(other) {
					t.Fatalf("%s: exterior icons overlap", direction)
				}
			}
		}
	}
}

func TestTreeVerticalBranchSpacingUsesUprightEndpointCaptions(t *testing.T) {
	for _, direction := range []string{"down", "up"} {
		height := func(annotation string) int {
			source := fmt.Sprintf(`direction: %s
Intake -> Review: {source-arrowhead: %s; target-arrowhead: %s}
Intake -> Audit
Intake -> Archive`, direction, annotation, annotation)
			d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
			var minTop, maxBottom = d.Shapes[0].Pos.Y, d.Shapes[0].Pos.Y + d.Shapes[0].Height
			for _, s := range d.Shapes {
				minTop = min(minTop, s.Pos.Y)
				maxBottom = max(maxBottom, s.Pos.Y+s.Height)
			}
			return maxBottom - minTop
		}
		if short, long := height("Sent"), height("Submitted for settlement"); short != long {
			t.Errorf("%s: one-line endpoint captions change the tree height from %d to %d", direction, short, long)
		}
	}
}

func TestTreeEndpointCaptionClearsTallerSibling(t *testing.T) {
	for _, direction := range []string{"down", "up"} {
		t.Run(direction, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), fmt.Sprintf(`direction: %s
Intake -> Review: {source-arrowhead: Confirmed by server; target-arrowhead: Submitted for settlement}
Intake -> Audit
Intake -> Archive
Audit: {width: 200; height: 160}`, direction), 1, nil)
			edge := d.Connections[0]
			if edge.Dst != "Review" || edge.DstLabel == nil {
				t.Fatal("missing Review endpoint caption")
			}
			caption := geo.NewBox(edge.GetArrowheadLabelPosition(true), float64(edge.DstLabel.LabelWidth), float64(edge.DstLabel.LabelHeight))
			for _, n := range d.Shapes {
				body := geo.NewBox(geo.NewPoint(float64(n.Pos.X), float64(n.Pos.Y)), float64(n.Width), float64(n.Height))
				if caption.Overlaps(*body) {
					t.Fatalf("Review caption overlaps %s", n.ID)
				}
			}
		})
	}
}

func TestTreeBranchEndCaptionsStayDistinct(t *testing.T) {
	for _, tc := range []struct {
		direction string
		end       string
	}{
		{"down", "target"},
		{"up", "source"},
	} {
		t.Run(tc.end, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), fmt.Sprintf(`direction: %s
Intake -> Review: {%[2]s-arrowhead: Approved for settlement}
Intake -> Audit: {%[2]s-arrowhead: Retained for compliance}
Intake -> Archive: {%[2]s-arrowhead: Stored for reconciliation}`, tc.direction, tc.end), 1, nil)
			var captions []*geo.Box
			for _, edge := range d.Connections {
				isTarget := tc.end == "target"
				text := edge.SrcLabel
				if isTarget {
					text = edge.DstLabel
				}
				if text == nil {
					t.Fatalf("missing %s caption on %s", tc.end, edge.ID)
				}
				box := geo.NewBox(edge.GetArrowheadLabelPosition(isTarget), float64(text.LabelWidth), float64(text.LabelHeight))
				for _, other := range captions {
					if box.Overlaps(*other) {
						t.Fatalf("caption on %s overlaps another branch caption", edge.ID)
					}
				}
				for _, node := range d.Shapes {
					body := geo.NewBox(geo.NewPoint(float64(node.Pos.X), float64(node.Pos.Y)), float64(node.Width), float64(node.Height))
					if box.Overlaps(*body) {
						t.Fatalf("caption on %s overlaps %s", edge.ID, node.ID)
					}
				}
				captions = append(captions, box)
			}
			for _, node := range d.Shapes[1:] {
				y := d.Shapes[0].Pos.Y
				if tc.direction == "up" && node.Pos.Y >= y || tc.direction == "down" && node.Pos.Y <= y {
					t.Fatalf("branch to %s runs against %s", node.ID, tc.direction)
				}
			}
		})
	}
}

func TestPinnedTreeJoinPlacesSourcesUpstream(t *testing.T) {
	for _, direction := range []string{"right", "left", "up", "down"} {
		t.Run(direction, func(t *testing.T) {
			shapes := hierarchyGeometry(t, fmt.Sprintf(`direction: %s
Publish: {left: 400; top: 400}
Verify -> Publish
Approve -> Publish
Archive -> Publish`, direction))
			root := shapes["Publish"]
			if root.Pos.X != 400 || root.Pos.Y != 400 {
				t.Fatalf("fixed destination moved to %v", root.Pos)
			}
			for _, id := range []string{"Verify", "Approve", "Archive"} {
				n := shapes[id]
				if direction == "right" && n.Pos.X+n.Width > root.Pos.X ||
					direction == "left" && n.Pos.X < root.Pos.X+root.Width ||
					direction == "down" && n.Pos.Y+n.Height > root.Pos.Y ||
					direction == "up" && n.Pos.Y < root.Pos.Y+root.Height {
					t.Errorf("%s is downstream of the fixed destination in direction %s", id, direction)
				}
			}
		})
	}
}

func TestAnnotatedTreeKeepsCaptionOutsideSource(t *testing.T) {
	const source = `direction: down
Hub -> Review: {source-arrowhead: Reconciled domestic payments}
Hub -> Audit: {source-arrowhead: Confirmed authorization window}
Hub -> Archive: {source-arrowhead: Records scheduled for archival}
Hub -> Export: {source-arrowhead: Outside institutional approval}
Hub -> Send: {source-arrowhead: Deferred pending verification}`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	hub := d.Shapes[0]
	body := geo.NewBox(geo.NewPoint(float64(hub.Pos.X), float64(hub.Pos.Y)), float64(hub.Width), float64(hub.Height))
	for _, edge := range d.Connections {
		caption := geo.NewBox(edge.GetArrowheadLabelPosition(false), float64(edge.SrcLabel.LabelWidth), float64(edge.SrcLabel.LabelHeight))
		if caption.Overlaps(*body) {
			t.Fatalf("%s source caption covers Hub", edge.ID)
		}
	}
}

func TestTreeFanKeepsRoutesWhenCaptionsOutnumberPorts(t *testing.T) {
	const leaves = 256
	var source strings.Builder
	source.WriteString("direction: down\n")
	for i := range leaves {
		fmt.Fprintf(&source, "Leaf%03d: \"\" {width: 8; height: 18}\n", i)
		if i%2 == 0 {
			fmt.Fprintf(&source, "Intake -> Leaf%03d: {source-arrowhead: Status %03d}\n", i, i)
		} else {
			fmt.Fprintf(&source, "Leaf%03d -> Intake: {target-arrowhead: Status %03d}\n", i, i)
		}
	}
	d := compileRoutingDiagram(t, t.Context(), source.String(), 1, nil)
	if len(d.Shapes) != leaves+1 || len(d.Connections) != leaves {
		t.Fatalf("got %d shapes and %d connections", len(d.Shapes), len(d.Connections))
	}
	for _, edge := range d.Connections {
		if len(edge.Route) < 2 || edge.Src == "Intake" && edge.SrcLabel == nil || edge.Dst == "Intake" && edge.DstLabel == nil {
			t.Fatalf("lost route or caption on %s", edge.ID)
		}
	}
}

func TestIncomingTreeFanRendersWithoutZeroLengthSegments(t *testing.T) {
	const source = `direction: down
Issue: {width: 190; height: 84}
Step0 -> Issue
Step1 -> Issue: {target-arrowhead: Review}
Step2 -> Issue: {source-arrowhead: Log; target-arrowhead: Log}
Step3 -> Issue
Step4 -> Issue: {source-arrowhead: Log; target-arrowhead: Log}
Detached`
	for _, seed := range []int64{1, 2, 3} {
		d := compileRoutingDiagram(t, t.Context(), source, seed, nil)
		if _, err := d2svg.Render(d, &d2svg.RenderOpts{}); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
	}
}

func TestTreePortSearchPreservesClearEndpointCaptions(t *testing.T) {
	const source = `direction: left
H: Main {width: 80; height: 75}
N0 <- H: {target-arrowhead: Next}
H -> N1: {source-arrowhead: Next}
N4 <- H
H -> N5
N6 <- H`
	const twoEdges = `direction: left
H: Main {width: 80; height: 155}
N0: Load {shape: document; width: 117; height: 64}
N0 <- H: {target-arrowhead: Hold for financial reconciliation}
N1: Job {width: 117; height: 64}
H -> N1: {source-arrowhead: Hold for financial reconciliation}`
	for _, input := range []string{source, source + "\nH.style.fill: red", twoEdges} {
		d := compileRoutingDiagram(t, t.Context(), input, 1, nil)
		var captions []geo.Box
		for _, e := range d.Connections {
			for _, target := range [...]bool{false, true} {
				end := e.SrcLabel
				if target {
					end = e.DstLabel
				}
				if end != nil {
					captions = append(captions, geo.Box{TopLeft: e.GetArrowheadLabelPosition(target), Width: float64(end.LabelWidth), Height: float64(end.LabelHeight)})
				}
			}
		}
		if len(captions) != 2 || captions[0].Overlaps(captions[1]) {
			t.Fatalf("the two visible endpoint captions should be separate, got %v", captions)
		}
		for _, shape := range d.Shapes {
			body := geo.NewBox(geo.NewPoint(float64(shape.Pos.X), float64(shape.Pos.Y)), float64(shape.Width), float64(shape.Height))
			for _, caption := range captions {
				if body.Overlaps(caption) {
					t.Fatalf("shape %q covers an endpoint caption", shape.ID)
				}
			}
		}
	}
}

func TestTreePortSearchPreservesStraightenedBranches(t *testing.T) {
	const source = `direction: right
H: Main {width: 80; height: 110}
N0: Task {width: 98; height: 75}
H -> N0
N1: Finish {width: 80; height: 50}
N1 <- H: {target-arrowhead: 3.1}
N2: Finish {width: 64; height: 50}
H -> N2: {source-arrowhead: 3.1}
N3: Finish {shape: oval; width: 80; height: 98}
H -> N3`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	var captions []geo.Box
	bends := 0
	for _, e := range d.Connections {
		if e.SrcLabel == nil && e.DstLabel == nil {
			continue
		}
		target := e.DstLabel != nil
		caption := e.SrcLabel
		if target {
			caption = e.DstLabel
		}
		captions = append(captions, *geo.NewBox(e.GetArrowheadLabelPosition(target), float64(caption.LabelWidth), float64(caption.LabelHeight)))
		bends += len(e.Route) - 2
	}
	if len(captions) != 2 || captions[0].Overlaps(captions[1]) || bends > 1 {
		t.Fatalf("wanted two readable captions and at most one bend, got %v and %d bends", captions, bends)
	}
}

func TestTreeFaceSeparatesWideCaptionsWithoutMovingChildren(t *testing.T) {
	const source = `direction: left
H: Hub {width: 126; height: 103}
N0: Review {width: 115; height: 116}
N0 <- H: {target-arrowhead: Cleared for processing}
N1: Review {width: 73; height: 64}
N1 <- H: {target-arrowhead: Cleared for processing; source-arrowhead: Received}
N2: Review {width: 73; height: 64}
H -> N2
N3: Review {width: 115; height: 64}
N3 <- H
N4: Review {width: 73; height: 48}
N4 <- H
N2 -> Audit`
	t.Run("collapsed whitespace", func(t *testing.T) {
		spaced := strings.ReplaceAll(source, "Cleared for processing", `"A`+strings.Repeat(" ", 21)+`B"`)
		diagram := compileRoutingDiagram(t, t.Context(), spaced, 1, nil)
		control := compileRoutingDiagram(t, t.Context(), spaced+"\nH.style.fill: red", 1, nil)
		if !reflect.DeepEqual(diagram.Connections, control.Connections) {
			t.Fatal("collapsed whitespace changed the tree ports")
		}
	})

	diagram := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	original := compileRoutingDiagram(t, t.Context(), source+"\nH.style.fill: red", 1, nil)
	tl, br := diagram.BoundingBox()
	oldTL, oldBR := original.BoundingBox()
	if tl.X < oldTL.X || tl.Y < oldTL.Y || br.X > oldBR.X || br.Y > oldBR.Y {
		t.Fatalf("changing a tree port expanded the canvas from %v..%v to %v..%v", oldTL, oldBR, tl, br)
	}
	var bodies []geo.Box
	for i, s := range diagram.Shapes {
		if s.Pos != original.Shapes[i].Pos {
			t.Fatalf("moved %s", s.ID)
		}
		bodies = append(bodies, *geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height)))
	}
	var ink []geo.Box
	for i, e := range diagram.Connections {
		old := original.Connections[i]
		if e.Src == "N0" || e.Src == "N1" {
			if *e.Route[0] != *old.Route[0] {
				t.Fatalf("changed the child attachment on %s", e.ID)
			}
		}
		for _, target := range [...]bool{false, true} {
			end := e.SrcLabel
			if target {
				end = e.DstLabel
			}
			if end == nil {
				continue
			}
			box := geo.NewBox(e.GetArrowheadLabelPosition(target), float64(end.LabelWidth), float64(end.LabelHeight))
			for _, other := range ink {
				if box.Overlaps(other) {
					t.Fatalf("endpoint captions overlap on %s", e.ID)
				}
			}
			for j, body := range bodies {
				if box.Overlaps(body) {
					t.Fatalf("caption on %s overlaps %s", e.ID, diagram.Shapes[j].ID)
				}
			}
			ink = append(ink, *box)
		}
	}
	if len(ink) != 3 {
		t.Fatalf("expected three endpoint captions, got %d", len(ink))
	}
}
