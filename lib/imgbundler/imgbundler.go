// Package imgbundler embeds generated SVG image references as data URIs.
// Loading, validation, network and local-file policy, caching, and cumulative
// resource budgets belong to the caller-provided imageasset.Resolver; this
// package only scans generated SVG and performs bounded replacement.
package imgbundler

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/d2lang/d2/lib/imageasset"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/simplelog"
)

const (
	maxImageSize    int64 = 1 << 25 // 33_554_432
	maxImageWorkers       = 16

	// The resolver bounds fetched, decompressed, retained, and decoded bytes.
	// These independent ceilings bound generated-SVG scanning and expansion.
	maxImageReferences             = 4_096
	maxImageReferenceBytes         = 64 << 10
	maxBundledImageBytes     int64 = 512 << 20
	maxBundledOutputBytes    int64 = 512 << 20
	maxImageDecodedWidth           = 32_768
	maxImageDecodedHeight          = 32_768
	maxImageDecodedPixels    int64 = 64 << 20
	maxReportedImageFailures       = 8
	maxImageErrorLabelBytes        = 1_024
	legacyCacheNamespace           = "imgbundler/imageasset/v2/default-http"
)

var imageRegex = regexp.MustCompile(`<image href="([^"]+)"`)

// BundleOptions selects the references eligible for bundling. Resolver is one
// cumulative-budget session, normally scoped to exactly one output document.
type BundleOptions struct {
	Resolver *imageasset.Resolver
	Local    bool
	Remote   bool
}

// BundleWithResolver embeds selected local and HTTP(S) references using one
// imageasset resolver session. Successful replacements completed before a
// failure are returned with that error, preserving D2's partial-SVG behavior.
// Resolution is canceled after the first failure so failed resources cannot
// multiply the resolver's per-resource I/O ceiling without bound.
func BundleWithResolver(ctx context.Context, l simplelog.Logger, in []byte, options BundleOptions) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	matches, hrefs, err := findImageElementsForOptions(ctx, in, options)
	if err != nil {
		return in, fmt.Errorf("failed to bundle images: %w", err)
	}
	if len(hrefs) == 0 {
		return in, nil
	}
	if options.Resolver == nil {
		return in, errors.New("failed to bundle images: image resolver is required")
	}
	output, err := runResolverWorkers(ctx, l, in, matches, hrefs, options.Resolver, &bundleBudget{maxBundledBytes: maxBundledImageBytes})
	if err != nil {
		return output, fmt.Errorf("failed to bundle images: %w", err)
	}
	return output, nil
}

// BundleLocal bundles local image references using the deny-by-default local
// file policy. Call BundleLocalWithPolicy to deliberately permit local files.
//
// Deprecated: construct an imageasset.Resolver and call BundleWithResolver so
// one caller-owned session can cover local and remote references together.
func BundleLocal(ctx context.Context, l simplelog.Logger, inputPath string, in []byte, cacheImages bool) ([]byte, error) {
	return BundleLocalWithPolicy(ctx, l, inputPath, in, localfile.Policy{}, cacheImages)
}

// BundleLocalWithPolicy bundles local image references allowed by localFiles.
// Use localfile.Rooted for untrusted input. localfile.Unrestricted is intended
// only for trusted local applications such as the D2 command-line interface.
// cacheImages is retained for source compatibility and creates only a
// per-invocation cache; use an explicit Resolver cache for cross-call reuse.
//
// Deprecated: construct an imageasset.Resolver and call BundleWithResolver so
// one caller-owned session can cover local and remote references together.
func BundleLocalWithPolicy(ctx context.Context, l simplelog.Logger, inputPath string, in []byte, localFiles localfile.Policy, cacheImages bool) ([]byte, error) {
	baseDir := ""
	if inputPath != "" && inputPath != "-" {
		baseDir = filepath.Dir(inputPath)
	}
	resolver, err := newLegacyResolver(baseDir, localFiles, nil, netpolicy.Policy{}, cacheImages)
	if err != nil {
		return in, fmt.Errorf("failed to bundle local images: %w", err)
	}
	defer resolver.CloseIdleConnections()
	output, err := BundleWithResolver(ctx, l, in, BundleOptions{Resolver: resolver, Local: true})
	if err != nil {
		return output, fmt.Errorf("failed to bundle local images: %w", err)
	}
	return output, nil
}

// BundleRemote bundles public HTTP(S) image references.
//
// Deprecated: construct an imageasset.Resolver and call BundleWithResolver.
func BundleRemote(ctx context.Context, l simplelog.Logger, in []byte, cacheImages bool) ([]byte, error) {
	return BundleRemoteWithPolicy(ctx, l, in, cacheImages, netpolicy.Policy{})
}

