//go:build !tinygo

package version

import "runtime/debug"

const modulePath = "github.com/d2lang/d2"

func init() {
	if Version == "(devel)" {
		if info, ok := debug.ReadBuildInfo(); ok {
			Version = moduleVersion(info)
		}
	}
}

func moduleVersion(info *debug.BuildInfo) string {
	mod := &info.Main
	if mod.Path != modulePath {
		for _, dep := range info.Deps {
			if dep.Path == modulePath {
				mod = dep
				break
			}
		}
	}
	if mod.Path == modulePath {
		if mod.Replace != nil {
			mod = mod.Replace
		}
		if mod.Version != "" {
			return mod.Version
		}
	}
	return "(devel)"
}
