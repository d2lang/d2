//go:build !tinygo || !js || !wasm

package localfile

import "os"

type fileRoot = os.Root

func openRoot(name string) (*fileRoot, error) { return os.OpenRoot(name) }
