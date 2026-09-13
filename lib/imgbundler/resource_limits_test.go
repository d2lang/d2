package imgbundler

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/imageasset"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/simplelog"
)

func TestBundleCapsImageReferences(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "image.svg")
	if err := os.WriteFile(imagePath, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o600); err != nil {
		t.Fatal(err)
	}

	makeSource := func(references int) []byte {
		var source strings.Builder
		source.WriteString(`<svg xmlns="http://www.w3.org/2000/svg">`)
		for range references {
			fmt.Fprintf(&source, `<image href="%s"/>`, imagePath)
		}
		source.WriteString(`</svg>`)
		return []byte(source.String())
	}

	ctx := log.With(context.Background(), testlog.New(t))
	if _, err := BundleLocalWithPolicy(ctx, simplelog.FromLibLog(ctx), "-", makeSource(maxImageReferences), localfile.Unrestricted(), false); err != nil {
		t.Fatalf("inclusive reference limit failed: %v", err)
	}
	_, err := BundleLocalWithPolicy(ctx, simplelog.FromLibLog(ctx), "-", makeSource(maxImageReferences+1), localfile.Unrestricted(), false)
	if err == nil || !strings.Contains(err.Error(), "image references exceed maximum of 4096") {
		t.Fatalf("reference-limit error = %v", err)
	}
}

