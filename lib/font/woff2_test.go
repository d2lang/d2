package font_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"testing"

	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"

	"github.com/d2lang/brotli"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/lib/font"
)

// Independently encoded with FontTools' WOFF2 writer using the corresponding
// SubsetWOFF tables, with recalcBBoxes and recalcTimestamp disabled. These
// hashes cover the uncompressed transformed outline streams, including the
// contour points, original instructions, components, overlap bits and bounds.
// The Brotli bitstream and choice of directory order can change independently.
func TestWOFF2Subsets(t *testing.T) {
	expected := map[string]string{
		"HandDrawn-bold":         "57c176f030cd3259615566841b2a52a1650ec7c3957e2cb19c7821a34c065aea",
		"HandDrawn-italic":       "983c6d00f07f0f2c164c2978d03f87e79b634e2175c1efbc7a6d0f515dba8388",
		"HandDrawn-regular":      "983c6d00f07f0f2c164c2978d03f87e79b634e2175c1efbc7a6d0f515dba8388",
		"HandDrawn-semibold":     "57c176f030cd3259615566841b2a52a1650ec7c3957e2cb19c7821a34c065aea",
		"SourceCodePro-bold":     "659fc69bc65e20f02887eaaf2df1d1edc2d627a49e12166873f86ca5372d4ee3",
		"SourceCodePro-italic":   "eeac0ea89f4580f7d7d10a1944994ba70865417bc0d87b742d89b565b8a45b4c",
		"SourceCodePro-regular":  "8cc2f560ae1dd9eb7be3377ec1042c5009b0ebe9f5f9ae46effc4f8021998bb2",
		"SourceCodePro-semibold": "4d7ea97bf237fd47193e49b5238fc57b43c560eb1a3ee2661a0a1a20fa5361af",
		"SourceSansPro-bold":     "924ca6823fade8756d3f106f3aacb613950aaf2edb85eccca0c81e1365d2b6a0",
		"SourceSansPro-italic":   "356c99582ba8dcbd2cf92411efd03e215ce2c1cebf12354466082a50a42bd8eb",
		"SourceSansPro-regular":  "c1222daf30c3895aed86f2faa253250fa205d99e35a28d564b97893277fd68f8",
		"SourceSansPro-semibold": "5afc8533f6835d1d4531ac630fe5f6bb8205f04d3ef8dba253c455bf37d10c62",
	}
	corpora := []string{"", " A", "0123456789", "fÁêÅöçńÉÆœﬃ", "🙂مرحباαЖ", strings.Repeat(" ", 99)}
	var alphabet []rune
	for r := rune(0x20); r < 0x600; r++ {
		alphabet = append(alphabet, r)
	}
	corpora = append(corpora, string(alphabet))
	random := rand.New(rand.NewSource(17))
	for i := range 12 {
		label := make([]rune, (i+1)*6)
		for j := range label {
			label[j] = alphabet[random.Intn(len(alphabet))]
		}
		corpora = append(corpora, string(label))
	}
	for _, family := range []d2fonts.FontFamily{d2fonts.SourceSansPro, d2fonts.SourceCodePro, d2fonts.HandDrawn} {
		for _, style := range d2fonts.FontStyles {
			name := string(family) + "-" + string(style)
			t.Run(name, func(t *testing.T) {
				face := d2fonts.FontFaces.Get(family.Font(0, style))
				series := sha256.New()
				for _, corpus := range corpora {
					before, err := font.SubsetWOFF(face, corpus)
					if err != nil {
						t.Fatal(err)
					}
					after, err := font.SubsetWOFF2(face, corpus)
					if err != nil {
						t.Fatal(err)
					}
					glyphs := checkWOFF2(t, before, after)
					series.Write(binary.BigEndian.AppendUint32(nil, uint32(len(glyphs))))
					series.Write(glyphs)
				}
				if got := fmt.Sprintf("%x", series.Sum(nil)); got != expected[name] {
					t.Errorf("transformed glyphs differ from FontTools: %s", got)
				}
			})
		}
	}
}

