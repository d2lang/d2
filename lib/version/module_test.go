//go:build !tinygo

package version

import (
	"runtime/debug"
	"testing"
)

func TestModuleVersion(t *testing.T) {
	d2 := debug.Module{Path: modulePath, Version: "v0.10.0"}
	host := debug.Module{Path: "example.org/app", Version: "v3.0.0"}
	for _, tc := range []struct {
		name string
		info debug.BuildInfo
		want string
	}{
		{"go install", debug.BuildInfo{Main: d2}, d2.Version},
		{"embedder", debug.BuildInfo{Main: host, Deps: []*debug.Module{&d2}}, d2.Version},
		{"versioned replacement", debug.BuildInfo{Main: host, Deps: []*debug.Module{{Path: modulePath, Replace: &debug.Module{Path: "example.org/fork", Version: "v0.10.1"}}}}, "v0.10.1"},
		{"local replacement", debug.BuildInfo{Main: host, Deps: []*debug.Module{{Path: modulePath, Version: d2.Version, Replace: &debug.Module{Path: "../d2"}}}}, "(devel)"},
		{"unknown", debug.BuildInfo{Main: host}, "(devel)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := moduleVersion(&tc.info); got != tc.want {
				t.Errorf("moduleVersion = %q, want %q", got, tc.want)
			}
		})
	}
}
