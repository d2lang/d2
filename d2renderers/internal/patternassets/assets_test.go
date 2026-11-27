package patternassets

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestCanonicalAssetHashes(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		bytes  int
		digest string
	}{
		{name: "paper SVG", data: []byte(PaperSVG()), bytes: 414575, digest: "d6f43553eeca8f5bf37f08720dfef17be958f349639cb328c6b1669b4cbfb944"},
		{name: "paper Brotli", data: paperBrotli, bytes: 131360, digest: "feee16c7085f5b3abe263388b10e13c5e703524f5effe49642c3eafdb429873d"},
		{name: "grain PNG", data: GrainPNG(), bytes: 19079, digest: "50730e6c56603eac7ad90780979ed2de1538488c56f709aa6795d6a24975fb84"},
		{name: "streak path", data: []byte(StreakPathData()), bytes: 8878, digest: "76e01a8aa5a6228fa08bed2c4b607cabc6c936e79777994df662c5fb8e9f4ffc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := len(test.data); got != test.bytes {
				t.Fatalf("bytes = %d, want %d", got, test.bytes)
			}
			if got := assetDigest(test.data); got != test.digest {
				t.Fatalf("SHA-256 = %s, want %s", got, test.digest)
			}
		})
	}
}

func TestSVGWrappersMatchCanonicalOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		bytes  int
		digest string
	}{
		{name: "grain", output: GrainSVG(), bytes: 26087, digest: "e830b29a299408e89d9f0c848e3a61299dd410166c1691bdde4fd3bf3ba55fda"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := len(test.output); got != test.bytes {
				t.Fatalf("bytes = %d, want %d", got, test.bytes)
			}
			if got := assetDigest([]byte(test.output)); got != test.digest {
				t.Fatalf("SHA-256 = %s, want %s", got, test.digest)
			}
		})
	}
}

func assetDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%x", digest)
}
