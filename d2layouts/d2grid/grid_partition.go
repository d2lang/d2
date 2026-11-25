package d2grid

import "math"

// integralCuts minimizes the total absolute deviation from the mean track size.
// It returns nil when the bounded search in getBestLayout should handle the grid.
// Rows and columns use the same one-dimensional partition.
func (m *gridRowMeasurements) integralCuts(tracks int) []int {
	n := len(m.sizes)
	// Each track must contain at least one item, leaving this many possible
	// ends per track. Bound both the search and the space used to recover cuts.
	span := n - tracks + 1
	const maxStates = 1_000_000
	if tracks > maxStates/span {
		return nil
	}
	m.initIntegralPrefix()
	if m.integralPrefix == nil {
		return nil
	}
	gap := int64(m.gap)
	total := int64(m.integralPrefix[n]) + int64(n)*gap
	// Scale by the track count to score against the exact (possibly fractional)
	// mean. Any partial score is at most twice the final scaled prefix, since
	// the measured lengths sum to at most that prefix and so do their targets.
	if total > math.MaxInt64/4/int64(tracks) {
		return nil
	}
	prefix := make([]int64, n+1)
	for i, size := range m.integralPrefix {
		prefix[i] = (int64(size) + int64(i)*gap) * int64(tracks)
	}

	prev, next := make([]int64, n+1), make([]int64, n+1)
	for end := 1; end <= span; end++ {
		prev[end] = prefix[end] - total
		if prev[end] < 0 {
			prev[end] = -prev[end]
		}
	}
	parent := make([]int32, (tracks-1)*span)
	queue := make([]int, span)
	for track := 2; track <= tracks; track++ {
		// For a last cut j, cost = prev[j] + |x - prefix[j]|, where
		// x = prefix[end] - total. On the left of x minimize prev[j]-prefix[j].
		// On its right minimize prev[j]+prefix[j] in a sliding window. The
		// prefix and x increase monotonically, so each cut is visited O(1) times.
		pending, leftBest := track-1, -1
		head, tail := 0, 0
		for end := track; end < track+span; end++ {
			j := end - 1
			rightCost := prev[j] + prefix[j]
			for tail > head {
				last := queue[tail-1]
				if prev[last]+prefix[last] < rightCost {
					break
				}
				tail--
			}
			queue[tail] = j
			tail++

			x := prefix[end] - total
			for pending < end && prefix[pending] <= x {
				if leftBest < 0 || prev[pending]-prefix[pending] <= prev[leftBest]-prefix[leftBest] {
					leftBest = pending
				}
				pending++
			}
			for head < tail && queue[head] < pending {
				head++
			}

			best, cost := leftBest, int64(math.MaxInt64)
			if best >= 0 {
				cost = prev[best] + (x - prefix[best])
			}
			if head < tail {
				right := queue[head]
				candidate := prev[right] + (prefix[right] - x)
				// Prefer the later final cut on ties, as in iterDivisions.
				if candidate <= cost {
					best, cost = right, candidate
				}
			}
			next[end] = cost
			parent[(track-2)*span+end-track] = int32(best)
		}
		prev, next = next, prev
	}

	cuts := make([]int, tracks-1)
	end := n
	for track := tracks; track >= 2; track-- {
		end = int(parent[(track-2)*span+end-track])
		cuts[track-2] = end - 1 // GenLayout takes the last item of each track.
	}
	return cuts
}
