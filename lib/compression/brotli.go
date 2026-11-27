// d2 uses compression for compressing large static assets when its built into WASM for d2.js
package compression

import (
	"bytes"
	"fmt"
	"io"

	"github.com/d2lang/brotli"
)

func DecompressBrotli(compressed []byte) (string, error) {
	reader := brotli.NewReader(bytes.NewReader(compressed))

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to decompress: %w", err)
	}

	return string(decompressed), nil
}