func TestApplyReplacementsCapsOutputAndHandlesDuplicates(t *testing.T) {
	source := []byte(`<svg><image href="same"/><g/><image href="same"/></svg>`)
	matches, hrefs, err := findImageElements(context.Background(), source, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || len(hrefs) != 1 {
		t.Fatalf("got %d matches and %d unique hrefs", len(matches), len(hrefs))
	}
	replacement := []byte(`<image href="data:image/png;base64,AAAA"`)
	want := bytes.ReplaceAll(source, []byte(`<image href="same"`), replacement)

	output, err := applyReplacements(context.Background(), source, matches, map[string][]byte{"same": replacement}, int64(len(want)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, want) {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
	limited, err := applyReplacements(context.Background(), source, matches, map[string][]byte{"same": replacement}, int64(len(want)-1))
	if err == nil || !strings.Contains(err.Error(), "bundled SVG output exceeds maximum") {
		t.Fatalf("expected output limit error, got %v", err)
	}
	if !bytes.Equal(limited, source) {
		t.Fatalf("output-limit failure returned invalid partial output: %s", limited)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	canceledOutput, err := applyReplacements(canceled, source, matches, map[string][]byte{"same": replacement}, int64(len(want)))
	if !errors.Is(err, context.Canceled) || !bytes.Equal(canceledOutput, source) {
		t.Fatalf("canceled apply = %q / %v, want original / context.Canceled", canceledOutput, err)
	}
}

func TestBundleBudgetIsCumulativeAndSticky(t *testing.T) {
	budget := &bundleBudget{maxBundledBytes: 6}
	if err := budget.reserveBundled(4); err != nil {
		t.Fatal(err)
	}
	if err := budget.reserveBundled(3); err == nil {
		t.Fatal("budget accepted cumulative bytes above its limit")
	}
	if err := budget.reserveBundled(2); err == nil {
		t.Fatal("budget continued after its first limit error")
	}
}

func TestResolveReplacementEnforcesCumulativeBundleBudget(t *testing.T) {
	resolver, err := imageasset.New(imageasset.Options{Limits: testResolverLimits()})
	if err != nil {
		t.Fatal(err)
	}
	href := "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNGFile)
	resource, err := resolver.Resolve(context.Background(), href)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := resource.DataURIContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	replacementBytes := int64(len(`<image href="`) + len(uri) + 1)
	if _, err := resolveReplacement(context.Background(), resolver, href, &bundleBudget{maxBundledBytes: replacementBytes - 1}); err == nil || !strings.Contains(err.Error(), "cumulative bundled image bytes") {
		t.Fatalf("bundle-budget error = %v", err)
	}
}

func TestBundleWithResolverCombinesLocalAndRemoteReferences(t *testing.T) {
	directory := t.TempDir()
	localPath := filepath.Join(directory, "local.png")
	if err := os.WriteFile(localPath, testPNGFile, 0o600); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		requests.Add(1)
		if req.URL.String() != "https://example.com/remote.png" {
			t.Fatalf("unexpected URL %s", req.URL)
		}
		response := httptest.NewRecorder()
		response.Header().Set("Content-Type", "image/png")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(testPNGFile)
		return response.Result()
	})}
	resolver, err := imageasset.New(imageasset.Options{
		BaseDir: directory, LocalFiles: localfile.Unrestricted(), HTTPClient: client,
		NetworkPolicy: netpolicy.Policy{AllowPrivateNetworks: true}, Limits: testResolverLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resolver.CloseIdleConnections()
	source := []byte(`<svg><image href="local.png"/><image href="https://example.com/remote.png"/></svg>`)
	ctx := log.With(context.Background(), testlog.New(t))
	output, err := BundleWithResolver(ctx, simplelog.FromLibLog(ctx), source, BundleOptions{
		Resolver: resolver, Local: true, Remote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || strings.Count(string(output), "data:image/png;base64,") != 2 {
		t.Fatalf("requests = %d, output = %s", requests.Load(), output)
	}
}

func TestBundleWithResolverRejectsUnsupportedFormats(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) *http.Response {
		response := httptest.NewRecorder()
		response.Header().Set("Content-Type", "image/avif")
		response.WriteHeader(http.StatusOK)
		_, _ = response.WriteString("not one of the five supported image formats")
		return response.Result()
	})}
	resolver, err := imageasset.New(imageasset.Options{
		HTTPClient: client, NetworkPolicy: netpolicy.Policy{AllowPrivateNetworks: true}, Limits: testResolverLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	source := []byte(`<svg><image href="https://example.com/unsupported.avif"/></svg>`)
	ctx := log.With(context.Background(), testlog.New(t))
	output, err := BundleWithResolver(ctx, simplelog.FromLibLog(ctx), source, BundleOptions{Resolver: resolver, Remote: true})
	if err == nil || !strings.Contains(err.Error(), "unsupported or malformed image") {
		t.Fatalf("unsupported-format error = %v", err)
	}
	if !bytes.Equal(output, source) {
		t.Fatalf("failed unsupported resource changed output: %s", output)
	}
}

func TestResolutionFailureStopsSchedulingNewFetches(t *testing.T) {
	var source strings.Builder
	source.WriteString(`<svg>`)
	for i := range 100 {
		fmt.Fprintf(&source, `<image href="https://example.com/%d"/>`, i)
	}
	source.WriteString(`</svg>`)
	sourceBytes := []byte(source.String())
	matches, hrefs, err := findImageElements(context.Background(), sourceBytes, true)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) *http.Response {
		requests.Add(1)
		response := httptest.NewRecorder()
		response.Header().Set("Content-Type", "image/png")
		response.WriteHeader(http.StatusOK)
		_, _ = response.WriteString("not a PNG")
		return response.Result()
	})}
	resolver, err := imageasset.New(imageasset.Options{
		HTTPClient: client, NetworkPolicy: netpolicy.Policy{AllowPrivateNetworks: true}, Limits: testResolverLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := log.With(context.Background(), testlog.New(t))
	output, err := runResolverWorkers(ctx, simplelog.FromLibLog(ctx), sourceBytes, matches, hrefs, resolver, &bundleBudget{maxBundledBytes: 1 << 20})
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("resolution error = %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("first resolution failure was replaced by context cancellation: %v", err)
	}
	if !bytes.Equal(output, sourceBytes) {
		t.Fatal("failed resources changed output")
	}
	if got := requests.Load(); got > maxImageWorkers {
		t.Fatalf("failure allowed %d requests, worker ceiling %d", got, maxImageWorkers)
	}
}

func TestResolutionFailurePreservesCompletedReplacements(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) *http.Response {
		response := httptest.NewRecorder()
		response.Header().Set("Content-Type", "image/png")
		response.WriteHeader(http.StatusOK)
		if strings.HasSuffix(req.URL.Path, "/good.png") {
			_, _ = response.Write(testPNGFile)
		} else {
			_, _ = response.WriteString("not a PNG")
		}
		return response.Result()
	})}
	resolver, err := imageasset.New(imageasset.Options{
		HTTPClient: client, NetworkPolicy: netpolicy.Policy{AllowPrivateNetworks: true}, Limits: testResolverLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	source := []byte(`<svg><image href="https://example.com/good.png"/><image href="https://example.com/bad.png"/></svg>`)
	matches, hrefs, err := findImageElements(context.Background(), source, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := log.With(context.Background(), testlog.New(t))
	output, err := runResolverWorkersWithLimit(ctx, simplelog.FromLibLog(ctx), source, matches, hrefs, resolver, &bundleBudget{maxBundledBytes: 1 << 20}, 1)
	if err == nil || !strings.Contains(err.Error(), "unsupported or malformed image") {
		t.Fatalf("resolution error = %v", err)
	}
	if !strings.Contains(string(output), "data:image/png;base64,") || !strings.Contains(string(output), `href="https://example.com/bad.png"`) {
		t.Fatalf("completed replacement was not preserved alongside failed source: %s", output)
	}
}

func TestBundleSkipsLargeDataURI(t *testing.T) {
	href := "data:image/png;base64," + strings.Repeat("A", maxImageReferenceBytes)
	source := []byte(fmt.Sprintf(`<svg><image href="%s"/></svg>`, href))
	ctx := log.With(context.Background(), testlog.New(t))
	output, err := BundleRemote(ctx, simplelog.FromLibLog(ctx), source, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, source) {
		t.Fatal("already-bundled data URI changed")
	}
}

func TestBundleIgnoresOversizedReferencesForOtherPass(t *testing.T) {
	remoteHref := "https://example.com/" + strings.Repeat("a", maxImageReferenceBytes)
	source := []byte(fmt.Sprintf(`<svg><image href="%s"/></svg>`, remoteHref))
	ctx := log.With(context.Background(), testlog.New(t))
	output, err := BundleLocalWithPolicy(ctx, simplelog.FromLibLog(ctx), "-", source, localfile.Unrestricted(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, source) {
		t.Fatal("remote reference changed during local bundling")
	}
}

func TestBundleCapsEligibleReferenceBytes(t *testing.T) {
	href := strings.Repeat("a", maxImageReferenceBytes+1)
	source := []byte(fmt.Sprintf(`<svg><image href="%s"/></svg>`, href))
	ctx := log.With(context.Background(), testlog.New(t))
	_, err := BundleLocalWithPolicy(ctx, simplelog.FromLibLog(ctx), "-", source, localfile.Unrestricted(), false)
	if err == nil || !strings.Contains(err.Error(), "image reference exceeds maximum") {
		t.Fatalf("expected image-reference limit error, got %v", err)
	}
}

func testResolverLimits() imageasset.Limits {
	return imageasset.Limits{
		MaxFetchedBytes: 1 << 20, MaxEncodedBytes: 1 << 20, MaxDecompressedBytes: 1 << 20, MaxSVGBytes: 1 << 20,
		MaxDecodedWidth: 1_000, MaxDecodedHeight: 1_000, MaxDecodedPixels: 1_000_000,
		MaxAssets: 1_000, MaxCumulativeEncodedBytes: 1 << 20, MaxCumulativeDecodedBytes: 8 << 20,
	}
}
