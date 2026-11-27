package font

import (
	"encoding/binary"

	"github.com/d2lang/brotli"
)

// SubsetWOFF2 embeds the same TrueType subset as SubsetWOFF in a WOFF2
// container, preserving its outlines, instructions and metrics.
func SubsetWOFF2(face []byte, chars string) ([]byte, error) {
	subset, err := subsetTrueType(face, chars)
	if err != nil {
		return nil, err
	}
	return encodeWOFF2(subset), nil
}

// WOFF2 directory indices for the tables emitted by the subsetter. Other tags
// use the explicit four-byte form. Version 3 is the null transform for glyf
// and loca; for other tables it is version 0.
var woff2Tags = map[string]byte{
	"cmap": 0, "head": 1, "hhea": 2, "hmtx": 3, "maxp": 4, "name": 5,
	"OS/2": 6, "post": 7, "cvt ": 8, "fpgm": 9, "glyf": 0xca, "loca": 0xcb,
	"prep": 12, "gasp": 17,
}

// encodeWOFF2 reads the validated directory assembled by the subsetter.
func encodeWOFF2(subset []byte) []byte {
	be := binary.BigEndian
	tables := int(be.Uint16(subset[4:]))
	output := make([]byte, 48, 48+tables*3)
	copy(output, "wOF2")
	copy(output[4:8], subset[:4])
	be.PutUint16(output[12:], uint16(tables))
	be.PutUint32(output[16:], uint32(len(subset)))
	ordered := make([]string, 0, tables)
	input := make(map[string][]byte, tables)
	for i := range tables {
		entry := subset[12+i*16:]
		tag := string(entry[:4])
		offset, length := be.Uint32(entry[8:]), be.Uint32(entry[12:])
		input[tag] = subset[offset : offset+length]
		if tag == "loca" {
			continue
		}
		ordered = append(ordered, tag)
		if tag == "glyf" {
			// Keep loca immediately after glyf at its original position. Some
			// FreeType versions reject a final transformed (zero-length) loca.
			ordered = append(ordered, "loca")
		}
	}
	head := input["head"]
	copy(output[24:28], head[4:8])
	be.PutUint16(head[16:], be.Uint16(head[16:])|1<<11)
	transformed, glyfLength := transformGlyphs(input["glyf"], input["loca"], int(be.Uint16(input["maxp"][4:])), int(be.Uint16(head[50:])))
	data := make([]byte, 0, len(subset))
	for _, tag := range ordered {
		table := input[tag]
		encoded, ok := woff2Tags[tag]
		if !ok {
			encoded = 63
		}
		isTransformed := transformed != nil && (tag == "glyf" || tag == "loca")
		if isTransformed {
			encoded &= 63
		}
		output = append(output, encoded)
		if encoded == 63 {
			output = append(output, tag...)
		}
		originalLength := uint32(len(table))
		if isTransformed && tag == "glyf" {
			oldPadded := (originalLength + 3) &^ 3
			be.PutUint32(output[16:], uint32(len(subset))-oldPadded+glyfLength)
			originalLength = glyfLength
		}
		output = appendBase128(output, originalLength)
		payload := table
		if isTransformed {
			payload = nil
			if tag == "glyf" {
				payload = transformed
			}
			output = appendBase128(output, uint32(len(payload)))
		}
		data = append(data, payload...)
	}
	compressed := brotli.EncodeFast(data)
	be.PutUint32(output[20:], uint32(len(compressed)))
	output = append(output, compressed...)
	for len(output)%4 != 0 {
		output = append(output, 0)
	}
	be.PutUint32(output[8:], uint32(len(output)))
	return output
}

func appendBase128(dst []byte, n uint32) []byte {
	var encoded [5]byte
	i := len(encoded) - 1
	encoded[i] = byte(n & 0x7f)
	for n >>= 7; n != 0; n >>= 7 {
		i--
		encoded[i] = byte(n&0x7f) | 0x80
	}
	return append(dst, encoded[i:]...)
}
