package font

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// A coordinate accumulator can overflow int16 even though its individual
// TrueType deltas fit. Other fonts leave instructions on a zero-contour glyph.
// WOFF2 cannot represent these with the ordinary transform that we use.
func TestWOFF2PreservesUnsupportedGlyphData(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count uint16
		body  []byte
	}{
		{"instructed-empty", 0, []byte{0, 1, 0}},
		{"wrapped-coordinate", 1, []byte{0, 1, 0, 0, 0x21, 0x21, 0x7f, 0xff, 0, 1}},
		{"truncated-flags", 1, []byte{0, 1, 0, 0, 0x39}},
		{"truncated-composite", 0xffff, []byte{0, 0x80, 0, 1, 0, 0}},
		{"unknown-contour-format", 0xfffe, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := make([]byte, 10)
			binary.BigEndian.PutUint16(raw, tc.count)
			raw = append(raw, tc.body...)
			loca := make([]byte, 8)
			binary.BigEndian.PutUint32(loca[4:], uint32(len(raw)))
			if got, _ := transformGlyphs(raw, loca, 1, 1); got != nil {
				t.Fatalf("transformed unrepresentable glyph (%d bytes)", len(got))
			}
		})
	}
	// Overlap only applies to the first simple-glyph point. Its bitmap lives
	// after all seven streams, and is absent entirely when all bits are zero.
	raw := append(make([]byte, 10), 0, 0, 0, 0, 0x71)
	binary.BigEndian.PutUint16(raw, 1)
	loca := []byte{0, 0, 0, 0, 0, 0, 0, byte(len(raw))}
	got, _ := transformGlyphs(raw, loca, 1, 1)
	if len(got) < 36 || binary.BigEndian.Uint16(got[2:]) != 1 || !bytes.HasSuffix(got, []byte{0x80}) {
		t.Fatal("simple overlap flag lost")
	}
}
