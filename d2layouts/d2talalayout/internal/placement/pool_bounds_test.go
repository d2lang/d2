package placement

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"
)

// TestPooledEdgeAbductionBoolsAreBounded: TinyGo's sync.Pool never frees, so
// a flag slice grown past typedpool.MaxRetainedEntries is not pooled again.
func TestPooledEdgeAbductionBoolsAreBounded(t *testing.T) {
	scratch := borrowEdgeAbductionBools(typedpool.MaxRetainedEntries + 1)
	returnEdgeAbductionBools(scratch)
	for i := 0; i < 4; i++ {
		if edgeAbductionBoolScratchPool.Get() == scratch {
			t.Fatal("oversized edge abduction flags returned to the pool")
		}
	}
}
