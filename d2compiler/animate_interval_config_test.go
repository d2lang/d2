package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

func TestAnimateIntervalConfig(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int64
	}{
		{"0", 0},
		{"800", 800},
		{"800ms", 800},
		{"2s", 2000},
		{"1.25s", 1250},
		{"${hold}", 250},
	} {
		t.Run(test.value, func(t *testing.T) {
			src := "vars: {hold: 250ms; d2-config: {animate-interval: " + test.value + "}}"
			_, config, err := d2compiler.Compile("interval.d2", strings.NewReader(src), nil)
			if err != nil {
				t.Fatal(err)
			}
			if config == nil || config.AnimateInterval == nil || *config.AnimateInterval != test.want {
				t.Fatalf("config = %+v, want animate interval %d ms", config, test.want)
			}
		})
	}

	for _, value := range []string{"-1", "-1s", "1.5", "1.5ms", "10us", "0.0000000001s", "-0.0000000001s", "later", "99999999999999999999", "4s {extra: true}", "[4s]"} {
		t.Run("invalid/"+value, func(t *testing.T) {
			src := "vars: {d2-config: {animate-interval: " + value + "}}"
			_, _, err := d2compiler.Compile("interval.d2", strings.NewReader(src), nil)
			if err == nil || !strings.Contains(err.Error(), "animate-interval") {
				t.Fatalf("error = %v, want animate-interval diagnostic", err)
			}
		})
	}
}

func TestAnimateProgressConfig(t *testing.T) {
	const source = `vars: {show: true; d2-config: {animate-progress: ${show}}}
a
steps: {next: {b}}`
	_, config, err := d2compiler.Compile("progress.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !config.ResolveAnimateProgress(nil) {
		t.Fatal("progress should be enabled by the source")
	}

	for _, tc := range []struct {
		source, diagnostic string
	}{
		{"vars.d2-config.animate-progress: sometimes", "expected a boolean"},
		{"vars.d2-config.animate-progress: true {extra: 1}", "expected a boolean"},
		{"vars.d2-config.animate-progress: [true]", "needs a value"},
		{"layers: {sub: {vars.d2-config.animate-progress: true}}", "root vars.d2-config"},
		{source + "\nsteps.next.vars.d2-config.animate-progress: false", "root vars.d2-config"},
	} {
		_, _, err := d2compiler.Compile("invalid.d2", strings.NewReader(tc.source), nil)
		if err == nil || !strings.Contains(err.Error(), tc.diagnostic) || !strings.Contains(err.Error(), "animate-progress") {
			t.Errorf("%s: expected %q diagnostic for animate-progress, got %v", tc.source, tc.diagnostic, err)
		}
	}
}
