package d2lib

import (
	"bytes"
	"context"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestApplyConfigsThemeOverridePrecedence(t *testing.T) {
	t.Parallel()

	configLight := "#abcdef"
	configDark := "#fedcba"
	callerLight := "#112233"
	callerDark := "#332211"
	config := &d2target.Config{
		ThemeOverrides:     &d2target.ThemeOverrides{B1: &configLight},
		DarkThemeOverrides: &d2target.ThemeOverrides{B1: &configDark},
	}

	t.Run("caller options win", func(t *testing.T) {
		renderOpts := &d2svg.RenderOpts{
			ThemeOverrides:     &d2target.ThemeOverrides{B1: &callerLight},
			DarkThemeOverrides: &d2target.ThemeOverrides{B1: &callerDark},
		}
		applyConfigs(config, &CompileOptions{}, renderOpts)

		if got := *renderOpts.ThemeOverrides.B1; got != callerLight {
			t.Fatalf("light theme override = %q, want caller value %q", got, callerLight)
		}
		if got := *renderOpts.DarkThemeOverrides.B1; got != callerDark {
			t.Fatalf("dark theme override = %q, want caller value %q", got, callerDark)
		}
	})

	t.Run("config fills absent options", func(t *testing.T) {
		renderOpts := &d2svg.RenderOpts{}
		applyConfigs(config, &CompileOptions{}, renderOpts)

		if got := *renderOpts.ThemeOverrides.B1; got != configLight {
			t.Fatalf("light theme override = %q, want config value %q", got, configLight)
		}
		if got := *renderOpts.DarkThemeOverrides.B1; got != configDark {
			t.Fatalf("dark theme override = %q, want config value %q", got, configDark)
		}
	})
}

func TestIsometricConfigUsesSketchPrecedence(t *testing.T) {
	for _, test := range []struct {
		name           string
		config, option *bool
		want           bool
	}{
		{"default", nil, nil, false},
		{"source enabled", boolPointer(true), nil, true},
		{"caller enabled", boolPointer(false), boolPointer(true), true},
		{"caller disabled", boolPointer(true), boolPointer(false), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			compileOpts := &CompileOptions{}
			renderOpts := &d2svg.RenderOpts{Isometric: test.option}
			applyConfigs(&d2target.Config{Isometric: test.config}, compileOpts, renderOpts)
			applyDefaults(compileOpts, renderOpts)
			if renderOpts.Isometric == nil || *renderOpts.Isometric != test.want {
				t.Fatalf("isometric = %v, want %v", renderOpts.Isometric, test.want)
			}
			if compileOpts.FontFamily != nil {
				t.Fatal("isometric unexpectedly selected the sketch hand-drawn font")
			}
		})
	}
}

func boolPointer(value bool) *bool { return &value }

func TestIsometricCompilePreservesUnspecifiedConfig(t *testing.T) {
	for _, value := range []*bool{nil, boolPointer(false), boolPointer(true)} {
		opts := &d2svg.RenderOpts{Isometric: value}
		diagram, _, err := Compile(context.Background(), "", nil, opts)
		if err != nil {
			t.Fatal(err)
		}
		if value == nil || !*value {
			if diagram.Config.Isometric != nil {
				t.Fatal("disabled isometric changed existing diagram config/hash")
			}
		} else if diagram.Config.Isometric == nil || *diagram.Config.Isometric != *value {
			t.Fatal("explicit mode was not retained")
		}
		if opts.Isometric == nil {
			t.Fatal("caller did not receive resolved default")
		}
	}
}

