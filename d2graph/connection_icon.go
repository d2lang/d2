package d2graph

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/label"
)

var iconRateNumber = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`)

// ParseConnectionIconAnimation interprets the edge icon's animated style.
// Other uses of style.animated retain their existing boolean grammar.
func ParseConnectionIconAnimation(value string) (*d2target.ConnectionIconAnimation, error) {
	if enabled, err := strconv.ParseBool(value); err == nil {
		if !enabled {
			return nil, nil
		}
		return &d2target.ConnectionIconAnimation{}, nil
	}
	if number, ok := strings.CutSuffix(value, "/s"); ok && iconRateNumber.MatchString(number) {
		if rate, err := strconv.ParseFloat(number, 64); err == nil {
			return &d2target.ConnectionIconAnimation{Rate: &rate}, nil
		}
	}
	return nil, errors.New(`connection icon.style.animated must be true, false, or a non-negative decimal rate such as 2/s`)
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
