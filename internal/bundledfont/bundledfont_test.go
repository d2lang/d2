package bundledfont

import "testing"

func TestIsRecognisesOnlyTheRegisteredFile(t *testing.T) {
	data := []byte("a font file")
	if Is(data) {
		t.Fatal("data was recognised before it was registered")
	}
	Register(nil)
	Register(data)
	if !Is(data) {
		t.Fatal("a registered file was not recognised")
	}
	for name, other := range map[string][]byte{
		"a copy":   append([]byte(nil), data...),
		"a prefix": data[:4],
		"a suffix": data[1:],
		"empty":    data[:0],
		"nil":      nil,
	} {
		if Is(other) {
			t.Fatalf("%s of a registered file was recognised as the file", name)
		}
	}
}
