package d2target

import (
	"math"
	"testing"
)

func TestConnectionIconDepartureRateCeiling(t *testing.T) {
	for _, tc := range []struct {
		name       string
		length     float64
		rate       float64
		wantPeriod float64
	}{
		{"below capacity", 180, 63, 1.0 / 63},
		{"exactly at capacity", 180, 64, 1.0 / 64},
		{"just above capacity", 180, 65, 1.0 / 64},
		{"far above capacity", 180, math.MaxFloat64, 1.0 / 64},
		{"short wire permits more than 64 departures per second", 90, 65, 1.0 / 65},
		{"short wire at capacity", 90, 150, 1.0 / 128},
		{"long wire caps below 64 departures per second", 360, 40, 1.0 / 32},
		{"very sparse stream", 180, math.SmallestNonzeroFloat64, math.MaxFloat64},
		{"stopped stream", 180, 0, math.Inf(1)},
		{"empty route", 0, 65, math.Inf(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := IconMotionPath{Distances: []float64{0, tc.length}}
			rate := tc.rate
			if got := path.LaunchPeriod(&ConnectionIconAnimation{Rate: &rate}); got != tc.wantPeriod {
				t.Fatalf("launch period = %g, want %g", got, tc.wantPeriod)
			}
			if rate != tc.rate {
				t.Fatalf("authored rate changed from %g to %g", tc.rate, rate)
			}
		})
	}
}

func TestTimedIconDepartures(t *testing.T) {
	for _, length := range []float64{90, 900} {
		path := IconMotionPath{Distances: []float64{0, length}}
		for _, tc := range []struct {
			animation ConnectionIconAnimation
			period    float64
		}{
			{ConnectionIconAnimation{DurationMS: 200}, .2},
			{ConnectionIconAnimation{Rate: new(2.), DurationMS: 200}, .5},
			{ConnectionIconAnimation{Rate: new(2.), DurationMS: 1500}, .5},
			{ConnectionIconAnimation{Rate: new(1000.), DurationMS: 1500}, 1.5 / MaxMovingIconsPerConnection},
			{ConnectionIconAnimation{Rate: new(0.), DurationMS: 200}, math.Inf(1)},
		} {
			if got := path.LaunchPeriod(&tc.animation); got != tc.period {
				t.Errorf("length %g, %+v: period %g, want %g", length, tc.animation, got, tc.period)
			}
			if got := path.TransitDuration(&tc.animation); got != float64(tc.animation.DurationMS)/1000 {
				t.Errorf("length %g, %+v: transit %gs", length, tc.animation, got)
			}
		}
	}
	var empty IconMotionPath
	if p := empty.LaunchPeriod(&ConnectionIconAnimation{DurationMS: 200}); !math.IsInf(p, 1) {
		t.Fatalf("empty route has finite launch period %g", p)
	}
}
