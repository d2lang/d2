package d2cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestAnimateProgressCLI(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		env, flags   []string
		want         bool
	}{
		{name: "default"},
		{name: "source", config: "animate-progress: true", want: true},
		{name: "positive flag", flags: []string{"--animate-progress"}, want: true},
		{name: "false flag over source", config: "animate-progress: true", flags: []string{"--animate-progress=false"}},
		{name: "environment over source", config: "animate-progress: false", env: []string{"D2_ANIMATE_PROGRESS=true"}, want: true},
		{name: "false environment over source", config: "animate-progress: true", env: []string{"D2_ANIMATE_PROGRESS=false"}},
		{name: "flag over environment", env: []string{"D2_ANIMATE_PROGRESS=true"}, flags: []string{"--animate-progress=false"}},
		{name: "subtree tour uses root config", config: "animate-progress: true", flags: []string{"--target=layers.sub.*"}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "vars: {d2-config: {animate-interval: 80ms; " + tc.config + "}}\na\nlayers: {sub: {b; steps: {next: {c}}}}"
			out, err := imageCLIWithEnv(t, t.TempDir(), source, tc.env, append(tc.flags, "--stdout-format=svg", "-")...)
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Contains(out, []byte(`class="d2-progress-`)); got != tc.want {
				t.Errorf("progress overlay present = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestAnimateProgressDoesNotStartSVGPlayback(t *testing.T) {
	out, err := imageCLI(t, t.TempDir(), "a", "--animate-progress", "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("d2-progress-")) || bytes.Contains(out, []byte("@keyframes")) {
		t.Error("static SVG unexpectedly contains animation")
	}
}

func TestAnimateProgressColorPrecedence(t *testing.T) {
	const source = `vars.d2-config: {animate-interval: 80ms; animate-progress-color: "#735acf"}
root
layers: {sub: {a; steps: {next: {b}}}}`
	for _, tc := range []struct {
		name  string
		env   []string
		flags []string
		color string
	}{
		{name: "source", color: "#735acf"},
		{name: "environment", env: []string{"D2_ANIMATE_PROGRESS_COLOR=#34b6c8"}, color: "#34b6c8"},
		{name: "flag over environment", env: []string{"D2_ANIMATE_PROGRESS_COLOR=#34b6c8"}, flags: []string{"--animate-progress-color=#E31"}, color: "#E31"},
		{name: "empty override uses source", flags: []string{"--animate-progress-color="}, color: "#735acf"},
		{name: "subtree", flags: []string{"--target=layers.sub.*"}, color: "#735acf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := imageCLIWithEnv(t, t.TempDir(), source, tc.env, append(tc.flags, "--animate-progress", "--stdout-format=svg", "-")...)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(out, []byte(tc.color)) || !bytes.Contains(out, []byte(`class="d2-progress-`)) {
				t.Errorf("missing progress line with color %q", tc.color)
			}
		})
	}
	out, err := imageCLI(t, t.TempDir(), source, "--stdout-format=svg", "-")
	if err != nil || bytes.Contains(out, []byte("d2-progress-")) {
		t.Fatalf("color unexpectedly enabled progress: %v", err)
	}
	for _, arg := range []string{"--animate-progress-color=transparent", "--animate-progress-color=unknown"} {
		out, err := imageCLI(t, t.TempDir(), "a", arg, "--stdout-format=svg", "-")
		if err == nil || !strings.Contains(err.Error(), "--animate-progress-color") || len(out) != 0 {
			t.Errorf("%s: expected a color diagnostic and empty output, got %v (%d bytes)", arg, err, len(out))
		}
	}
}
