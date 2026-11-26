package htmlentity

import (
	"html"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"
)

// Unescape must agree with the standard library on every input: links, icons
// and Markdown text reach users through it.
func TestUnescapeMatchesStandardLibrary(t *testing.T) {
	checked := 0
	check := func(s string) {
		t.Helper()
		checked++
		if got, want := Unescape(s), html.UnescapeString(s); got != want {
			t.Fatalf("Unescape(%q) = %q, want %q", s, got, want)
		}
	}

	suffixes := []string{"", ";", "x", "=", "1", " ", ";;", "&", "&amp;", "\x00", "é", "&nGt;", "&nLt;"}
	for _, name := range entityNames(t) {
		for _, suffix := range suffixes {
			check("&" + name + suffix)
			check("a&" + name + suffix + "b")
			check("&" + name[:len(name)-1] + suffix)
		}
	}

	numbers := []string{"", "0", "1", "9", "10", "128", "159", "160", "55295", "55296", "57343", "57344", "65533", "1114111", "1114112", "99999999999999999999", "x", "x0", "x80", "x9f", "X9F", "xD800", "xDFFF", "x10FFFF", "x110000", "xFFFFFFFFFFFF", "xg", "-1", "+1", " 1"}
	for _, number := range numbers {
		for _, suffix := range suffixes {
			check("&#" + number + suffix)
			check("&nGt;&#" + number + suffix)
		}
	}

	for x := 0; x <= 0x120; x++ {
		check("&#" + strconv.Itoa(x) + ";")
		check("&#x" + strconv.FormatInt(int64(x), 16) + ";")
		check("&#" + strconv.Itoa(x) + " ")
	}

	pieces := []string{"&", "&", "#", ";", "x", "X", "a", "m", "p", "l", "t", "g", "n", "G", "L", "q", "u", "o", "1", "9", "0", "f", " ", "=", "é", "\x80", "<", "amp", "lt", "not", "notin", "copy", "nGt;", "nLt;", "&nGt;", "&nLt;", "&n", "Gt;", "\n"}
	random := rand.New(rand.NewSource(1))
	iterations := 2_000_000
	if testing.Short() {
		iterations = 50_000
	}
	for range iterations {
		var s string
		for range 1 + random.Intn(12) {
			s += pieces[random.Intn(len(pieces))]
		}
		check(s)
	}
	t.Logf("%d inputs agree", checked)
}

// entityNames reads the names of the standard library's own table, so that a
// Go release that changes the table is compared name by name.
func entityNames(t *testing.T) []string {
	source, err := os.ReadFile(filepath.Join(runtime.GOROOT(), "src", "html", "entity.go"))
	if err != nil {
		t.Skipf("standard library source is unavailable: %v", err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`"([A-Za-z0-9]+;?)":`).FindAllSubmatch(source, -1) {
		names = append(names, string(match[1]))
	}
	if len(names) < 2000 {
		t.Fatalf("found only %d entity names in the standard library source", len(names))
	}
	return names
}
