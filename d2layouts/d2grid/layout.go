package d2grid

import (
	"context"
	"math"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
	"github.com/d2lang/util-go/go2"
)

const (
	CONTAINER_PADDING = 60
	DEFAULT_GAP       = 40
)

// Layout places the grid and routes connections using straight joins between cells.
func Layout(ctx context.Context, g *d2graph.Graph) error {
	fit, err := PlaceCells(ctx, g, RouteEdges)
	if err != nil {
		return err
	}
	fit(nil)
	return nil
}

// PlaceCells positions the cells and routes their direct edges. After nested
// diagrams have been restored and their cross-cell edges routed, call the
// returned function with those edges to size the container around the result.
func PlaceCells(ctx context.Context, g *d2graph.Graph, router d2graph.RouteEdges) (fit func([]*d2graph.Edge), err error) {
	obj := g.Root
	gd, err := layoutGrid(g, obj)
	if err != nil {
		return nil, err
	}

	var direct []*d2graph.Edge
	for _, e := range g.Edges {
		if e.Src.Parent == obj && e.Dst.Parent == obj {
			direct = append(direct, e)
		}
	}
	if len(direct) != 0 {
		if router == nil {
			router = RouteEdges
		}
		if err := gd.routeCells(ctx, g, direct, router); err != nil {
			return nil, err
		}
	}
	return func(cross []*d2graph.Edge) {
		// All child diagrams now use this grid's coordinate system. Shift their
		// routes with their owners when adding container padding or label space.
		gd.edges = g.Edges
		if g.RootLevel > 0 {
			gd.fitRoutes(append(direct, cross...))
		}
		gd.fitContainer(g)
	}, nil
}

func (gd *gridDiagram) fitContainer(g *d2graph.Graph) {
	obj := g.Root
	if obj.HasLabel() && obj.LabelPosition == nil {
		obj.LabelPosition = go2.Pointer(label.InsideTopCenter.String())
	}
	if obj.Icon != nil && obj.IconPosition == nil {
		obj.IconPosition = go2.Pointer(label.InsideTopLeft.String())
	}

	if obj.Box != nil {
		// CONTAINER_PADDING is default, but use gap value if set
		horizontalPadding, verticalPadding := CONTAINER_PADDING, CONTAINER_PADDING
		if obj.GridGap != nil || obj.HorizontalGap != nil {
			horizontalPadding = gd.horizontalGap
		}
		if obj.GridGap != nil || obj.VerticalGap != nil {
			verticalPadding = gd.verticalGap
		}

		contentWidth, contentHeight := gd.width, gd.height

		var labelPosition, iconPosition label.Position
		if obj.LabelPosition != nil {
			labelPosition = label.FromString(*obj.LabelPosition)
		}
		if obj.IconPosition != nil {
			iconPosition = label.FromString(*obj.IconPosition)
		}

		// compute how much space the label and icon occupy
		_, padding := obj.Spacing()

		var labelWidth, labelHeight float64
		if obj.LabelDimensions.Width > 0 {
			labelWidth = float64(obj.LabelDimensions.Width) + 2*label.PADDING
		}
		if obj.LabelDimensions.Height > 0 {
			labelHeight = float64(obj.LabelDimensions.Height) + 2*label.PADDING
		}

		labelPosition.ReserveBorderHalf(&padding, labelWidth, labelHeight)
		if obj.HasIcon() {
			iconPosition.ReserveBorderHalf(&padding, d2target.MAX_ICON_SIZE, d2target.MAX_ICON_SIZE)
		}
		if labelWidth > 0 {
			switch labelPosition {
			case label.OutsideTopLeft, label.OutsideTopCenter, label.OutsideTopRight,
				label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight,
				label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight,
				label.OutsideBottomLeft, label.OutsideBottomCenter, label.OutsideBottomRight,
				label.BorderTopLeft, label.BorderTopCenter, label.BorderTopRight,
				label.BorderBottomLeft, label.BorderBottomCenter, label.BorderBottomRight:
				overflow := labelWidth - contentWidth
				if overflow > 0 {
					padding.Left += overflow / 2
					padding.Right += overflow / 2
				}
			}
		}
		if labelHeight > 0 {
			switch labelPosition {
			case label.OutsideLeftTop, label.OutsideLeftMiddle, label.OutsideLeftBottom,
				label.InsideMiddleLeft, label.InsideMiddleCenter, label.InsideMiddleRight,
				label.OutsideRightTop, label.OutsideRightMiddle, label.OutsideRightBottom,
				label.BorderLeftTop, label.BorderLeftMiddle, label.BorderLeftBottom,
				label.BorderRightTop, label.BorderRightMiddle, label.BorderRightBottom:
				overflow := labelHeight - contentHeight
				if overflow > 0 {
					padding.Top += overflow / 2
					padding.Bottom += overflow / 2
				}
			}
		}

		// configure spacing for default label+icon
		if iconPosition == label.InsideTopLeft && labelPosition == label.InsideTopCenter {
			// . ├────┤───────├────┤
			// .  icon  label  icon
			// with an icon in top left we need 2x the space to fit the label in the center
			iconSize := float64(d2target.MAX_ICON_SIZE) + 2*label.PADDING
			padding.Left = math.Max(padding.Left, iconSize)
			padding.Right = math.Max(padding.Right, iconSize)
			minWidth := 2*iconSize + float64(obj.LabelDimensions.Width) + 2*label.PADDING
			overflow := minWidth - contentWidth
			if overflow > 0 {
				padding.Left = math.Max(padding.Left, overflow/2)
				padding.Right = math.Max(padding.Right, overflow/2)
			}
		}

		padding.Top = math.Max(padding.Top, float64(verticalPadding))
		padding.Bottom = math.Max(padding.Bottom, float64(verticalPadding))
		padding.Left = math.Max(padding.Left, float64(horizontalPadding))
		padding.Right = math.Max(padding.Right, float64(horizontalPadding))

		shapeType := d2target.DSL_SHAPE_TO_SHAPE_TYPE[strings.ToLower(obj.Shape.Value)]
		shape.ReserveGridPadding(shapeType, contentHeight, &padding)

		totalWidth := padding.Left + contentWidth + padding.Right
		totalHeight := padding.Top + contentHeight + padding.Bottom
		obj.SizeToContent(totalWidth, totalHeight, 0, 0)

		// compute where the grid should be placed inside shape
		innerBox := obj.LayoutInnerBox()
		innerTL := innerBox.TopLeft
		// depending on the shape innerBox may be larger than totalWidth, totalHeight
		// if this is the case, we want to center the cells within the larger innerBox
		var resizeDx, resizeDy float64
		if innerBox.Width > totalWidth {
			resizeDx = (innerBox.Width - totalWidth) / 2
		}
		if innerBox.Height > totalHeight {
			resizeDy = (innerBox.Height - totalHeight) / 2
		}

		// move from horizontalPadding,verticalPadding to innerTL.X+padding.Left, innerTL.Y+padding.Top
		// and if innerBox is larger than content dimensions, adjust to center within innerBox
		dx := -float64(horizontalPadding) + innerTL.X + padding.Left + resizeDx
		dy := -float64(verticalPadding) + innerTL.Y + padding.Top + resizeDy
		if dx != 0 || dy != 0 {
			gd.shift(dx, dy)
		}
	}

	if g.Root.IsGridDiagram() && len(g.Root.ChildrenArray) != 0 {
		g.Root.TopLeft = geo.NewPoint(0, 0)
	}

	if g.RootLevel > 0 {
		horizontalPadding, verticalPadding := CONTAINER_PADDING, CONTAINER_PADDING
		if obj.GridGap != nil || obj.HorizontalGap != nil {
			horizontalPadding = gd.horizontalGap
		}
		if obj.GridGap != nil || obj.VerticalGap != nil {
			verticalPadding = gd.verticalGap
		}

		// shift the grid from (0, 0)
		gd.shift(
			obj.TopLeft.X+float64(horizontalPadding),
			obj.TopLeft.Y+float64(verticalPadding),
		)
	}
}

