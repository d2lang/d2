package placement

const (
	compactionFactor        = 3.0
	nodePlacementIterations = 90
	placementPadding        = 1000.0
	jitterThreshold         = 80.0
	signFlipPadding         = 5.0
	largeGapThreshold       = 0.5
	// maxSizedPhaseWork bounds the edge terms the sized optimizer's
	// annealing scores in one level (passes times twice the level's edges);
	// a level above it runs only the first passes of its sized phase that
	// fit. Of d2's e2e corpus only us_map's 48-node, 108-edge level (311
	// passes, 67,000 terms; now 111 passes) exceeds it; the next largest
	// level has 13,500.
	maxSizedPhaseWork = 48000
	// maxQuietSizedPasses ends the sized phase's annealing early, after that
	// many passes in a row in which no node moved, swapped or was
	// transposed. (Stopping after the first such pass changed ~45 corpus
	// layouts; after 20 the annealing rarely moves again.)
	maxQuietSizedPasses = 20
)
