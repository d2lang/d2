// Package imageasset resolves bounded local, HTTP, and data-URI image
// resources without depending on a renderer or scene representation. Animated
// GIF, APNG, and WebP resources use their first frame on the logical canvas;
// later frames are neither decoded nor charged to the decoded-pixel budget.
package imageasset

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/netpolicy"
)

// Preserve imgbundler's worker ceiling so per-resource byte limits also bound
// aggregate in-flight fetch, decompression, validation, and retained bytes.
const maxConcurrentResolutions = 16

const maxResponseHeaderBytes int64 = 64 << 10

// Kind identifies how a resolved resource must be consumed.
type Kind uint8

const (
	KindRaster Kind = iota + 1
	KindSVG
)

// ErrUnavailable identifies a source whose bytes could not be fetched. It is
// reserved for absence and transport failures; malformed image data, policy
// limits, unsupported sources, and caller cancellation do not match it.
var ErrUnavailable = errors.New("imageasset: source unavailable")

type unavailableError struct {
	err error
}

func (e *unavailableError) Error() string { return e.err.Error() }
func (e *unavailableError) Unwrap() error { return e.err }
func (e *unavailableError) Is(target error) bool {
	return target == ErrUnavailable
}

func unavailable(err error) error {
	if err == nil || errors.Is(err, ErrUnavailable) {
		return err
	}
	return &unavailableError{err: err}
}

func (k Kind) String() string {
	switch k {
	case KindRaster:
		return "raster"
	case KindSVG:
		return "svg"
	default:
		return fmt.Sprintf("Kind(%d)", k)
	}
}

// Limits are caller-selected, inclusive ceilings. There are intentionally no
// package defaults: callers must choose budgets appropriate to their corpus.
type Limits struct {
	// MaxFetchedBytes bounds bytes read from a local file or HTTP response and
	// the encoded payload text of a data URI.
	MaxFetchedBytes int64
	// MaxEncodedBytes bounds the final PNG, JPEG, GIF, WebP, or SVG byte stream.
	MaxEncodedBytes int64
	// MaxDecompressedBytes bounds bytes after HTTP content decoding. Identity,
	// local-file, and data-URI sources are checked against it as well.
	MaxDecompressedBytes int64
	// MaxSVGBytes bounds raw or declared-compressed SVG XML before tokenization.
	// Callers that import resolved SVG must align this with the importer's
	// per-source byte ceiling.
	MaxSVGBytes int64

	MaxDecodedWidth  int
	MaxDecodedHeight int
	MaxDecodedPixels int64

	// MaxAssets bounds distinct successfully resolved canonical sources.
	MaxAssets int
	// MaxCumulativeEncodedBytes bounds immutable encoded bytes retained by one
	// Resolver session.
	MaxCumulativeEncodedBytes int64
	// MaxCumulativeDecodedBytes bounds unique canonical sources successfully
	// returned by one Resolver session. Raster resources, including animated
	// inputs represented by their first frame, charge at least width*height*4,
	// or eight bytes per pixel for 16-bit models; SVG resources charge their
	// strict raw byte length.
	MaxCumulativeDecodedBytes int64
}

func (l Limits) Validate() error {
	if l.MaxFetchedBytes <= 0 || l.MaxEncodedBytes <= 0 || l.MaxDecompressedBytes <= 0 || l.MaxSVGBytes <= 0 ||
		l.MaxDecodedWidth <= 0 || l.MaxDecodedHeight <= 0 || l.MaxDecodedPixels <= 0 ||
		l.MaxAssets <= 0 || l.MaxCumulativeEncodedBytes <= 0 || l.MaxCumulativeDecodedBytes <= 0 {
		return errors.New("imageasset: every limit must be positive")
	}
	return nil
}