func layoutGrid(g *d2graph.Graph, obj *d2graph.Object) (*gridDiagram, error) {
	gd, err := newGridDiagram(obj)
	if err != nil {
		return nil, err
	}

	// position labels and icons
	for _, o := range gd.objects {
		positionedLabel := false
		if o.Icon != nil && o.IconPosition == nil {
			if len(o.ChildrenArray) > 0 {
				o.IconPosition = go2.Pointer(label.OutsideTopLeft.String())
				// don't overwrite position if nested graph layout positioned label/icon
				if o.LabelPosition == nil {
					o.LabelPosition = go2.Pointer(label.OutsideTopRight.String())
					positionedLabel = true
				}
			} else {
				o.IconPosition = go2.Pointer(label.InsideMiddleCenter.String())
			}
		}
		if !positionedLabel && o.HasLabel() && o.LabelPosition == nil {
			if len(o.ChildrenArray) > 0 {
				o.LabelPosition = go2.Pointer(label.OutsideTopCenter.String())
			} else if o.HasOutsideBottomLabel() {
				o.LabelPosition = go2.Pointer(label.OutsideBottomCenter.String())
			} else if o.Icon != nil && (o.IconPosition == nil || !label.FromString(*o.IconPosition).IsOutside()) {
				o.LabelPosition = go2.Pointer(label.InsideTopCenter.String())
			} else {
				o.LabelPosition = go2.Pointer(label.InsideMiddleCenter.String())
			}
		}
	}

	// to handle objects with outside labels, we adjust their dimensions before layout and
	// after layout, we remove the label adjustment and reposition TopLeft if needed
	placeWithinCells := gd.reserveCellMargins()

	if gd.rows != 0 && gd.columns != 0 {
		gd.layoutEvenly(g, obj)
	} else {
		gd.layoutDynamic(g, obj)
	}

	placeWithinCells()

	return gd, nil
}

