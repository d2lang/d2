package imageasset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/d2lang/d2/lib/localfile"
)

func TestLocalFilesDeniedByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, encodePNG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := New(Options{Limits: generousLimits()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), path)
	if !errors.Is(err, localfile.ErrDenied) {
		t.Fatalf("Resolve error = %v, want localfile.ErrDenied", err)
	}
}

func TestRootedLocalFilesContainResolution(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	insidePath := filepath.Join(root, "inside.png")
	outsidePath := filepath.Join(parent, "outside.png")
	if err := os.WriteFile(insidePath, encodePNG(t, 2, 3), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsidePath, encodePNG(t, 4, 5), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := localfile.Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = policy.Close() })
	resolver, err := New(Options{LocalFiles: policy, Limits: generousLimits()})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"inside.png", insidePath} {
		resource, err := resolver.Resolve(context.Background(), source)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", source, err)
		}
		assertResource(t, resource, KindRaster, "image/png", 2, 3)
	}
	for _, source := range []string{"../outside.png", outsidePath} {
		if _, err := resolver.Resolve(context.Background(), source); !errors.Is(err, localfile.ErrDenied) {
			t.Fatalf("Resolve(%q) error = %v, want localfile.ErrDenied", source, err)
		}
	}
}

func TestRootedLocalFilesRejectSymlinkEscapeAndOversize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks commonly requires elevated Windows privileges")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(parent, "outside.png")
	if err := os.WriteFile(outsidePath, encodePNG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(root, "escape.png")); err != nil {
		t.Fatal(err)
	}
	oversizedPath := filepath.Join(root, "oversized.png")
	if err := os.WriteFile(oversizedPath, encodePNG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := localfile.Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = policy.Close() })
	limits := generousLimits()
	limits.MaxFetchedBytes = 1
	resolver, err := New(Options{LocalFiles: policy, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "escape.png"); err == nil {
		t.Fatal("resolver followed a symlink outside the configured root")
	}
	_, err = resolver.Resolve(context.Background(), "oversized.png")
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || limitErr.Name != "fetched bytes" {
		t.Fatalf("oversized local error = %v, want fetched-byte LimitError", err)
	}
}

func TestLocalCacheCannotCrossPolicyBoundary(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(parent, "outside.png")
	if err := os.WriteFile(outsidePath, encodePNG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	cache, err := NewMemoryCache(4, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	limits := generousLimits()
	prime, err := New(Options{
		BaseDir:        root,
		LocalFiles:     localfile.Unrestricted(),
		Cache:          cache,
		CacheNamespace: "policy-boundary",
		Limits:         limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prime.Resolve(context.Background(), outsidePath); err != nil {
		t.Fatal(err)
	}
	rootedPolicy, err := localfile.Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rootedPolicy.Close() })
	rooted, err := New(Options{
		LocalFiles:     rootedPolicy,
		Cache:          cache,
		CacheNamespace: "policy-boundary",
		Limits:         limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rooted.Resolve(context.Background(), outsidePath); !errors.Is(err, localfile.ErrDenied) {
		t.Fatalf("rooted resolver reused unrestricted cache entry: %v", err)
	}
	if len(cache.entries) != 1 {
		t.Fatalf("denied resolve changed cache size to %d", len(cache.entries))
	}
}
