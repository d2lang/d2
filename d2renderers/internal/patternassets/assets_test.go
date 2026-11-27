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
		{name: "paper SVG", data: []byte(PaperSVG()), bytes: 419795, digest: "6d2131fa5828dedfc0648b93ee3dd4f2492f706dc8ec81a2f7106f063f99487c"},
		{name: "paper Brotli", data: paperBrotli, bytes: 132925, digest: "12f137f63add58ee23c90db58a1a592df5e1e114d487d013270959be8e26e19f"},
		{name: "grain PNG", data: GrainPNG(), bytes: 20845, digest: "7aec37844fa033cbdb586efaa1955dc6f43d56707c0b1bf91e8b5e32386d8d69"},
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
		{name: "grain", output: GrainSVG(), bytes: 28443, digest: "e23dfd58ed7febaf5c2485c087585953c4d42e37cbca621f0bfc46a8381ef075"},
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
