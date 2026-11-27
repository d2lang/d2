package font

import (
	"encoding/binary"

	"github.com/go-text/typesetting/font/opentype/tables"
)

// WOFF2 5.1 separates contours, points, instructions and component records so
// each stream can be compressed more efficiently. On unsupported glyph data,
// encodeWOFF2 keeps glyf and loca untransformed.
type woff2Glyphs struct {
	contours, points, flags, coordinates []byte
	components, bounds, instructions     []byte
	explicitBounds, overlaps             []byte
}

func transformGlyphs(glyf, loca []byte, glyphs, indexFormat int) ([]byte, uint32) {
	be := binary.BigEndian
	if indexFormat != 0 && indexFormat != 1 {
		return nil, 0
	}
	offsets, err := tables.ParseLoca(loca, glyphs, indexFormat == 1)
	if err != nil {
		return nil, 0
	}
	s := woff2Glyphs{explicitBounds: make([]byte, 4*((glyphs+31)/32)), overlaps: make([]byte, (glyphs+7)/8)}
	var originalLength uint64
	for i := range glyphs {
		start, end := offsets[i], offsets[i+1]
		if end < start || uint64(end) > uint64(len(glyf)) {
			return nil, 0
		}
		// Reconstructed TrueType glyphs may occupy up to four-byte boundaries.
		// A decoder can use the declared glyf length to bound glyph reads.
		originalLength += (uint64(end-start) + 3) &^ 3
		if !s.glyph(i, glyf[start:end]) {
			return nil, 0
		}
	}
	if originalLength > 0xffffffff || indexFormat == 0 && originalLength > 2*0xffff {
		return nil, 0
	}
	overlap := false
	for _, v := range s.overlaps {
		if v != 0 {
			overlap = true
			break
		}
	}
	streams := [...][]byte{s.contours, s.points, s.flags, s.coordinates, s.components, append(s.explicitBounds, s.bounds...), s.instructions}
	total := 36
	for _, stream := range streams {
		total += len(stream)
	}
	if overlap {
		total += len(s.overlaps)
	}
	result := make([]byte, 36, total)
	if overlap {
		be.PutUint16(result[2:], 1)
	}
	be.PutUint16(result[4:], uint16(glyphs))
	be.PutUint16(result[6:], uint16(indexFormat))
	for i, stream := range streams {
		be.PutUint32(result[8+i*4:], uint32(len(stream)))
		result = append(result, stream...)
	}
	if overlap {
		result = append(result, s.overlaps...)
	}
	return result, uint32(originalLength)
}

func (s *woff2Glyphs) glyph(i int, raw []byte) bool {
	be := binary.BigEndian
	if len(raw) == 0 {
		s.contours = be.AppendUint16(s.contours, 0)
		return true
	}
	if len(raw) < 10 {
		return false
	}
	n := int16(be.Uint16(raw))
	s.contours = append(s.contours, raw[:2]...)
	mask := byte(0x80) >> (i % 8)
	var explicit bool
	switch {
	case n > 0:
		var ok bool
		explicit, ok = s.simpleGlyph(i, raw, int(n), mask)
		if !ok {
			return false
		}
	case n == -1:
		data := raw[10:]
		length, hasInstructions := compositeLength(data)
		if length == 0 {
			return false
		}
		s.components = append(s.components, data[:length]...)
		if hasInstructions {
			code := data[length:]
			if len(code) < 2 || int(be.Uint16(code)) > len(code)-2 {
				return false
			}
			s.addInstructions(code[2 : 2+int(be.Uint16(code))])
		}
		explicit = true
	default:
		// A stored zero-contour glyph can carry instructions and metrics.
		// WOFF2 would reconstruct it as an empty loca entry.
		return false
	}
	if explicit {
		s.explicitBounds[i/8] |= mask
		s.bounds = append(s.bounds, raw[2:10]...)
	}
	return true
}