func (gd *gridDiagram) layoutEvenly(g *d2graph.Graph, obj *d2graph.Object) {
	// layout objects in a grid with these 2 properties:
	// all objects in the same row should have the same height
	// all objects in the same column should have the same width

	getObject := func(rowIndex, columnIndex int) *d2graph.Object {
		var index int
		if gd.rowDirected {
			index = rowIndex*gd.columns + columnIndex
		} else {
			index = columnIndex*gd.rows + rowIndex
		}
		if index < len(gd.objects) {
			return gd.objects[index]
		}
		return nil
	}

	occupiedRows, occupiedColumns := gd.occupiedDimensions()
	rowHeights := make([]float64, 0, occupiedRows)
	colWidths := make([]float64, 0, occupiedColumns)
	for i := 0; i < occupiedRows; i++ {
		rowHeight := 0.
		for j := 0; j < occupiedColumns; j++ {
			o := getObject(i, j)
			if o == nil {
				break
			}
			rowHeight = math.Max(rowHeight, o.Height)
		}
		rowHeights = append(rowHeights, rowHeight)
	}
	for j := 0; j < occupiedColumns; j++ {
		columnWidth := 0.
		for i := 0; i < occupiedRows; i++ {
			o := getObject(i, j)
			if o == nil {
				break
			}
			columnWidth = math.Max(columnWidth, o.Width)
		}
		colWidths = append(colWidths, columnWidth)
	}

	horizontalGap := float64(gd.horizontalGap)
	verticalGap := float64(gd.verticalGap)

	cursor := geo.NewPoint(0, 0)
	if gd.rowDirected {
		for i := 0; i < occupiedRows; i++ {
			for j := 0; j < occupiedColumns; j++ {
				o := getObject(i, j)
				if o == nil {
					break
				}
				o.Width = colWidths[j]
				o.Height = rowHeights[i]
				o.MoveWithDescendantsTo(cursor.X, cursor.Y)
				cursor.X += o.Width + horizontalGap
			}
			cursor.X = 0
			cursor.Y += rowHeights[i] + verticalGap
		}
	} else {
		for j := 0; j < occupiedColumns; j++ {
			for i := 0; i < occupiedRows; i++ {
				o := getObject(i, j)
				if o == nil {
					break
				}
				o.Width = colWidths[j]
				o.Height = rowHeights[i]
				o.MoveWithDescendantsTo(cursor.X, cursor.Y)
				cursor.Y += o.Height + verticalGap
			}
			cursor.X += colWidths[j] + horizontalGap
			cursor.Y = 0
		}
	}

	totalWidth := float64(gd.columns-1) * horizontalGap
	totalHeight := float64(gd.rows-1) * verticalGap
	for _, w := range colWidths {
		totalWidth += w
	}
	for _, h := range rowHeights {
		totalHeight += h
	}
	gd.width = totalWidth
	gd.height = totalHeight
}

// occupiedDimensions bounds temporary layout storage by the number of objects,
// while the final dimensions still account for explicitly retained empty cells.
func (gd *gridDiagram) occupiedDimensions() (rows, columns int) {
	if len(gd.objects) == 0 {
		return 0, 0
	}
	if gd.rowDirected {
		return divideRoundUp(len(gd.objects), gd.columns), min(len(gd.objects), gd.columns)
	}
	return min(len(gd.objects), gd.rows), divideRoundUp(len(gd.objects), gd.rows)
}

