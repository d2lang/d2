//go:build !unix

package d2cli

import "io"

// Go's os.File.Close cancels pending pipe writes on Windows.
func videoPipeWriter(dst io.WriteCloser) (io.WriteCloser, func() error, error) {
	return dst, nil, nil
}
