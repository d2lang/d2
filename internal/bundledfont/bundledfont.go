// Package bundledfont identifies the font files compiled into D2.
//
// Results derived from a bundled font, such as glyph metrics or an encoded
// subset, can be shared for the life of the process. The same is not true of
// a font a caller registers at run time, which may be supplied afresh for
// every diagram and must not be kept alive by a cache.
package bundledfont

// files maps the first byte of each bundled font file to the file's length.
// It is written only while packages initialize.
var files = make(map[*byte]int)

// Register records data as a bundled font file. It must only be called while
// packages initialize, and the bytes must not be modified afterwards.
func Register(data []byte) {
	if len(data) > 0 {
		files[&data[0]] = len(data)
	}
}

// Is reports whether data is a registered font file itself: the same backing
// array and length, rather than a copy of its contents or a part of it.
func Is(data []byte) bool {
	return len(data) > 0 && files[&data[0]] == len(data)
}