func (gd *gridDiagram) layoutDynamic(g *d2graph.Graph, obj *d2graph.Object) {
	// assume we have the following objects to layout:
	// . ┌A──────────────┐  ┌B──┐  ┌C─────────┐  ┌D────────┐  ┌E────────────────┐
	// . └───────────────┘  │   │  │          │  │         │  │                 │
	// .                    │   │  └──────────┘  │         │  │                 │
	// .                    │   │                │         │  └─────────────────┘
	// .                    └───┘                │         │
	// .                                         └─────────┘
	// Note: if the grid is row dominant, all objects should be the same height (same width if column dominant)
	// . ┌A─────────────┐  ┌B──┐  ┌C─────────┐  ┌D────────┐  ┌E────────────────┐
	// . ├ ─ ─ ─ ─ ─ ─ ─┤  │   │  │          │  │         │  │                 │
	// . │              │  │   │  ├ ─ ─ ─ ─ ─┤  │         │  │                 │
	// . │              │  │   │  │          │  │         │  ├ ─ ─ ─ ─ ─ ─ ─ ─ ┤
	// . │              │  ├ ─ ┤  │          │  │         │  │                 │
	// . └──────────────┘  └───┘  └──────────┘  └─────────┘  └─────────────────┘

	horizontalGap := float64(gd.horizontalGap)
	verticalGap := float64(gd.verticalGap)

	// we want to split up the total width across the N rows or columns as evenly as possible
	var totalWidth, totalHeight float64
	for _, o := range gd.objects {
		totalWidth += o.Width
		totalHeight += o.Height
	}
	totalWidth += horizontalGap * float64(len(gd.objects)-gd.rows)
	totalHeight += verticalGap * float64(len(gd.objects)-gd.columns)

	var layout [][]*d2graph.Object
	if gd.rowDirected {
		targetWidth := totalWidth / float64(gd.rows)
		layout = gd.getBestLayout(targetWidth, false)
	} else {
		targetHeight := totalHeight / float64(gd.columns)
		layout = gd.getBestLayout(targetHeight, true)
	}

	cursor := geo.NewPoint(0, 0)
	gd.tracks = layout
	var maxY, maxX float64
	if gd.rowDirected {
		// measure row widths
		rowWidths := []float64{}
		for _, row := range layout {
			x := 0.
			for _, o := range row {
				x += o.Width + horizontalGap
			}
			rowWidth := x - horizontalGap
			rowWidths = append(rowWidths, rowWidth)
			maxX = math.Max(maxX, rowWidth)
		}

		// TODO if object is a nested grid, consider growing descendants according to the inner grid layout

		// then expand thinnest objects to make each row the same width
		// . ┌A─────────────┐  ┌B──┐  ┌C─────────┐ ┬ maxHeight(A,B,C)
		// . │              │  │   │  │          │ │
		// . │              │  │   │  │          │ │
		// . │              │  │   │  │          │ │
		// . └──────────────┘  └───┘  └──────────┘ ┴
		// . ┌D────────┬────┐  ┌E────────────────┐ ┬ maxHeight(D,E)
		// . │              │  │                 │ │
		// . │         │    │  │                 │ │
		// . │              │  │                 │ │
		// . │         │    │  │                 │ │
		// . └─────────┴────┘  └─────────────────┘ ┴
		for i, row := range layout {
			rowWidth := rowWidths[i]
			if rowWidth == maxX {
				continue
			}
			delta := maxX - rowWidth
			var widest float64
			for _, o := range row {
				widest = math.Max(widest, o.Width)
			}
			diffs := make([]float64, len(row))
			totalDiff := 0.
			for i, o := range row {
				diffs[i] = widest - o.Width
				totalDiff += diffs[i]
			}
			if totalDiff > 0 {
				// expand smaller nodes up to the size of the larger ones with delta
				// percentage diff
				for i := range diffs {
					diffs[i] /= totalDiff
				}
				growth := math.Min(delta, totalDiff)
				// expand smaller objects to fill remaining space
				for i, o := range row {
					o.Width += diffs[i] * growth
				}
			}
			if delta > totalDiff {
				growth := (delta - totalDiff) / float64(len(row))
				for _, o := range row {
					o.Width += growth
				}
			}
		}

		// if we have 2 rows, then each row's objects should have the same height
		// . ┌A─────────────┐  ┌B──┐  ┌C─────────┐ ┬ maxHeight(A,B,C)
		// . ├ ─ ─ ─ ─ ─ ─ ─┤  │   │  │          │ │
		// . │              │  │   │  ├ ─ ─ ─ ─ ─┤ │
		// . │              │  │   │  │          │ │
		// . └──────────────┘  └───┘  └──────────┘ ┴
		// . ┌D────────┐  ┌E────────────────┐ ┬ maxHeight(D,E)
		// . │         │  │                 │ │
		// . │         │  │                 │ │
		// . │         │  ├ ─ ─ ─ ─ ─ ─ ─ ─ ┤ │
		// . │         │  │                 │ │
		// . └─────────┘  └─────────────────┘ ┴
		for _, row := range layout {
			rowHeight := 0.
			for _, o := range row {
				o.MoveWithDescendantsTo(cursor.X, cursor.Y)
				cursor.X += o.Width + horizontalGap
				rowHeight = math.Max(rowHeight, o.Height)
			}

			// set all objects in row to the same height
			for _, o := range row {
				o.Height = rowHeight
			}

			// new row
			cursor.X = 0
			cursor.Y += rowHeight + verticalGap
		}
		maxY = cursor.Y - verticalGap
	} else {
		// measure column heights
		colHeights := []float64{}
		for _, column := range layout {
			y := 0.
			for _, o := range column {
				y += o.Height + verticalGap
			}
			colHeight := y - verticalGap
			colHeights = append(colHeights, colHeight)
			maxY = math.Max(maxY, colHeight)
		}

		// then expand shortest objects to make each column the same height
		// . ├maxWidth(A,B)─┤  ├maxW(C,D)─┤  ├maxWidth(E)──────┤
		// . ┌A─────────────┐  ┌C─────────┐  ┌E────────────────┐
		// . ├ ─ ─ ─ ─ ─ ─  ┤  │          │  │                 │
		// . │              │  └──────────┘  │                 │
		// . └──────────────┘  ┌D─────────┐  ├ ─ ─ ─ ─ ─ ─ ─ ─ ┤
		// . ┌B─────────────┐  │          │  │                 │
		// . │              │  │          │  │                 │
		// . │              │  │          │  │                 │
		// . │              │  │          │  │                 │
		// . └──────────────┘  └──────────┘  └─────────────────┘
		for i, column := range layout {
			colHeight := colHeights[i]
			if colHeight == maxY {
				continue
			}
			delta := maxY - colHeight
			var tallest float64
			for _, o := range column {
				tallest = math.Max(tallest, o.Height)
			}
			diffs := make([]float64, len(column))
			totalDiff := 0.
			for i, o := range column {
				diffs[i] = tallest - o.Height
				totalDiff += diffs[i]
			}
			if totalDiff > 0 {
				// expand smaller nodes up to the size of the larger ones with delta
				// percentage diff
				for i := range diffs {
					diffs[i] /= totalDiff
				}
				growth := math.Min(delta, totalDiff)
				// expand smaller objects to fill remaining space
				for i, o := range column {
					o.Height += diffs[i] * growth
				}
			}
			if delta > totalDiff {
				growth := (delta - totalDiff) / float64(len(column))
				for _, o := range column {
					o.Height += growth
				}
			}
		}
		// if we have 3 columns, then each column's objects should have the same width
		// . ├maxWidth(A,B)─┤  ├maxW(C,D)─┤  ├maxWidth(E)──────┤
		// . ┌A─────────────┐  ┌C─────────┐  ┌E────────────────┐
		// . └──────────────┘  │          │  │                 │
		// . ┌B──┬──────────┐  └──────────┘  │                 │
		// . │              │  ┌D────────┬┐  └─────────────────┘
		// . │   │          │  │          │
		// . │              │  │         ││
		// . └───┴──────────┘  │          │
		// .                   │         ││
		// .                   └─────────┴┘
		for _, column := range layout {
			colWidth := 0.
			for _, o := range column {
				o.MoveWithDescendantsTo(cursor.X, cursor.Y)
				cursor.Y += o.Height + verticalGap
				colWidth = math.Max(colWidth, o.Width)
			}
			// set all objects in column to the same width
			for _, o := range column {
				o.Width = colWidth
			}

			// new column
			cursor.Y = 0
			cursor.X += colWidth + horizontalGap
		}
		maxX = cursor.X - horizontalGap
	}
	gd.width = maxX
	gd.height = maxY
}

