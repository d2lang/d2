package d2lib

import "math"

// Select one already-clear pose for each sample. Charge squared distance at
// the visibility shared by adjacent poses. A hidden object's change of lanes
// costs nothing. The ordinary pose is always first, wins ties, and receives a
// modest preference so small improvements do not churn native geometry.
// Both held samples have exactly one choice.
func chooseTweenTrack[T any](choices [][]T, distance2 func(T, T) float64, opacity func(int) float64, nativePreference float64) []int {
	parents := make([][]int, len(choices))
	previous := []float64{0}
	for i := 1; i < len(choices); i++ {
		visible := min(opacity(i-1), opacity(i))
		current := make([]float64, len(choices[i]))
		parents[i] = make([]int, len(current))
		for j, c := range choices[i] {
			best := math.Inf(1)
			for k, p := range choices[i-1] {
				if cost := previous[k] + visible*distance2(c, p); cost < best {
					best, parents[i][j] = cost, k
				}
			}
			current[j] = best + nativePreference*opacity(i)*distance2(c, choices[i][0])
		}
		previous = current
	}
	path := make([]int, len(choices))
	for i := len(path) - 1; i > 0; i-- {
		path[i-1] = parents[i][path[i]]
	}
	return path
}
