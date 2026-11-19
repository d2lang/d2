//go:build tinygo && js && wasm

package localfile

import "os"

// TinyGo does not implement os.Root. D2.js imports are served by memfs.
type fileRoot struct{}

func openRoot(string) (*fileRoot, error)        { return nil, ErrDenied }
func (*fileRoot) Open(string) (*os.File, error) { return nil, ErrDenied }
func (*fileRoot) Close() error                  { return nil }
