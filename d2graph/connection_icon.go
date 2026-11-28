package d2graph

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	fields := strings.Fields(value)
	var rateValue, tripValue string
	switch {
	case len(fields) == 1:
		if strings.HasSuffix(fields[0], "/s") {
			rateValue = fields[0]
		} else {
			tripValue = fields[0]
		}
	case len(fields) == 3 && fields[1] == "in":
		rateValue, tripValue = fields[0], fields[2]
	default:
		return nil, errors.New("connection icon.style.animated expects true, false, a rate (2/s), a trip duration (200ms), or both (2/s in 200ms)")
	}

	animation := &d2target.ConnectionIconAnimation{}
	if rateValue != "" {
		number, ok := strings.CutSuffix(rateValue, "/s")
		rate, err := strconv.ParseFloat(number, 64)
		if !ok || !iconRateNumber.MatchString(number) || err != nil {
			return nil, errors.New("connection icon.style.animated requires a non-negative decimal departure rate such as 2/s")
		}
		animation.Rate = &rate
	}
	if tripValue != "" {
		duration, err := time.ParseDuration(tripValue)
		if err != nil || duration <= 0 || duration%time.Millisecond != 0 {
			return nil, errors.New("connection icon.style.animated requires a positive trip duration in whole milliseconds, such as 200ms or 1.5s")
		}
		animation.DurationMS = duration.Milliseconds()
	}
	return animation, nil
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
