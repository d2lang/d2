//go:build tinygo && js && wasm

package d2fonts

import "os"

// Browser workers have no system font roots. TinyGo also has no os.SameFile,
// so decline any candidate whose identity we cannot verify.
func sameFontFile(listed, opened os.FileInfo) bool {
	return false
}
