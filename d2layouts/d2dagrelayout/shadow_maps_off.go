//go:build !tinygo && !dagre_shadowmaps

package d2dagrelayout

// shadowMaps is false under gc, where repeating the original's other maps of
// a cross-rank shift changes no distribution of results (see map_order.go and
// shadow_maps_on.go), so the code that makes them is not compiled in.
const shadowMaps = false
