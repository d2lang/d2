package d2cli

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/xmain"
)

func TestSVGOutputReusesCompilerRuler(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	opts := d2svg.RenderOpts{}
	diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), "a: |md\n  **Native** `SVG`\n|\na -> b\n", &d2lib.CompileOptions{
		Ruler:          ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) { return d2dagrelayout.DefaultLayout, nil },
	}, &opts)
	if err != nil {
		t.Fatal(err)
	}
	want, err := d2svg.Render(diagram, &opts)
	if err != nil {
		t.Fatal(err)
	}
	dot := ruler.Dot
	stdout := &controlledWriteCloser{limit: -1}
	state := &xmain.TestState{
		Run: func(ctx context.Context, ms *xmain.State) error {
			_, _, err := _renderWithPNGEncoder(ctx, ms, opts, "input.d2", "-", false, false, ruler, diagram, SVG, "", false, nil)
			return err
		},
		Args: []string{"d2"}, PWD: t.TempDir(), Stdout: stdout,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state.Start(t, ctx)
	defer state.Cleanup(t)
	if err := state.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), append(want, '\n')) {
		t.Fatal("native CLI SVG differs from standalone rendering")
	}
	if ruler.Dot == dot {
		t.Fatal("native CLI SVG did not use the compiler ruler")
	}
	if opts.Ruler != nil {
		t.Fatal("native CLI mutated caller options")
	}
}
