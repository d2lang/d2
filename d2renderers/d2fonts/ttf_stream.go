package d2fonts

import (
	"bytes"
	"errors"
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
	r := brotli.NewReader(bytes.NewReader(stream))
	faces := make(map[string][]byte, len(bundledTTFStreamFaces))
	for _, face := range bundledTTFStreamFaces {
		// Each face gets an allocation of its own. On every collection, for
		// every pointer, TinyGo's collector finds the object pointed into by
		// walking forward to the object's last block. D2 keeps pointers to
		// the first byte of each face, so in one shared buffer each of those
		// walks would cross every later face as well.
		ttf := make([]byte, face.size)
		if n, err := io.ReadFull(r, ttf); err != nil {
			return nil, fmt.Errorf("bundled TTF stream: read %d of %d bytes of %s: %v", n, face.size, face.name, err)
		}
		faces[face.name] = ttf
	}
	var extra [1]byte
	switch _, err := io.ReadFull(r, extra[:]); err {
	case io.EOF:
		return faces, nil
	case nil:
		return nil, errors.New("bundled TTF stream: longer than its faces")
	default:
		return nil, fmt.Errorf("bundled TTF stream: %v", err)
	}
}