// BundleRemoteWithPolicy bundles HTTP(S) images under policy. The zero policy
// permits public destinations only; trusted callers may explicitly opt into
// private-network assets. cacheImages is retained for source compatibility and
// creates only a per-invocation cache; use an explicit Resolver cache for
// cross-call reuse.
//
// Deprecated: construct an imageasset.Resolver and call BundleWithResolver so
// one caller-owned session can cover local and remote references together.
func BundleRemoteWithPolicy(ctx context.Context, l simplelog.Logger, in []byte, cacheImages bool, policy netpolicy.Policy) ([]byte, error) {
	resolver, err := newLegacyResolver("", localfile.Policy{}, nil, policy, cacheImages)
	if err != nil {
		return in, fmt.Errorf("failed to bundle remote images: %w", err)
	}
	defer resolver.CloseIdleConnections()
	output, err := BundleWithResolver(ctx, l, in, BundleOptions{Resolver: resolver, Remote: true})
	if err != nil {
		return output, fmt.Errorf("failed to bundle remote images: %w", err)
	}
	return output, nil
}

func newLegacyResolver(baseDir string, localFiles localfile.Policy, client *http.Client, policy netpolicy.Policy, cacheImages bool) (*imageasset.Resolver, error) {
	var cache imageasset.Cache
	cacheNamespace := ""
	if cacheImages {
		var err error
		cache, err = imageasset.NewMemoryCache(maxImageReferences, maxBundledImageBytes)
		if err != nil {
			return nil, err
		}
		cacheNamespace = legacyCacheNamespace
	}
	return imageasset.New(imageasset.Options{
		BaseDir:        baseDir,
		LocalFiles:     localFiles,
		HTTPClient:     client,
		NetworkPolicy:  policy,
		Cache:          cache,
		CacheNamespace: cacheNamespace,
		Limits: imageasset.Limits{
			MaxFetchedBytes:           maxImageSize,
			MaxEncodedBytes:           maxImageSize,
			MaxDecompressedBytes:      maxImageSize,
			MaxSVGBytes:               maxImageSize,
			MaxDecodedWidth:           maxImageDecodedWidth,
			MaxDecodedHeight:          maxImageDecodedHeight,
			MaxDecodedPixels:          maxImageDecodedPixels,
			MaxAssets:                 maxImageReferences,
			MaxCumulativeEncodedBytes: maxBundledImageBytes,
			MaxCumulativeDecodedBytes: maxBundledImageBytes,
		},
	})
}

type repl struct {
	href string
	to   []byte
	err  error
}

type imageMatch struct {
	start     int
	end       int
	hrefStart int
	hrefEnd   int
}

type bundleLimitError struct {
	name   string
	actual int64
	limit  int64
}

func (e *bundleLimitError) Error() string {
	return fmt.Sprintf("%s exceeds maximum of %d bytes: %d", e.name, e.limit, e.actual)
}

type bundleBudget struct {
	mu               sync.Mutex
	bundledBytes     int64
	maxBundledBytes  int64
	exhaustedByError *bundleLimitError
}

func (b *bundleBudget) reserveBundled(byteCount int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exhaustedByError != nil {
		return b.exhaustedByError
	}
	if byteCount > b.maxBundledBytes-b.bundledBytes {
		b.exhaustedByError = &bundleLimitError{
			name: "cumulative bundled image bytes", actual: b.bundledBytes + byteCount, limit: b.maxBundledBytes,
		}
		return b.exhaustedByError
	}
	b.bundledBytes += byteCount
	return nil
}

type referenceClass uint8

const (
	referenceLocal referenceClass = iota + 1
	referenceRemote
	referenceData
)

func classifyReference(href string) referenceClass {
	decoded := html.UnescapeString(href)
	if len(decoded) >= len("data:") && strings.EqualFold(decoded[:len("data:")], "data:") {
		return referenceData
	}
	parsed, err := url.Parse(decoded)
	if err == nil && strings.HasPrefix(strings.ToLower(parsed.Scheme), "http") {
		return referenceRemote
	}
	return referenceLocal
}

// findImageElements is retained for focused scanner tests and the deprecated
// split APIs. New callers use findImageElementsForOptions through
// BundleWithResolver.
func findImageElements(ctx context.Context, svg []byte, isRemote bool) ([]imageMatch, []string, error) {
	return findImageElementsForOptions(ctx, svg, BundleOptions{Local: !isRemote, Remote: isRemote})
}