// generate the best layout of objects aiming for each row to be the targetSize width
// if columns is true, each column aims to have the targetSize height
func (gd *gridDiagram) getBestLayout(targetSize float64, columns bool) [][]*d2graph.Object {
	var nCuts int
	if columns {
		nCuts = gd.columns - 1
	} else {
		nCuts = gd.rows - 1
	}
	if nCuts <= 0 {
		return GenLayout(gd.objects, nil)
	}
	if nCuts == len(gd.objects)-1 {
		cuts := make([]int, nCuts)
		for i := range cuts {
			cuts[i] = i
		}
		return GenLayout(gd.objects, cuts)
	}

	var bestLayout [][]*d2graph.Object
	bestDist := math.MaxFloat64
	fastIsBest := false
	// try fast layout algorithm as a baseline
	if fastLayout := gd.fastLayout(targetSize, nCuts, columns); fastLayout != nil {
		dist := getDistToTarget(fastLayout, targetSize, float64(gd.horizontalGap), float64(gd.verticalGap), columns)
		if dist == 0 {
			return fastLayout
		}
		bestDist = dist
		bestLayout = fastLayout
		fastIsBest = true
	}

	var gap float64
	if columns {
		gap = float64(gd.verticalGap)
	} else {
		gap = float64(gd.horizontalGap)
	}
	getSize := func(o *d2graph.Object) float64 {
		if columns {
			return o.Height
		} else {
			return o.Width
		}
	}

	sizes := []float64{}
	for _, obj := range gd.objects {
		size := getSize(obj)
		sizes = append(sizes, size)
	}
	measurements := newGridRowMeasurements(sizes, gap)
	if cuts := measurements.integralCuts(nCuts + 1); cuts != nil {
		return GenLayout(gd.objects, cuts)
	}
	sd := stddev(sizes)

	skipCount := 0
	count := 0
	// quickly eliminate bad row groupings
	startingCache := make(map[int]bool)
	// Note: we want a low threshold to explore good options within attemptLimit,
	// but the best option may require a few rows that are far from the target size.
	okThreshold := STARTING_THRESHOLD
	// The smallest row this pass rejected as too large and the largest it
	// rejected as too small: the first rows a higher okThreshold accepts.
	minTooLarge, maxTooSmall := math.Inf(1), math.Inf(-1)
	rowOk := func(start, end int, starting bool) (ok bool) {
		n := end - start
		if starting {
			// we can cache results from starting positions since they repeat and don't change
			// with starting=true it will always be the first n objects
			if ok, has := startingCache[n]; has {
				return ok
			}
			defer func() {
				// cache result before returning
				startingCache[n] = ok
			}()
		}

		rowSize := measurements.get(start, end).size
		if n > 1 {
			rowSize += gap * float64(n-1)
			// if multiple nodes are too big, it isn't ok. but a single node can't shrink so only check here
			if rowSize > okThreshold*targetSize {
				skipCount++
				minTooLarge = math.Min(minTooLarge, rowSize)
				// there may even be too many to skip
				return skipCount >= SKIP_LIMIT
			}
		}
		// row is too small to be good overall
		if rowSize < targetSize/okThreshold {
			skipCount++
			maxTooSmall = math.Max(maxTooSmall, rowSize)
			return skipCount >= SKIP_LIMIT
		}
		return true
	}

	// get all options for where to place these cuts, preferring later cuts over earlier cuts
	// with 5 objects and 2 cuts we have these options:
	// .       A   B   C │ D │ E     <- these cuts would produce: ┌A─┐ ┌B─┐ ┌C─┐
	// .       A   B │ C   D │ E                                  └──┘ └──┘ └──┘
	// .       A │ B   C   D │ E                                  ┌D───────────┐
	// .       A   B │ C │ D   E                                  └────────────┘
	// .       A │ B   C │ D   E                                  ┌E───────────┐
	// .       A │ B │ C   D   E                                  └────────────┘
	// of these divisions, find the layout with rows closest to the targetSize
	tryDivision := func(division []int) bool {
		dist := 0.
		start := 0
		for i := 0; i <= len(division); i++ {
			end := len(gd.objects)
			if i < len(division) {
				end = division[i] + 1
			}
			dist += math.Abs(measurements.get(start, end).withGap - targetSize)
			start = end
		}
		if dist < bestDist {
			bestLayout = GenLayout(gd.objects, division)
			bestDist = dist
			fastIsBest = false
		} else if fastIsBest && dist == bestDist {
			// prefer ordered search solution to fast layout solution
			bestLayout = GenLayout(gd.objects, division)
			fastIsBest = false
		}
		count++
		// with few objects we can try all options to get best result but this won't scale, so only try up to 100k options
		return count >= ATTEMPT_LIMIT || skipCount >= SKIP_LIMIT
	}

	// When every cut is cheap to check, score fractional rows in their
	// original floating-point addition order without heuristic pruning.
	if affordablePartitions(len(sizes), nCuts) {
		iterDivisions(len(sizes), nCuts, tryDivision, func(_, _ int, _ bool) bool { return true })
		return bestLayout
	}

	// try number of different okThresholds depending on std deviation of sizes
	thresholdAttempts := int(math.Ceil(sd))
	if thresholdAttempts < MIN_THRESHOLD_ATTEMPTS {
		thresholdAttempts = MIN_THRESHOLD_ATTEMPTS
	} else if thresholdAttempts > MAX_THRESHOLD_ATTEMPTS {
		thresholdAttempts = MAX_THRESHOLD_ATTEMPTS
	}
	for i := 0; i < thresholdAttempts || bestLayout == nil; i++ {
		count = 0.
		skipCount = 0.
		minTooLarge, maxTooSmall = math.Inf(1), math.Inf(-1)
		iterDivisions(len(gd.objects), nCuts, tryDivision, rowOk)
		okThreshold += THRESHOLD_STEP_SIZE
		startingCache = make(map[int]bool)
		if skipCount == 0 {
			// threshold isn't skipping anything so increasing it won't help
			break
		}
		// okThreshold isn't high enough yet, we skipped every option so don't count it
		if count == 0 && thresholdAttempts < MAX_THRESHOLD_ATTEMPTS {
			thresholdAttempts++
		}
		if count == 0 && bestLayout == nil {
			// No division passed, and the passes that follow repeat this one
			// until okThreshold accepts a row it rejected. Skip those passes.
			// With one object far larger than the rest, that can take more
			// passes than finish in any reasonable time, or never happen.
			passes, ok := passesBeforeAccepting(okThreshold, targetSize, minTooLarge, maxTooSmall)
			if !ok {
				// Score divisions without the size checks instead.
				count, skipCount = 0, 0
				iterDivisions(len(gd.objects), nCuts, func(division []int) bool {
					if bestLayout == nil {
						bestLayout = GenLayout(gd.objects, division)
					}
					return tryDivision(division)
				}, func(int, int, bool) bool { return true })
				break
			}
			for ; passes > 0; passes-- {
				okThreshold += THRESHOLD_STEP_SIZE
				i++
				if thresholdAttempts < MAX_THRESHOLD_ATTEMPTS {
					thresholdAttempts++
				}
			}
		}
	}

	return bestLayout
}

