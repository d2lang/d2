package d2graph

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/label"
)

// ParseConnectionIconAnimation interprets the edge icon's animated style.
// Other uses of style.animated retain their existing boolean grammar.
func ParseConnectionIconAnimation(value string) (*d2target.ConnectionIconAnimation, error) {
	if enabled, err := strconv.ParseBool(value); err == nil {
		if !enabled {
			return nil, nil
		}
		return &d2target.ConnectionIconAnimation{}, nil
	}
	value = strings.TrimSpace(value)
	if number, ok := strings.CutSuffix(value, "/s"); ok && strings.TrimLeft(number, "0123456789.") == "" {
		rate, err := strconv.ParseFloat(number, 64)
		// ParseFloat rounds sufficiently small positive numbers to zero without
		// an error. Only an authored zero should stop an existing stream.
		if err == nil && (rate != 0 || strings.TrimLeft(number, "0.") == "") {
			return &d2target.ConnectionIconAnimation{Rate: &rate}, nil
		}
	}
	if number, ok := strings.CutSuffix(value, "ms"); ok {
		const maxMS = math.MaxInt64 / int64(time.Millisecond)
		ms, err := strconv.ParseUint(number, 10, 64)
		if errors.Is(err, strconv.ErrRange) || err == nil && (ms == 0 || ms > uint64(maxMS)) {
			return nil, fmt.Errorf("connection icon.style.animated trip duration must be between 1ms and %dms", maxMS)
		}
		if err == nil {
			return &d2target.ConnectionIconAnimation{DurationMS: int64(ms)}, nil
		}
	}
	return nil, errors.New("connection icon.style.animated expects true, false, a non-negative rate such as 2/s, or a positive whole-millisecond duration such as 200ms")
}

// HasIcon includes native artwork and the default dot of an animated stream.
func (e *Edge) HasIcon() bool {
	return e.Icon != nil || e.IconShape != nil || e.iconAnimation() != nil
}

func (e *Edge) iconAnimation() *d2target.ConnectionIconAnimation {
	if e.IconStyle.Animated == nil {
		return nil
	}
	animation, _ := ParseConnectionIconAnimation(e.IconStyle.Animated.Value)
	return animation
}

// ExportIcon supplies the same artwork dimensions and positioning to label placement and rendering.
func (e *Edge) ExportIcon(connection *d2target.Connection) {
	connection.Icon = e.Icon
	if e.IconShape != nil {
		connection.IconShape = e.IconShape.Value
	}
	connection.IconAnimation = e.iconAnimation()
	if connection.IconAnimation != nil && connection.Icon == nil {
		connection.IconShape = d2target.ShapeCircle
	}
	if connection.Icon != nil || connection.IconShape != "" {
		if e.IconPosition != nil {
			connection.IconPosition = (d2ast.LabelPositionsMapping[e.IconPosition.Value]).String()
		} else {
			connection.IconPosition = label.InsideMiddleCenter.String()
		}
	}

	if e.IconStyle.BorderRadius != nil {
		connection.IconBorderRadius, _ = strconv.ParseFloat(e.IconStyle.BorderRadius.Value, 64)
	}
}
