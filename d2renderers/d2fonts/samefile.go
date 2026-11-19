//go:build !tinygo || !js || !wasm

package d2fonts

import "os"

func sameFontFile(listed, opened os.FileInfo) bool {
	return os.SameFile(listed, opened)
}