// maxRepeatedPasses bounds the passes of getBestLayout that
// passesBeforeAccepting skips. Each costs at least a search over the cuts, so
// more of them would take minutes or longer.
const maxRepeatedPasses = 1 << 24

// passesBeforeAccepting returns how many passes, each adding
// THRESHOLD_STEP_SIZE to okThreshold, come before the first okThreshold that
// accepts a row too large (minTooLarge) or too small (maxTooSmall) for the
// last pass. It reports false if none comes within maxRepeatedPasses.
func passesBeforeAccepting(okThreshold, targetSize, minTooLarge, maxTooSmall float64) (int, bool) {
	accepts := func(okThreshold float64) bool {
		return !(minTooLarge > okThreshold*targetSize) || !(maxTooSmall < targetSize/okThreshold)
	}
	// A rejected row is accepted once okThreshold reaches its ratio to the
	// target size (for a positive target size). Give up early when that is
	// out of reach.
	need := math.Inf(1)
	if targetSize > 0 {
		need = math.Min(minTooLarge/targetSize, need)
		if maxTooSmall > 0 {
			need = math.Min(targetSize/maxTooSmall, need)
		}
	}
	if !((need-okThreshold)/THRESHOLD_STEP_SIZE < maxRepeatedPasses) {
		return 0, false
	}
	// Step as the loop does, so okThreshold rounds the same way.
	for passes := 0; passes <= maxRepeatedPasses; passes++ {
		if accepts(okThreshold) {
			return passes, true
		}
		okThreshold += THRESHOLD_STEP_SIZE
	}
	return 0, false
}