// Resource is immutable after construction. BytesContext returns an owned
// copy, so a cached Resource can safely be shared between goroutines and
// converted into a scene asset without exposing the cache's backing storage.
type Resource struct {
	kind              Kind
	mimeType          string
	data              []byte
	pixelWidth        int
	pixelHeight       int
	fetchedBytes      int64
	decompressedBytes int64
	decodedBytes      int64
}

func (r *Resource) Kind() Kind          { return r.kind }
func (r *Resource) MIMEType() string    { return r.mimeType }
func (r *Resource) PixelWidth() int     { return r.pixelWidth }
func (r *Resource) PixelHeight() int    { return r.pixelHeight }
func (r *Resource) EncodedBytes() int64 { return int64(len(r.data)) }
func (r *Resource) DecodedBytes() int64 { return r.decodedBytes }

func (r *Resource) fetchedByteCount() int64      { return r.fetchedBytes }
func (r *Resource) decompressedByteCount() int64 { return r.decompressedBytes }
func (r *Resource) cloneBytes() []byte           { return append([]byte(nil), r.data...) }

// BytesContext returns an owned copy while observing cancellation between
// bounded chunks. A canceled copy never exposes partially initialized bytes.
func (r *Resource) BytesContext(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	result := make([]byte, len(r.data))
	const chunkBytes = 32 << 10
	for offset := 0; offset < len(r.data); offset += chunkBytes {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		end := offset + chunkBytes
		if end > len(r.data) {
			end = len(r.data)
		}
		copy(result[offset:end], r.data[offset:end])
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// DataURIContext returns a data URI for the immutable resource while observing
// cancellation between bounded encoding chunks. It encodes the resource's
// private backing bytes directly, avoiding the full owned copy required by
// BytesContext for callers that only need a self-contained representation.
func (r *Resource) DataURIContext(ctx context.Context) ([]byte, error) {
	if r == nil {
		return nil, errors.New("imageasset: nil resource")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	prefix := []byte("data:" + r.mimeType + ";base64,")
	dataBytes := int64(len(r.data))
	if dataBytes > (int64(^uint64(0)>>1)-2)/4*3 {
		return nil, errors.New("imageasset: data URI size overflows int64")
	}
	encodedBytes := (dataBytes + 2) / 3 * 4
	totalBytes := int64(len(prefix)) + encodedBytes
	maxInt := uint64(^uint(0) >> 1)
	if totalBytes < 0 || uint64(totalBytes) > maxInt {
		return nil, errors.New("imageasset: data URI is not representable")
	}
	result := make([]byte, int(totalBytes))
	copy(result, prefix)
	writeAt := len(prefix)
	// A multiple of three lets each non-final chunk encode independently.
	const chunkBytes = 48 << 10
	for readAt := 0; readAt < len(r.data); readAt += chunkBytes {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		end := readAt + chunkBytes
		if end > len(r.data) {
			end = len(r.data)
		}
		base64.StdEncoding.Encode(result[writeAt:], r.data[readAt:end])
		writeAt += base64.StdEncoding.EncodedLen(end - readAt)
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if writeAt != len(result) {
		return nil, errors.New("imageasset: data URI size preflight mismatch")
	}
	return result, nil
}

// Cache is optional. Implementations must be safe for concurrent use. Only
// Resources produced by this package can contain non-empty immutable data.
type Cache interface {
	Get(key string) (*Resource, bool)
	Put(key string, resource *Resource)
}

// MemoryCache is a bounded, concurrency-safe LRU cache. It is never installed
// globally; callers explicitly own it, and Resolver namespaces every key.
type MemoryCache struct {
	mu         sync.Mutex
	entries    map[string]*cacheEntry
	recent     *list.List
	maxEntries int
	maxBytes   int64
	bytes      int64
}

type cacheEntry struct {
	key      string
	resource *Resource
	element  *list.Element
}

// NewMemoryCache returns a cache with inclusive entry and encoded-byte limits.
func NewMemoryCache(maxEntries int, maxBytes int64) (*MemoryCache, error) {
	if maxEntries <= 0 || maxBytes <= 0 {
		return nil, errors.New("imageasset: cache limits must be positive")
	}
	return &MemoryCache{
		entries:    make(map[string]*cacheEntry),
		recent:     list.New(),
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
	}, nil
}

func (c *MemoryCache) Get(key string) (*Resource, bool) {
	if c == nil || c.recent == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.recent.MoveToFront(entry.element)
	return entry.resource, true
}

func (c *MemoryCache) Put(key string, resource *Resource) {
	if c == nil || c.recent == nil || resource == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if int64(len(resource.data)) > c.maxBytes {
		if old, ok := c.entries[key]; ok {
			delete(c.entries, key)
			c.recent.Remove(old.element)
			c.bytes -= int64(len(old.resource.data))
		}
		return
	}
	if old, ok := c.entries[key]; ok {
		delete(c.entries, key)
		c.bytes -= int64(len(old.resource.data))
		c.recent.Remove(old.element)
	}
	resourceBytes := int64(len(resource.data))
	for len(c.entries) >= c.maxEntries || resourceBytes > c.maxBytes-c.bytes {
		oldestElement := c.recent.Back()
		if oldestElement == nil {
			break
		}
		oldest := oldestElement.Value.(*cacheEntry)
		delete(c.entries, oldest.key)
		c.recent.Remove(oldestElement)
		c.bytes -= int64(len(oldest.resource.data))
	}
	entry := &cacheEntry{key: key, resource: resource}
	entry.element = c.recent.PushFront(entry)
	c.entries[key] = entry
	c.bytes += resourceBytes
}

// Options configures one independent resolver.
type Options struct {
	// BaseDir resolves relative local paths. New makes it absolute. When it is
	// empty, rooted local-file policies use their configured root and other
	// policies freeze the current working directory at construction time.
	BaseDir string
	// LocalFiles controls local-file access. Its zero value denies local files.
	// Use localfile.Rooted for untrusted input. localfile.Unrestricted is an
	// explicit opt-in intended only for trusted local applications.
	LocalFiles localfile.Policy
	HTTPClient *http.Client
	// NetworkPolicy defaults to public addresses only. Trusted callers can
	// explicitly allow private-network assets.
	NetworkPolicy netpolicy.Policy
	Cache         Cache
	// CacheNamespace is required when Cache is set. Reusing a namespace asserts
	// identical tenant, credentials, CookieJar, redirect policy, transport, and
	// fetch semantics for every Resolver sharing that cache. NetworkPolicy is
	// automatically included in the effective namespace.
	CacheNamespace string
	Limits         Limits
}

// Resolver is one cumulative-budget session (normally one output document). It
// owns no global state and is safe for concurrent use.
type Resolver struct {
	baseDir    string
	localFiles localfile.Policy
	client     *http.Client
	// ownsHTTPTransport is true when New created or policy-cloned the
	// transport. Injected transports deliberately allowed by a private-network
	// policy remain caller-owned and are not closed by this resolver.
	ownsHTTPTransport bool
	cache             Cache
	cachePrefix       string
	limits            Limits

	mu                     sync.Mutex
	assetCount             int
	cumulativeEncodedBytes int64
	cumulativeDecodedBytes int64
	resolvedSources        map[string]*Resource
	inflight               map[string]*resolveCall
	resolveSlots           chan struct{}
}

func New(options Options) (*Resolver, error) {
	if err := options.Limits.Validate(); err != nil {
		return nil, err
	}
	if options.Cache != nil && options.CacheNamespace == "" {
		return nil, errors.New("imageasset: CacheNamespace is required when Cache is set")
	}
	baseDir := options.BaseDir
	if baseDir == "" {
		if root, ok := options.LocalFiles.RootPath(); ok {
			baseDir = root
		} else {
			baseDir = "."
		}
	}
	absolute, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("imageasset: resolve base directory %q: %w", baseDir, err)
	}
	baseDir = filepath.Clean(absolute)
	// Match imgbundler's per-request safety net when the caller does not
	// provide a client. An injected client's fields are preserved, including a
	// shorter timeout and its redirect policy; loadHTTP also keeps the existing
	// one-minute request ceiling.
	defaultTransport := boundedDefaultTransport(http.DefaultTransport)
	client := &http.Client{Transport: defaultTransport, Timeout: time.Minute}
	ownsHTTPTransport := true
	if options.HTTPClient != nil {
		clone := *options.HTTPClient
		if clone.Transport == nil {
			clone.Transport = defaultTransport
		} else if options.NetworkPolicy.AllowPrivateNetworks {
			// The trusted policy preserves a caller-injected transport instead of
			// cloning it, so its connection pool remains caller-owned.
			ownsHTTPTransport = false
		}
		client = &clone
	}
	client, err = netpolicy.NewHTTPClient(client, options.NetworkPolicy)
	if err != nil {
		return nil, fmt.Errorf("imageasset: configure HTTP network policy: %w", err)
	}
	cachePrefix := ""
	if options.Cache != nil {
		networkNamespace := "public-only"
		if options.NetworkPolicy.AllowPrivateNetworks {
			networkNamespace = "private-network"
		}
		namespaceHash := sha256.Sum256([]byte(options.CacheNamespace + "\x00network-policy:" + networkNamespace))
		cachePrefix = fmt.Sprintf("namespace:%x:", namespaceHash)
	}
	return &Resolver{
		baseDir:           baseDir,
		localFiles:        options.LocalFiles,
		client:            client,
		ownsHTTPTransport: ownsHTTPTransport,
		cache:             options.Cache,
		cachePrefix:       cachePrefix,
		limits:            options.Limits,
		resolvedSources:   make(map[string]*Resource),
		inflight:          make(map[string]*resolveCall),
		resolveSlots:      make(chan struct{}, maxConcurrentResolutions),
	}, nil
}

// CloseIdleConnections releases idle connections held by the transport New
// created for this resolver session. A trusted caller-injected transport is
// borrowed and remains the caller's responsibility.
func (r *Resolver) CloseIdleConnections() {
	if r == nil || r.client == nil || !r.ownsHTTPTransport {
		return
	}
	r.client.CloseIdleConnections()
}

func boundedDefaultTransport(base http.RoundTripper) *http.Transport {
	if transport, ok := base.(*http.Transport); ok {
		clone := transport.Clone()
		clone.MaxResponseHeaderBytes = maxResponseHeaderBytes
		return clone
	}
	// http.DefaultTransport is exported and may legally be replaced. Retain the
	// standard library's safe defaults instead of asserting its concrete type.
	return &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           100,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		MaxResponseHeaderBytes: maxResponseHeaderBytes,
	}
}

func (r *Resolver) assetCountSnapshot() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.assetCount
}

func (r *Resolver) cumulativeEncodedBytesSnapshot() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cumulativeEncodedBytes
}

func (r *Resolver) cumulativeDecodedBytesSnapshot() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cumulativeDecodedBytes
}

// SourceError associates an error with the external source and resolution
// stage. Data URI payloads are never copied into Source or error text.
type SourceError struct {
	Source string
	Op     string
	Err    error
}

func (e *SourceError) Error() string {
	return fmt.Sprintf("imageasset: %s %q: %v", e.Op, e.Source, e.Err)
}

func (e *SourceError) Unwrap() error { return e.Err }

// LimitError reports the exact inclusive ceiling that was exceeded.
type LimitError struct {
	Name   string
	Actual int64
	Limit  int64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s %d exceeds limit %d", e.Name, e.Actual, e.Limit)
}

func sourceError(source, op string, err error) error {
	if err == nil {
		return nil
	}
	return &SourceError{Source: source, Op: op, Err: err}
}

func checkContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
