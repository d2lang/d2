package d2animate

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
)

func TestSVGProgressFollowsPlayback(t *testing.T) {
	root := d2target.NewDiagram()
	off := false
	svgs := [][]byte{[]byte(`<g viewBox="0 0 20 20"></g>`)}
	for _, wrap := range []func() ([]byte, error){
		func() ([]byte, error) { return Wrap(root, svgs, d2svg.RenderOpts{}, 160) },
		func() ([]byte, error) { return WrapTimed(root, svgs, d2svg.RenderOpts{}, []int{160}) },
		func() ([]byte, error) {
			return WrapTimeline(root, nil, []Frame{{Diagram: root, DurationMS: 160}}, svgs, d2svg.RenderOpts{}, 0)
		},
	} {
		for _, enabled := range []bool{true, false} {
			root.Config = nil
			if !enabled {
				root.Config = &d2target.Config{AnimateProgress: &off}
			}
			out, err := wrap()
			if err != nil || strings.Contains(string(out), `class="d2-progress-`) != enabled {
				t.Fatalf("timed SVG expected progress %t, error %v", enabled, err)
			}
		}
	}
	root.Config = nil
	out, err := WrapTimed(root, nil, d2svg.RenderOpts{}, nil)
	if err != nil || strings.Contains(string(out), "d2-progress-") {
		t.Fatalf("empty playback unexpectedly drew a progress line, error %v", err)
	}
}

func TestSVGProgressClock(t *testing.T) {
	root := d2target.NewDiagram()
	on, off := true, false
	root.Config = &d2target.Config{AnimateProgress: &on}
	frames := [][]byte{[]byte(`<g viewBox="-14 -6 60 24"></g>`), []byte(`<g></g>`)}
	lightAccent, darkAccent := "#123456", "#abcdef"
	opts := d2svg.RenderOpts{
		Pad: new(int64), DarkThemeID: &d2themescatalog.DarkMauve.ID,
		ThemeOverrides: &d2target.ThemeOverrides{B1: &lightAccent}, DarkThemeOverrides: &d2target.ThemeOverrides{B1: &darkAccent},
	}
	wrap := func() string {
		t.Helper()
		out, err := WrapTimed(root, frames, opts, []int{250, 750})
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	hash, _ := root.HashID(nil)
	line := ".d2-progress-" + hash
	active := wrap()
	for _, want := range []string{
		"@keyframes d2Progress-" + hash + "{from{transform:scaleX(0)}to{transform:scaleX(1)}}",
		line + "{fill:var(--d2-progress-light)}", "@media screen and (prefers-color-scheme:dark){" + line + "{fill:var(--d2-progress-dark)}}",
		`style="--d2-progress-light:#123456;--d2-progress-dark:#abcdef;`,
		"@media(prefers-reduced-motion:reduce){" + line + "{visibility:hidden;animation:none!important}}",
		`x="-14" y="12" width="60" height="6" transform="scale(0 1)" pointer-events="none" aria-hidden="true"`,
		"1000ms linear infinite",
	} {
		if !strings.Contains(active, want) {
			t.Errorf("missing progress rule or geometry %q", want)
		}
	}
	if strings.Count(active, `<rect class="d2-progress-`) != 1 {
		t.Fatal("the tour should have one shared progress line")
	}
	// A second inline rendering of this diagram has the same selector. Its
	// single-theme paint must survive the first rendering's dark media rule.
	opts.DarkThemeID = nil
	if !strings.Contains(wrap(), `style="fill:#123456;animation:d2Progress-`) {
		t.Fatal("single-theme line is missing its own fill")
	}
	opts.AnimateProgress = &off
	if strings.Contains(wrap(), "d2Progress-") {
		t.Fatal("explicit false did not disable the source's progress line")
	}
}

func TestSVGProgressFitsShortCanvas(t *testing.T) {
	on := true
	opts := d2svg.RenderOpts{Pad: new(int64), AnimateProgress: &on}
	frame := []byte(`<g viewBox="0 0 12 3"></g>`)
	out, err := Wrap(d2target.NewDiagram(), [][]byte{frame}, opts, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `x="0" y="0" width="12" height="3"`) {
		t.Fatal("progress line should fit the three-unit viewport")
	}
}

func TestSVGProgressColor(t *testing.T) {
	root := d2target.NewDiagram()
	on := true
	root.Config = &d2target.Config{AnimateProgress: &on, AnimateProgressColor: "rebeccapurple"}
	hash, _ := root.HashID(nil)
	line := ".d2-progress-" + hash
	defaultAccent, darkAccent := "#701234", "#abcdef"
	opts := d2svg.RenderOpts{
		DarkThemeID: &d2themescatalog.DarkMauve.ID, ThemeOverrides: &d2target.ThemeOverrides{B1: &defaultAccent},
		DarkThemeOverrides: &d2target.ThemeOverrides{B1: &darkAccent},
	}
	frames := [][]byte{[]byte(`<g></g>`)}
	for _, tc := range []struct{ override, want string }{{"", "rebeccapurple"}, {"#aBC", "#aBC"}} {
		opts.AnimateProgressColor = tc.override
		out, err := Wrap(root, frames, opts, 1000)
		if err != nil {
			t.Fatal(err)
		}
		if css := string(out); !strings.Contains(css, `style="fill:`+tc.want+";animation:d2Progress-") || strings.Contains(css, line+"{fill:") {
			t.Errorf("expected one inline light/dark progress color %q", tc.want)
		}
	}
	for _, invalid := range []string{"currentColor", "transparent", "#1234", "#12345678", "url(#paint)", "red}*{opacity:0}"} {
		opts.AnimateProgressColor = invalid
		if out, err := Wrap(root, frames, opts, 1000); err == nil || out != nil {
			t.Errorf("invalid progress paint %q produced %d bytes and error %v", invalid, len(out), err)
		}
	}
}

func TestProgressColorThemeFallback(t *testing.T) {
	theme := d2themescatalog.DarkMauve
	for _, paint := range []string{"", "Transparent", "currentColor", "#11000044", "bogus"} {
		if got := ProgressColor(&theme.ID, &d2target.ThemeOverrides{B1: &paint}); got != theme.Colors.B1 {
			t.Errorf("theme B1 %q resolved to %q, want %q", paint, got, theme.Colors.B1)
		}
	}
}
