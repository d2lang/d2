//go:build tinygo || dagre_shadowmaps

package d2dagrelayout

// shadowMaps makes each cross-rank shift whose grow pass may grow a
// container make and fill the original's other maps of the shift as well
// (crossRankShifter.originalMaps). Only TinyGo needs them (see map_order.go);
// under gc the build tag dagre_shadowmaps compiles them in for the tests
// (CI runs this package's tests with it; see TestShadowWritesMatchLegacy).
const shadowMaps = true
