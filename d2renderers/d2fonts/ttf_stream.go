package d2fonts

import (
	"bytes"
	"fmt"
	"io"

	"github.com/d2lang/brotli"
)

// The WebAssembly build carries the bundled TTFs as ttf_stream.br: the files
// of ttf/ concatenated and compressed as one brotli stream. The faces share
// tables, so one stream is far smaller than ten, and unlike ten separately
// compressed files it does not make the module larger once the module itself
// is compressed for transport.
//
//go:generate go run gen_ttf_stream.go

type bundledTTFStreamFace struct {
	name string
	size int
}

// decodeBundledTTFStream returns the faces of a stream written by
// gen_ttf_stream.go, keyed by file name. It fails unless the stream holds
// exactly the bytes that bundledTTFStreamFaces describes.
func decodeBundledTTFStream(stream []byte) (map[string][]byte, error) {
	total := 0
	for _, face := range bundledTTFStreamFaces {
		total += face.size
	}
	// One byte more than expected: a stream of the right length ends the
	// read early, exactly at total.
	buf := make([]byte, total+1)
	n, err := io.ReadFull(brotli.NewReader(bytes.NewReader(stream)), buf)
	if err != io.ErrUnexpectedEOF || n != total {
		return nil, fmt.Errorf("bundled TTF stream: read %d of %d bytes: %v", n, total, err)
	}
	faces := make(map[string][]byte, len(bundledTTFStreamFaces))
	offset := 0
	for _, face := range bundledTTFStreamFaces {
		end := offset + face.size
		// The capacity ends with the face, so appending to one face cannot
		// write into the next.
		faces[face.name] = buf[offset:end:end]
		offset = end
	}
	return faces, nil
}
