package localfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestZeroPolicyDeniesLocalFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	var policy Policy
	if _, err := policy.Open(path); !errors.Is(err, ErrDenied) {
		t.Fatalf("Open error = %v, want ErrDenied", err)
	}
	if _, err := policy.CacheKey(path); !errors.Is(err, ErrDenied) {
		t.Fatalf("CacheKey error = %v, want ErrDenied", err)
	}
}

func TestRootedPolicyContainsOpens(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "inside.txt")
	outside := filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(inside, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = policy.Close() })

	for _, name := range []string{"inside.txt", inside} {
		file, err := policy.Open(name)
		if err != nil {
			t.Fatalf("Open(%q): %v", name, err)
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read/close %q: %v / %v", name, readErr, closeErr)
		}
		if string(data) != "inside" {
			t.Fatalf("Open(%q) = %q", name, data)
		}
	}
	for _, name := range []string{"../outside.txt", outside} {
		if _, err := policy.Open(name); !errors.Is(err, ErrDenied) {
			t.Fatalf("Open(%q) error = %v, want ErrDenied", name, err)
		}
	}
}

func TestRootedPolicyRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks commonly requires elevated Windows privileges")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	policy, err := Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = policy.Close() })
	if _, err := policy.Open("escape"); err == nil {
		t.Fatal("Open followed a symlink outside the configured root")
	}
}

func TestRootedPolicyPinsDirectoryAcrossPathReplacement(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "js" || runtime.GOOS == "plan9" {
		t.Skip("directory identity across renames is not portable to this platform")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	movedRoot := filepath.Join(parent, "moved-root")
	replacement := filepath.Join(parent, "replacement")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(replacement, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "value"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, "value"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = policy.Close() })
	if err := os.Rename(root, movedRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(replacement, root); err != nil {
		t.Fatal(err)
	}

	file, err := policy.Open("value")
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read/close pinned file: %v / %v", readErr, closeErr)
	}
	if string(data) != "original" {
		t.Fatalf("Open after root replacement = %q, want original directory", data)
	}
}

func TestRootedPolicyCacheScopeIsPerInstance(t *testing.T) {
	root := t.TempDir()
	first, err := Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	firstKey, err := first.CacheKey("value")
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := second.CacheKey("value")
	if err != nil {
		t.Fatal(err)
	}
	if firstKey == secondKey {
		t.Fatal("distinct rooted policy instances share a cache scope")
	}
}

func TestUnrestrictedPolicyIsExplicitAndCacheScoped(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	rooted, err := Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rooted.Close() })
	unrestricted := Unrestricted()
	rootedKey, err := rooted.CacheKey(path)
	if err != nil {
		t.Fatal(err)
	}
	unrestrictedKey, err := unrestricted.CacheKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if rootedKey == unrestrictedKey {
		t.Fatal("rooted and unrestricted cache keys collide")
	}
	file, err := unrestricted.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRootedPolicyCloseIsSharedAndCopySafe(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "value"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	copies := make([]Policy, 32)
	for index := range copies {
		copies[index] = policy
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(copies))
	for copy := range copies {
		wg.Add(1)
		go func(policyCopy Policy) {
			defer wg.Done()
			errs <- policyCopy.Close()
		}(copies[copy])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if err := policy.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
	if _, err := copies[0].Open("value"); err == nil {
		t.Fatal("Policy copy retained access after shared root was closed")
	}
	if err := (Policy{}).Close(); err != nil {
		t.Fatalf("zero Policy Close: %v", err)
	}
	if err := Unrestricted().Close(); err != nil {
		t.Fatalf("unrestricted Policy Close: %v", err)
	}
}
