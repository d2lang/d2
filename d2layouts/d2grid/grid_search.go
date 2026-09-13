package d2grid

import "math"

// Row measurements preserve the original left-to-right floating-point results.
// Prefix sums are used only when every intermediate value is an exact integer.
type gridRowMeasurement struct {
	start, end    int
	size, withGap float64
}

type gridRowMeasurements struct {
	sizes          []float64
	gap            float64
	cache          []gridRowMeasurement
	prefixChecked  bool
	integralPrefix []float64
}

func newGridRowMeasurements(sizes []float64, gap float64) *gridRowMeasurements {
	return &gridRowMeasurements{sizes: sizes, gap: gap}
}

func (m *gridRowMeasurements) get(start, end int) gridRowMeasurement {
	// Empty and singleton rows take constant time to measure. In particular,
	// a grid with one row per object should not allocate a large row cache.
	if end-start <= 1 {
		return m.measure(start, end)
	}
	if !m.prefixChecked {
		m.initIntegralPrefix()
	}
	if m.integralPrefix != nil {
		size := m.integralPrefix[end] - m.integralPrefix[start]
		return gridRowMeasurement{start: start, end: end, size: size, withGap: size + m.gap*float64(end-start-1)}
	}
	if m.cache == nil {
		// Bound the cache independently of diagram size; collisions only cause
		// recomputation. Small diagrams fit without collisions.
		const maxEntries = 32 * 1024
		entries := 1
		for entries < maxEntries && entries/len(m.sizes) < len(m.sizes) {
			entries *= 2
		}
		m.cache = make([]gridRowMeasurement, entries)
	}
	slot := (uint(start)*uint(len(m.sizes)) + uint(end)) & uint(len(m.cache)-1)
	cached := &m.cache[slot]
	if cached.start == start && cached.end == end {
		return *cached
	}
	*cached = m.measure(start, end)
	return *cached
}

func (m *gridRowMeasurements) initIntegralPrefix() {
	m.prefixChecked = true
	// All integers through 2^53 are represented exactly by float64. Requiring
	// nonnegative integral sizes and gaps, and bounding their complete sum,
	// also bounds every intermediate in the original row sums (including the
	// trailing gap before it is subtracted). Prefix subtraction and adding the
	// internal gaps therefore reproduce the original results exactly.
	const maxExactInteger = uint64(1 << 53)
	exact := func(value float64) bool {
		return value >= 0 && value <= float64(maxExactInteger) && math.Trunc(value) == value
	}
	if !exact(m.gap) {
		return
	}
	gap := uint64(m.gap)
	var total uint64
	for _, value := range m.sizes {
		if !exact(value) {
			return
		}
		size := uint64(value)
		if size > maxExactInteger-total || gap > maxExactInteger-total-size {
			return
		}
		total += size + gap
	}
	m.integralPrefix = make([]float64, len(m.sizes)+1)
	for i, value := range m.sizes {
		m.integralPrefix[i+1] = m.integralPrefix[i] + value
	}
}

func (m *gridRowMeasurements) measure(start, end int) gridRowMeasurement {
	size, withGap := 0., 0.
	for _, v := range m.sizes[start:end] {
		size += v
		withGap += v + m.gap
	}
	if start < end {
		withGap -= m.gap
	}
	return gridRowMeasurement{start: start, end: end, size: size, withGap: withGap}
}

// The division and row checks run in their original order, including repeated
// checks: their attempt/skip counts affect when the bounded search terminates.
// The callback borrows cuts only until it returns.
type iterDivision func(division []int) (done bool)
type checkCut func(start, end int, starting bool) (ok bool)

func iterDivisions(nObjects, nCuts int, f iterDivision, check checkCut) {
	if nObjects < 2 || nCuts == 0 {
		return
	}
	cuts := make([]int, nCuts)
	var visit func(end, remaining int) bool
	visit = func(end, remaining int) bool {
		for index := end - 1; index >= remaining; index-- {
			if !check(index, end, false) {
				continue
			}
			cuts[remaining-1] = index - 1
			if remaining > 1 {
				if visit(index, remaining-1) {
					return true
				}
			} else if check(0, index, true) && f(cuts) {
				return true
			}
		}
		return false
	}
	visit(nObjects, nCuts)
}