func (s *woff2Glyphs) simpleGlyph(i int, raw []byte, n int, mask byte) (explicitBounds, ok bool) {
	be := binary.BigEndian
	g, _, err := tables.ParseSimpleGlyph(raw[10:], n)
	if err != nil || len(g.Points) == 0 {
		return false, false
	}
	count := 0
	for _, endpoint := range g.EndPtsOfContours {
		next := int(endpoint) + 1
		if next <= count || next-count > 65535 {
			return false, false
		}
		s.points = append255(s.points, next-count)
		count = next
	}
	if g.Points[0].Flag&0x40 != 0 {
		s.overlaps[i/8] |= mask
	}
	xmin, ymin := g.Points[0].X, g.Points[0].Y
	xmax, ymax := xmin, ymin
	var x, y int
	for _, point := range g.Points {
		if point.Flag&0x80 != 0 {
			return false, false
		}
		dx, dy := int(point.X)-x, int(point.Y)-y
		// The reader accumulates coordinates in int16. A larger difference
		// means they wrapped. Preserve the original glyf for such a face.
		if dx < -32768 || dx > 32767 || dy < -32768 || dy > 32767 {
			return false, false
		}
		s.triplet(dx, dy, point.Flag&1 != 0)
		x, y = int(point.X), int(point.Y)
		xmin, ymin = min(xmin, point.X), min(ymin, point.Y)
		xmax, ymax = max(xmax, point.X), max(ymax, point.Y)
	}
	explicit := int16(be.Uint16(raw[2:])) != xmin || int16(be.Uint16(raw[4:])) != ymin ||
		int16(be.Uint16(raw[6:])) != xmax || int16(be.Uint16(raw[8:])) != ymax
	s.addInstructions(g.Instructions)
	return explicit, true
}

// Return the end of the component records, preserving their argument and
// transformation bytes verbatim. The optional instructions follow all records.
func compositeLength(data []byte) (int, bool) {
	pos := 0
	hasInstructions := false
	for len(data)-pos >= 4 {
		flags := binary.BigEndian.Uint16(data[pos:])
		if flags&0x0100 != 0 {
			hasInstructions = true
		}
		pos += 6
		if flags&0x0001 != 0 {
			pos += 2
		}
		switch {
		case flags&0x0008 != 0:
			pos += 2
		case flags&0x0040 != 0:
			pos += 4
		case flags&0x0080 != 0:
			pos += 8
		}
		if pos > len(data) {
			return 0, false
		}
		if flags&0x0020 == 0 {
			return pos, hasInstructions
		}
	}
	return 0, false
}

func (s *woff2Glyphs) addInstructions(code []byte) {
	s.coordinates = append255(s.coordinates, len(code))
	s.instructions = append(s.instructions, code...)
}

func append255(dst []byte, v int) []byte {
	switch {
	case v < 253:
		return append(dst, byte(v))
	case v < 506:
		return append(dst, 255, byte(v-253))
	case v < 762:
		return append(dst, 254, byte(v-506))
	default:
		return append(dst, 253, byte(v>>8), byte(v))
	}
}

// Triplet flags select one of six signed coordinate-delta encodings in WOFF2
// table 18. Single-axis moves and small deltas need just one data byte.
func (s *woff2Glyphs) triplet(x, y int, onCurve bool) {
	xSign, ySign := 1, 1
	if x < 0 {
		x = -x
		xSign = 0
	}
	if y < 0 {
		y = -y
		ySign = 0
	}
	sign := xSign | ySign<<1
	var flag int
	switch {
	case x == 0 && y < 1280:
		flag = (y>>8)*2 + ySign
		s.coordinates = append(s.coordinates, byte(y))
	case y == 0 && x < 1280:
		flag = 10 + (x>>8)*2 + xSign
		s.coordinates = append(s.coordinates, byte(x))
	case x < 65 && y < 65:
		flag = 20 + ((x - 1) & 0x30) + (((y - 1) & 0x30) >> 2) + sign
		s.coordinates = append(s.coordinates, byte((x-1)<<4|(y-1)&15))
	case x < 769 && y < 769:
		flag = 84 + 12*((x-1)>>8) + 4*((y-1)>>8) + sign
		s.coordinates = append(s.coordinates, byte(x-1), byte(y-1))
	case x < 4096 && y < 4096:
		flag = 120 + sign
		s.coordinates = append(s.coordinates, byte(x>>4), byte((x&15)<<4|y>>8), byte(y))
	default:
		flag = 124 + sign
		s.coordinates = append(s.coordinates, byte(x>>8), byte(x), byte(y>>8), byte(y))
	}
	if !onCurve {
		flag |= 0x80
	}
	s.flags = append(s.flags, byte(flag))
}