// findImageElementsForOptions records every eligible occurrence, but returns
// each raw href only once for resolution. Iterative matching avoids allocating
// an unbounded regexp result before the reference ceiling can be enforced.
func findImageElementsForOptions(ctx context.Context, svg []byte, options BundleOptions) ([]imageMatch, []string, error) {
	var matches []imageMatch
	var hrefs []string
	unique := make(map[string]struct{})
	for offset := 0; offset < len(svg); {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		indices := imageRegex.FindSubmatchIndex(svg[offset:])
		if indices == nil {
			break
		}
		match := imageMatch{
			start:     offset + indices[0],
			end:       offset + indices[1],
			hrefStart: offset + indices[2],
			hrefEnd:   offset + indices[3],
		}
		offset = match.end
		hrefBytes := svg[match.hrefStart:match.hrefEnd]
		href := string(hrefBytes)
		class := classifyReference(href)
		if class == referenceData || class == referenceLocal && !options.Local || class == referenceRemote && !options.Remote {
			continue
		}
		if len(hrefBytes) > maxImageReferenceBytes {
			return nil, nil, &bundleLimitError{name: "image reference", actual: int64(len(hrefBytes)), limit: maxImageReferenceBytes}
		}
		if len(matches) == maxImageReferences {
			return nil, nil, fmt.Errorf("image references exceed maximum of %d", maxImageReferences)
		}
		matches = append(matches, match)
		if _, ok := unique[href]; !ok {
			unique[href] = struct{}{}
			hrefs = append(hrefs, href)
		}
	}
	return matches, hrefs, nil
}

func runResolverWorkers(ctx context.Context, l simplelog.Logger, svg []byte, matches []imageMatch, hrefs []string, resolver *imageasset.Resolver, budget *bundleBudget) ([]byte, error) {
	return runResolverWorkersWithLimit(ctx, l, svg, matches, hrefs, resolver, budget, maxImageWorkers)
}

func runResolverWorkersWithLimit(ctx context.Context, l simplelog.Logger, svg []byte, matches []imageMatch, hrefs []string, resolver *imageasset.Resolver, budget *bundleBudget, maxWorkers int) ([]byte, error) {
	workerCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	jobs := make(chan string)
	results := make(chan repl, len(hrefs))
	workerCount := min(maxWorkers, len(hrefs))
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for range workerCount {
		go func() {
			defer wg.Done()
			for href := range jobs {
				replacement, err := resolveReplacement(workerCtx, resolver, href, budget)
				results <- repl{href: href, to: replacement, err: err}
				if err != nil {
					cancel(err)
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, href := range hrefs {
			select {
			case jobs <- href:
			case <-workerCtx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	replacements := make(map[string][]byte, len(hrefs))
	reportedFailures := 0
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	done := ctx.Done()
	for results != nil {
		select {
		case result, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			if result.err != nil {
				if reportedFailures < maxReportedImageFailures {
					l.Error("failed to bundle image: " + boundedLabel(result.err.Error(), maxImageErrorLabelBytes))
					reportedFailures++
				}
				continue
			}
			replacements[result.href] = result.to
		case <-ticker.C:
			l.Info("fetching images...")
		case <-done:
			cancel(ctx.Err())
			done = nil
		}
	}

	output, replacementErr := applyReplacements(ctx, svg, matches, replacements, maxBundledOutputBytes)
	cause := context.Cause(workerCtx)
	return output, errors.Join(replacementErr, cause)
}

func resolveReplacement(ctx context.Context, resolver *imageasset.Resolver, href string, budget *bundleBudget) ([]byte, error) {
	resource, err := resolver.Resolve(ctx, html.UnescapeString(href))
	if err != nil {
		return nil, err
	}
	if resource == nil {
		return nil, errors.New("image resolver returned nil")
	}
	dataURI, err := resource.DataURIContext(ctx)
	if err != nil {
		return nil, err
	}
	outputBytes := len(`<image href="`) + len(dataURI) + 1
	if err := budget.reserveBundled(int64(outputBytes)); err != nil {
		return nil, err
	}
	output := make([]byte, 0, outputBytes)
	output = append(output, `<image href="`...)
	output = append(output, dataURI...)
	output = append(output, '"')
	return output, nil
}

func boundedLabel(label string, maxBytes int) string {
	if len(label) <= maxBytes {
		return label
	}
	return label[:maxBytes-3] + "..."
}

func applyReplacements(ctx context.Context, svg []byte, matches []imageMatch, replacements map[string][]byte, maxOutputBytes int64) ([]byte, error) {
	if len(replacements) == 0 {
		return svg, nil
	}
	outputBytes := int64(len(svg))
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return svg, err
		}
		replacement, ok := replacements[string(svg[match.hrefStart:match.hrefEnd])]
		if !ok {
			continue
		}
		outputBytes += int64(len(replacement)) - int64(match.end-match.start)
		if outputBytes > maxOutputBytes {
			return svg, &bundleLimitError{name: "bundled SVG output", actual: outputBytes, limit: maxOutputBytes}
		}
	}
	if outputBytes > maxOutputBytes {
		return svg, &bundleLimitError{name: "bundled SVG output", actual: outputBytes, limit: maxOutputBytes}
	}

	output := make([]byte, 0, int(outputBytes))
	previous := 0
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return svg, err
		}
		replacement, ok := replacements[string(svg[match.hrefStart:match.hrefEnd])]
		if !ok {
			continue
		}
		output = append(output, svg[previous:match.start]...)
		output = append(output, replacement...)
		previous = match.end
	}
	output = append(output, svg[previous:]...)
	return output, nil
}
