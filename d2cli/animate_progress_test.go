package d2cli

import (
	"bytes"
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
