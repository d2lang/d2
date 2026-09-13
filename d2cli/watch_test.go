package d2cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/d2lang/d2/lib/localfile"
)

func TestTrackedFSHonorsLocalFilePolicy(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside.d2")
	if err := os.WriteFile(inside, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideRoot := t.TempDir()
	outside := filepath.Join(outsideRoot, "outside.d2")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	rooted, err := localfile.Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rooted.Close() })
	tracked := trackedFS{localFiles: rooted}
	file, err := tracked.Open(inside)
	if err != nil {
		t.Fatalf("open rooted regular file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if len(tracked.opened) != 1 || tracked.opened[0] != inside {
		t.Fatalf("tracked opens = %q, want [%q]", tracked.opened, inside)
	}

	if _, err := tracked.Open(outside); !errors.Is(err, localfile.ErrDenied) {
		t.Fatalf("outside-root Open error = %v, want localfile.ErrDenied", err)
	}
	if _, err := tracked.Open(root); err == nil {
		t.Fatal("trackedFS accepted a directory")
	}
	if len(tracked.opened) != 1 {
		t.Fatalf("failed opens were tracked: %q", tracked.opened)
	}

	var denied trackedFS
	if _, err := denied.Open(inside); !errors.Is(err, localfile.ErrDenied) {
		t.Fatalf("zero-policy Open error = %v, want localfile.ErrDenied", err)
	}

	trusted := trackedFS{localFiles: localfile.Unrestricted()}
	file, err = trusted.Open(outside)
	if err != nil {
		t.Fatalf("trusted CLI Open error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if len(trusted.opened) != 1 || trusted.opened[0] != outside {
		t.Fatalf("trusted tracked opens = %q, want [%q]", trusted.opened, outside)
	}
}