func sum(values []float64) float64 {
	s := 0.
	for _, v := range values {
		s += v
	}
	return s
}

func avg(values []float64) float64 {
	return sum(values) / float64(len(values))
}

func variance(values []float64) float64 {
	mean := avg(values)
	total := 0.
	for _, value := range values {
		dev := mean - value
		total += dev * dev
	}
	return total / float64(len(values))
}

func stddev(values []float64) float64 {
	return math.Sqrt(variance(values))
}

func (gd *gridDiagram) fastLayout(targetSize float64, nCuts int, columns bool) (layout [][]*d2graph.Object) {
	var gap float64
	if columns {
		gap = float64(gd.verticalGap)
	} else {
		gap = float64(gd.horizontalGap)
	}

	debt := 0.
	fastDivision := make([]int, 0, nCuts)
	rowSize := 0.
	for i := 0; i < len(gd.objects); i++ {
		o := gd.objects[i]
		var size float64
		if columns {
			size = o.Height
		} else {
			size = o.Width
		}
		if rowSize == 0 {
			// if a single object meets the target size, end the row here
			if size > targetSize-debt {
				// cut row with just this object
				fastDivision = append(fastDivision, i)
				// we build up a debt of distance past the target size across rows
				newDebt := size - targetSize
				debt += newDebt
			} else {
				rowSize += size
			}
			continue
		}
		// debt is paid by decreasing threshold to start new row and ending below targetSize
		if rowSize+gap+(size)/2. > targetSize-debt {
			// start a new row before this object since it is mostly past the target size
			// .              size
			// ├...row─┼gap┼───┼───┤
			// ├──targetSize──┤ (debt=0)
			fastDivision = append(fastDivision, i-1)
			newDebt := rowSize - targetSize
			debt += newDebt
			rowSize = size
		} else {
			rowSize += gap + size
		}
	}
	if len(fastDivision) == nCuts {
		layout = GenLayout(gd.objects, fastDivision)
	}

	return layout
}

// generate a grid of objects from the given cut indices
// each cut index applies after the object at that index
// e.g. [0 1 2 3 4 5 6 7] with cutIndices [0, 2, 6] => [[0], [1, 2], [3,4,5,6], [7]]
func GenLayout(objects []*d2graph.Object, cutIndices []int) [][]*d2graph.Object {
	layout := make([][]*d2graph.Object, len(cutIndices)+1)
	objIndex := 0
	for i := 0; i <= len(cutIndices); i++ {
		var stop int
		if i < len(cutIndices) {
			stop = cutIndices[i]
		} else {
			stop = len(objects) - 1
		}
		if stop >= objIndex {
			layout[i] = make([]*d2graph.Object, 0, stop-objIndex+1)
		}
		for ; objIndex <= stop; objIndex++ {
			layout[i] = append(layout[i], objects[objIndex])
		}
	}
	return layout
}

func getDistToTarget(layout [][]*d2graph.Object, targetSize float64, horizontalGap, verticalGap float64, columns bool) float64 {
	totalDelta := 0.
	for _, row := range layout {
		rowSize := 0.
		for _, o := range row {
			if columns {
				rowSize += o.Height + verticalGap
			} else {
				rowSize += o.Width + horizontalGap
			}
		}
		if len(row) > 0 {
			if columns {
				rowSize -= verticalGap
			} else {
				rowSize -= horizontalGap
			}
		}
		totalDelta += math.Abs(rowSize - targetSize)
	}
	return totalDelta
}

