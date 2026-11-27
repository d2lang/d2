//go:build !js || !wasm

package d2fonts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The WebAssembly build registers its faces from ttf_stream.br instead of the
// files in ttf/. A face that is added, removed or changed without running
// go generate fails here.
func TestBundledTTFStream(t *testing.T) {
	stream, err := os.ReadFile("ttf_stream.br")
	if err != nil {
		t.Fatal(err)
	}
	faces, err := decodeBundledTTFStream(stream)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob("ttf/*.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 || len(paths) != len(faces) || len(paths) != len(bundledTTFStreamFaces) {
		t.Fatalf("ttf/ has %d faces, the stream table %d (%d distinct names); run go generate", len(paths), len(bundledTTFStreamFaces), len(faces))
	}
	for _, path := range paths {
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := faces[filepath.Base(path)]
		if !ok {
			t.Errorf("%s is not in ttf_stream.br; run go generate", path)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from its copy in ttf_stream.br; run go generate", path)
		}
		if cap(got) != len(got) {
			t.Errorf("%s: the stream's face has room to grow into the next one", path)
		}
	}
}

func TestBundledTTFStreamRejectsOtherLengths(t *testing.T) {
	stream, err := os.ReadFile("ttf_stream.br")
	if err != nil {
		t.Fatal(err)
	}
	last := &bundledTTFStreamFaces[len(bundledTTFStreamFaces)-1]
	size := last.size
	defer func() { last.size = size }()
	for _, delta := range []int{-1, 1} {
		last.size = size + delta
		if _, err := decodeBundledTTFStream(stream); err == nil {
			t.Errorf("a table %+d byte off the stream's length was accepted", delta)
		}
	}
	last.size = size
	if _, err := decodeBundledTTFStream(stream[:len(stream)/2]); err == nil {
		t.Error("a truncated stream was accepted")
	}
}