func TestDisabledIsometricRendersIdenticallyToDefault(t *testing.T) {
	var baseline []byte
	for _, test := range []struct {
		name, source string
		option       *bool
	}{
		{"default", "a -> b", nil},
		{"explicit false", "a -> b", boolPointer(false)},
		{"source false", "vars: {d2-config: {isometric: false}}\na -> b", nil},
		{"disable source true", "vars: {d2-config: {isometric: true}}\na -> b", boolPointer(false)},
	} {
		t.Run(test.name, func(t *testing.T) {
			ruler, err := textmeasure.NewRuler()
			if err != nil {
				t.Fatal(err)
			}
			opts := &d2svg.RenderOpts{Isometric: test.option}
			diagram, _, err := Compile(context.Background(), test.source, &CompileOptions{
				Ruler:          ruler,
				LayoutResolver: func(string) (d2graph.LayoutGraph, error) { return d2dagrelayout.DefaultLayout, nil },
			}, opts)
			if err != nil {
				t.Fatal(err)
			}
			if opts.Isometric == nil || *opts.Isometric {
				t.Fatal("effective disabled mode was not returned")
			}
			if diagram.Config.Isometric != nil {
				t.Fatal("disabled mode leaked into appearance hash")
			}
			out, err := d2svg.Render(diagram, opts)
			if err != nil {
				t.Fatal(err)
			}
			if baseline == nil {
				baseline = out
			} else if !bytes.Equal(baseline, out) {
				t.Fatal("disabled isometric mode changed ordinary SVG bytes")
			}
		})
	}
}

func TestIsometricExplodedConfiguration(t *testing.T) {
	const source = `vars: {d2-config: {isometric-exploded: true}}
layers: {
  upper: {vars: {d2-config: {isometric-layer-gap: 150}}; copy -> proof}
  lower: {vars: {d2-config: {isometric-layer-gap: 0}}; paper -> book}
}`
	for _, tc := range []struct {
		name                string
		opts                d2svg.RenderOpts
		isometric, exploded bool
	}{
		{"source", d2svg.RenderOpts{}, true, true},
		{"disable renderer", d2svg.RenderOpts{Isometric: boolPointer(false)}, false, false},
		{"disable exploded", d2svg.RenderOpts{IsometricExploded: boolPointer(false)}, false, false},
		{"ordinary isometric", d2svg.RenderOpts{Isometric: boolPointer(true), IsometricExploded: boolPointer(false)}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ruler, err := textmeasure.NewRuler()
			if err != nil {
				t.Fatal(err)
			}
			diagram, _, err := Compile(context.Background(), source, &CompileOptions{
				Ruler: ruler, LayoutResolver: func(string) (d2graph.LayoutGraph, error) { return d2dagrelayout.DefaultLayout, nil },
			}, &tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if tc.opts.IsometricEnabled() != tc.isometric || (tc.opts.IsometricExploded != nil && *tc.opts.IsometricExploded) != tc.exploded {
				t.Fatalf("resolved modes = %+v", tc.opts)
			}
			if (diagram.Config.IsometricExploded != nil) != tc.exploded {
				t.Fatalf("unexpected exploded mode in diagram hash config")
			}
			if len(diagram.Layers) != 2 || diagram.Layers[0].IsometricLayerGap == nil || *diagram.Layers[0].IsometricLayerGap != 150 || diagram.Layers[1].IsometricLayerGap == nil || *diagram.Layers[1].IsometricLayerGap != 0 {
				t.Fatal("per-floor gaps did not survive compilation")
			}
			boards, err := d2svg.RenderMultiboard(diagram, &tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if tc.exploded {
				single, err := d2svg.Render(diagram, &d2svg.RenderOpts{IsometricExploded: boolPointer(true)})
				if err != nil {
					t.Fatal(err)
				}
				if len(boards) != 1 || !bytes.Equal(single, boards[0]) {
					t.Fatal("expected a single stack from both SVG entry points")
				}
				if !bytes.Contains(single, []byte("-isometric-exploded-deck-")) {
					t.Fatal("exploded SVG must namespace its floor anchors separately from ordinary isometric SVGs")
				}
			} else if len(boards) != 2 {
				t.Fatalf("ordinary render returned %d boards", len(boards))
			}
		})
	}
}

func TestInvalidIsometricLayerGap(t *testing.T) {
	for _, v := range []string{"-1", "1.2", "1000000001", "tall"} {
		if _, _, err := Compile(context.Background(), "layers: {book: {vars: {d2-config: {isometric-layer-gap: "+v+"}}}}", nil, nil); err == nil {
			t.Errorf("accepted invalid gap %s", v)
		}
	}
}
