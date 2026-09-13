package imgbundler

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/simplelog"
)

func TestBundleLocalDeniesByDefault(t *testing.T) {
	path := writeLocalPolicyImage(t, t.TempDir(), "image.svg", `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	_, err := BundleLocal(localPolicyContext(t), localPolicyLogger(t), "-", localPolicySVG(path), false)
	if err == nil {
		t.Fatal("BundleLocal read a local file without an explicit policy")
	}
}

func TestBundleLocalRootedPolicy(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLocalPolicyImage(t, root, "inside.svg", `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	outside := writeLocalPolicyImage(t, parent, "outside.svg", `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	policy, err := localfile.Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = policy.Close() })
	ctx := localPolicyContext(t)
	logger := localPolicyLogger(t)
	output, err := BundleLocalWithPolicy(ctx, logger, filepath.Join(root, "input.d2"), localPolicySVG("inside.svg"), policy, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), `data:image/svg+xml;base64,`) {
		t.Fatalf("in-root image was not bundled: %s", output)
	}
	for _, href := range []string{"../outside.svg", outside} {
		if _, err := BundleLocalWithPolicy(ctx, logger, filepath.Join(root, "input.d2"), localPolicySVG(href), policy, false); err == nil {
			t.Fatalf("rooted policy bundled outside path %q", href)
		}
	}
}

func TestBundleLocalRootedPolicyRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks commonly requires elevated Windows privileges")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := writeLocalPolicyImage(t, parent, "outside.svg", `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	if err := os.Symlink(outside, filepath.Join(root, "escape.svg")); err != nil {
		t.Fatal(err)
	}
	policy, err := localfile.Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = policy.Close() })
	_, err = BundleLocalWithPolicy(localPolicyContext(t), localPolicyLogger(t), filepath.Join(root, "input.d2"), localPolicySVG("escape.svg"), policy, false)
	if err == nil {
		t.Fatal("rooted policy followed an escaping symlink")
	}
}

func TestBundleLocalCacheDoesNotCrossRootReplacement(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "js" || runtime.GOOS == "plan9" {
		t.Skip("directory identity across renames is not portable to this platform")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	movedRoot := filepath.Join(parent, "moved-root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	const original = `<svg xmlns="http://www.w3.org/2000/svg"><title>original</title></svg>`
	const replacement = `<svg xmlns="http://www.w3.org/2000/svg"><title>replacement</title></svg>`
	writeLocalPolicyImage(t, root, "asset.svg", original)
	firstPolicy, err := localfile.Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstPolicy.Close() })
	ctx := localPolicyContext(t)
	logger := simplelog.FromLibLog(ctx)
	inputPath := filepath.Join(root, "input.d2")
	if _, err := BundleLocalWithPolicy(ctx, logger, inputPath, localPolicySVG("asset.svg"), firstPolicy, true); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(root, movedRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLocalPolicyImage(t, root, "asset.svg", replacement)
	secondPolicy, err := localfile.Rooted(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondPolicy.Close() })
	output, err := BundleLocalWithPolicy(ctx, logger, inputPath, localPolicySVG("asset.svg"), secondPolicy, true)
	if err != nil {
		t.Fatal(err)
	}
	want := base64.StdEncoding.EncodeToString([]byte(replacement))
	if !strings.Contains(string(output), want) {
		t.Fatal("replacement root reused the previous root's cached asset")
	}
}

func TestBundleLocalCapsLocalFileBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxImageSize + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = BundleLocalWithPolicy(localPolicyContext(t), localPolicyLogger(t), "-", localPolicySVG(path), localfile.Unrestricted(), false)
	if err == nil {
		t.Fatal("BundleLocal accepted an oversized local image")
	}
}

func localPolicySVG(href string) []byte {
	return []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg"><image href="%s"/></svg>`, href))
}

func writeLocalPolicyImage(t *testing.T, directory, name, contents string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func localPolicyContext(t *testing.T) context.Context {
	t.Helper()
	return log.With(context.Background(), testlog.New(t))
}

func localPolicyLogger(t *testing.T) simplelog.Logger {
	t.Helper()
	return simplelog.FromLibLog(localPolicyContext(t))
}
