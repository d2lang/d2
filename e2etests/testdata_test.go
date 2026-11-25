package e2etests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The golden comparison decides every snapshot of the suite, so pin both of
// its outcomes and the acceptance path.
func TestTestdataComparesGoldens(t *testing.T) {
	for _, name := range []string{"TESTDATA_ACCEPT", "TA", "NO_DIFF", "ND"} {
		t.Setenv(name, "")
	}
	path := filepath.Join(t.TempDir(), "golden")
	read := func(kind string) string {
		data, err := os.ReadFile(path + "." + kind + ".txt")
		if err != nil {
			return "<" + kind + " missing>"
		}
		return string(data)
	}
	if err := os.WriteFile(path+".exp.txt", []byte("want\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := testdata(path, ".txt", []byte("want\n")); err != nil || read("got") != "<got missing>" {
		t.Fatalf("matching output: %v, got file %q", err, read("got"))
	}
	err := testdata(path, ".txt", []byte("wanT\n"))
	if err == nil || !strings.Contains(err.Error(), "TESTDATA_ACCEPT") {
		t.Fatalf("output of the same length but different bytes was accepted: %v", err)
	}
	if read("exp") != "want\n" || read("got") != "wanT\n" {
		t.Fatalf("after a mismatch: exp %q, got %q", read("exp"), read("got"))
	}
	t.Setenv("TESTDATA_ACCEPT", "1")
	if err := testdata(path, ".txt", []byte("wanT\n")); err != nil || read("exp") != "wanT\n" || read("got") != "<got missing>" {
		t.Fatalf("accepting: %v, exp %q, got %q", err, read("exp"), read("got"))
	}
}