func (gd *gridDiagram) reserveCellMargins() (placeWithinCells func()) {
	type measuredCell struct {
		margin        geo.Spacing
		width, height float64
	}
	cells := make([]measuredCell, len(gd.objects))
	for i, o := range gd.objects {
		margin := o.GetMargin()
		cells[i] = measuredCell{margin, o.Width, o.Height}

		o.Height += margin.Top + margin.Bottom
		o.Width += margin.Left + margin.Right
	}

	// Example: a single column with 3 shapes and
	// `x.label: long label {near: outside-bottom-left}`
	// `y.label: outsider {near: outside-right-center}`
	// . ┌───────────────────┐
	// . │ widest shape here │
	// . └───────────────────┘
	// . ┌───┐
	// . │ x │
	// . └───┘
	// . long label
	// . ├─────────┤ x's new width
	// .     ├─mr──┤ margin.right added to width during layout
	// . ┌───┐
	// . │ y │ outsider
	// . └───┘
	// . ├─────────────┤ y's new width
	// .     ├───mr────┤ margin.right added to width during layout

	// BEFORE LAYOUT
	// . ┌───────────────────┐
	// . │ widest shape here │
	// . └───────────────────┘
	// . ┌─────────┐
	// . │ x       │
	// . └─────────┘
	// . ┌─────────────┐
	// . │ y           │
	// . └─────────────┘

	// AFTER LAYOUT
	// . ┌───────────────────┐
	// . │ widest shape here │
	// . └───────────────────┘
	// . ┌───────────────────┐
	// . │ x                 │
	// . └───────────────────┘
	// . ┌───────────────────┐
	// . │ y                 │
	// . └───────────────────┘

	// CLEANUP 1/2
	// . ┌───────────────────┐
	// . │ widest shape here │
	// . └───────────────────┘
	// . ┌─────────────┐
	// . │ x           │
	// . └─────────────┘
	// . long label    ├─mr──┤ remove margin we added
	// . ┌─────────┐
	// . │ y       │ outsider
	// . └─────────┘
	// .           ├───mr────┤ remove margin we added
	// CLEANUP 2/2
	// . ┌───────────────────┐
	// . │ widest shape here │
	// . └───────────────────┘
	// . ┌───────────────────┐
	// . │ x                 │
	// . └───────────────────┘
	// . long label    ├─mr──┤ we removed too much so add back margin we subtracted, then subtract new margin
	// . ┌─────────┐
	// . │ y       │ outsider
	// . └─────────┘
	// .           ├───mr────┤ margin.right is still needed

	return func() {
		for i, o := range gd.objects {
			cell := cells[i]
			slotWidth, slotHeight := o.Width, o.Height
			m := cell.margin
			dy := m.Top + m.Bottom
			dx := m.Left + m.Right
			o.Height -= dy
			o.Width -= dx

			// less margin may be needed if layout grew the object
			// compute the new margin after removing the old margin we added
			margin := o.GetMargin()
			marginX := margin.Left + margin.Right
			marginY := margin.Top + margin.Bottom
			if marginX < dx {
				// layout grew width and now we need less of a margin (but we subtracted too much)
				// add back dx and subtract the new amount
				o.Width += dx - marginX
			}
			if marginY < dy {
				o.Height += dy - marginY
			}

			// A track can stretch independently along either axis. Fit constrained
			// shapes inside it, preserving their measured minimum dimensions.
			if fitGridAspectRatio(o, cell.width, cell.height) {
				margin = o.GetMargin()
				// Center the shape and its outside decorations as a unit.
				margin.Left += (slotWidth - o.Width - margin.Left - margin.Right) / 2
				margin.Top += (slotHeight - o.Height - margin.Top - margin.Bottom) / 2
			}
			if margin.Left != 0 || margin.Top != 0 {
				o.MoveWithDescendants(margin.Left, margin.Top)
			}
		}
	}
}

// fitGridAspectRatio leaves the track intact and fits the painted shape inside it.
func fitGridAspectRatio(o *d2graph.Object, measuredWidth, measuredHeight float64) bool {
	var ratio float64
	switch strings.ToLower(o.Shape.Value) {
	case d2target.ShapeCircle, d2target.ShapeSquare:
		ratio = 1
	case d2target.ShapePerson:
		ratio = shape.PERSON_AR_LIMIT
	case d2target.ShapeOval:
		ratio = shape.OVAL_AR_LIMIT
	case d2target.ShapeC4Person:
		// The head radius is proportional to the outer width. Preserve the
		// measured proportions so the body retains room for its contents.
		if measuredWidth <= 0 || measuredHeight <= 0 {
			return false
		}
		scale := min(o.Width/measuredWidth, o.Height/measuredHeight)
		o.Width, o.Height = measuredWidth*scale, measuredHeight*scale
		return true
	default:
		return false
	}
	o.Width = min(o.Width, max(measuredWidth, ratio*o.Height))
	o.Height = min(o.Height, max(measuredHeight, ratio*o.Width))
	return true
}
