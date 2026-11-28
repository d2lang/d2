package d2lib

import (
	"context"
	"testing"
	"time"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	d2log "github.com/d2lang/d2/lib/log"
)

func BenchmarkCompilePinnedTween(b *testing.B) {
	const src = `vars: {x:0; d2-config: {animate-interval: 1200ms; animate-duration: 1000ms}}
 rail: "" {left: 0; top: 19; width: 100; height: 2}
 marker: "" {shape: circle; left: ${x}; top: 0; width: 40; height: 40}
 steps: {across: {vars.x: 5}; back: {vars.x: 0}}`
	engine := "tala"
	var layout time.Duration
	opts := &CompileOptions{Ruler: layoutReuseTestRuler, Layout: &engine, LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
		return func(ctx context.Context, g *d2graph.Graph) error {
			start := time.Now()
			err := d2talalayout.DefaultLayout(ctx, g)
			layout += time.Since(start)
			return err
		}, nil
	}}
	ctx := d2log.WithDefault(b.Context())
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := Compile(ctx, src, opts, nil); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(layout.Microseconds())/float64(b.N), "layout-us/op")
}