func checkWOFF2(t *testing.T, original, encoded []byte) []byte {
	t.Helper()
	be := binary.BigEndian
	if len(encoded) < 48 || string(encoded[:4]) != "wOF2" || be.Uint32(encoded[4:]) != 0x10000 || int(be.Uint32(encoded[8:])) != len(encoded) || len(encoded)%4 != 0 {
		t.Fatal("invalid WOFF2 SFNT header")
	}
	loader, err := opentype.NewLoader(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	// WOFF2 table 3. Unknown indices are errors for the subsetter's table set.
	tags := []string{"cmap", "head", "hhea", "hmtx", "maxp", "name", "OS/2", "post", "cvt ", "fpgm", "glyf", "loca", "prep", "CFF ", "VORG", "EBDT", "EBLC", "gasp"}
	rows := int(be.Uint16(encoded[12:]))
	if rows != int(be.Uint16(original[12:])) {
		t.Fatal("table count changed")
	}
	in := bytes.NewReader(encoded[48:])
	readLength := func() int {
		n := 0
		for i := 0; i < 5; i++ {
			b, err := in.ReadByte()
			if err != nil || i == 0 && b == 0x80 {
				t.Fatal("invalid UIntBase128 length")
			}
			n = n<<7 | int(b&0x7f)
			if b < 0x80 {
				return n
			}
		}
		t.Fatal("overlong UIntBase128 length")
		return 0
	}
	type entry struct {
		tag              string
		original, length int
	}
	entries := make([]entry, rows)
	for i := range entries {
		b, err := in.ReadByte()
		if err != nil || int(b&63) >= len(tags) {
			t.Fatal("unexpected table flag", b)
		}
		tag := tags[b&63]
		n := readLength()
		entries[i] = entry{tag, n, n}
		if tag == "glyf" || tag == "loca" {
			if b>>6 != 0 {
				t.Fatalf("%s was not transformed", tag)
			}
			entries[i].length = readLength()
			if tag == "loca" && (i == 0 || i == rows-1 || entries[i-1].tag != "glyf" || entries[i].length != 0) {
				t.Fatal("invalid transformed loca entry")
			}
		}
	}
	sfntSize := 12 + 16*len(entries)
	for _, e := range entries {
		sfntSize += (e.original + 3) &^ 3
	}
	if sfntSize != int(be.Uint32(encoded[16:])) {
		t.Fatal("incorrect reconstructed SFNT size")
	}
	data, err := io.ReadAll(brotli.NewReader(io.LimitReader(in, int64(be.Uint32(encoded[20:])))))
	if err != nil {
		t.Fatal(err)
	}
	var glyphs []byte
	for _, entry := range entries {
		if entry.length > len(data) {
			t.Fatalf("short table %s", entry.tag)
		}
		got := data[:entry.length]
		data = data[entry.length:]
		want, err := loader.RawTable(opentype.MustNewTag(entry.tag))
		if entry.tag == "glyf" {
			head, err1 := loader.RawTable(opentype.MustNewTag("head"))
			maxp, err2 := loader.RawTable(opentype.MustNewTag("maxp"))
			loca, err3 := loader.RawTable(opentype.MustNewTag("loca"))
			if err1 != nil || err2 != nil || err3 != nil {
				t.Fatal("missing TrueType directory")
			}
			offsets, err := tables.ParseLoca(loca, int(be.Uint16(maxp[4:])), be.Uint16(head[50:]) != 0)
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for i := 1; i < len(offsets); i++ {
				n += (int(offsets[i]-offsets[i-1]) + 3) &^ 3
			}
			if n != entry.original {
				t.Fatalf("wrong normalized glyf length: got %d, want %d", entry.original, n)
			}
		} else if err != nil || len(want) != entry.original {
			t.Fatalf("wrong original table %s: %v", entry.tag, err)
		}
		switch entry.tag {
		case "glyf":
			glyphs = got
		case "loca": // Reconstructed from the glyph stream.
		case "head":
			copy(want[8:12], got[8:12]) // The reconstructed sfnt gets its own checksum.
			want[16] |= 0x08            // Required WOFF2 flags bit 11.
			if !bytes.Equal(want, got) {
				t.Fatal("head changed")
			}
		default:
			if !bytes.Equal(want, got) {
				t.Fatalf("%s changed", entry.tag)
			}
		}
	}
	if len(data) != 0 || len(glyphs) == 0 {
		t.Fatal("invalid WOFF2 data length")
	}
	return glyphs
}
