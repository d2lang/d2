package textmeasure

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/rivo/uniseg"
)

// The local iterator must report what uniseg.Graphemes reports: text
// measurement, and so every layout, depends on these clusters and widths.
func TestGraphemesMatchUniseg(t *testing.T) {
	samples := []string{
		"", "a", "héllo wörld", "é̂", "🇩🇪🇫🇷🇯", "👩‍👩‍👧‍👦 family", "👍🏽", "☺︎☺️",
		"한국어 조합 각", "\r\n\r\n\n", "क्षि", "\x00\xff\xfe invalid \xed\xa0\x80", "a‍b", "１２３ｗｉｄｅ", "\t tab nbsp",
	}
	pieces := []string{"a", " ", "é", "e", "́", "‍", "👩", "👧", "🏽", "🇩", "🇪", "︎", "️", "한", "ᄀ", "ᅡ", "ᆨ", "क", "्", "ष", "\r", "\n", "\xff", "１", "☺", "؀", "ำ", "\U000e0067", "\U000e007f"}
	random := rand.New(rand.NewSource(1))
	for range 20000 {
		var s string
		for range 1 + random.Intn(10) {
			s += pieces[random.Intn(len(pieces))]
		}
		samples = append(samples, s)
	}
	for _, s := range samples {
		want := uniseg.NewGraphemes(s)
		got := newGraphemes(s)
		for want.Next() {
			if !got.Next() {
				t.Fatalf("%q: iterator ended before cluster %q", s, want.Str())
			}
			wantFrom, wantTo := want.Positions()
			gotFrom, gotTo := got.Positions()
			if got.Str() != want.Str() || got.Width() != want.Width() || !slices.Equal(got.Runes(), want.Runes()) || gotFrom != wantFrom || gotTo != wantTo {
				t.Fatalf("%q: cluster %q width %d at [%d,%d), want %q width %d at [%d,%d)", s, got.Str(), got.Width(), gotFrom, gotTo, want.Str(), want.Width(), wantFrom, wantTo)
			}
		}
		if got.Next() {
			t.Fatalf("%q: extra cluster %q", s, got.Str())
		}
	}
}
