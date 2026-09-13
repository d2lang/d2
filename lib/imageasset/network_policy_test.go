package imageasset

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/d2lang/d2/lib/netpolicy"
)

func TestHTTPNetworkPolicy(t *testing.T) {
	image := encodePNG(t, 2, 3)
	var privateHits atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/private":
			privateHits.Add(1)
			response.Header().Set("Content-Type", "image/png")
			_, _ = response.Write(image)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	newResolver := func(t *testing.T) *Resolver {
		t.Helper()
		resolver, err := New(Options{HTTPClient: server.Client(), Limits: generousLimits()})
		if err != nil {
			t.Fatal(err)
		}
		return resolver
	}

	t.Run("blocks direct private target", func(t *testing.T) {
		_, err := newResolver(t).Resolve(context.Background(), server.URL+"/private")
		if !errors.Is(err, netpolicy.ErrBlockedAddress) {
			t.Fatalf("Resolve error = %v, want ErrBlockedAddress", err)
		}
		if privateHits.Load() != 0 {
			t.Fatalf("private endpoint received %d requests", privateHits.Load())
		}
	})

	t.Run("explicit opt-in allows trusted private target", func(t *testing.T) {
		resolver, err := New(Options{
			HTTPClient:    server.Client(),
			NetworkPolicy: netpolicy.Policy{AllowPrivateNetworks: true},
			Limits:        generousLimits(),
		})
		if err != nil {
			t.Fatal(err)
		}
		before := privateHits.Load()
		resource, err := resolver.Resolve(context.Background(), server.URL+"/private")
		if err != nil {
			t.Fatal(err)
		}
		assertResource(t, resource, KindRaster, "image/png", 2, 3)
		if privateHits.Load() != before+1 {
			t.Fatalf("private endpoint received %d requests", privateHits.Load())
		}
	})

	t.Run("cache is separated by policy", func(t *testing.T) {
		cache, err := NewMemoryCache(4, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		trusted, err := New(Options{
			HTTPClient:     server.Client(),
			NetworkPolicy:  netpolicy.Policy{AllowPrivateNetworks: true},
			Cache:          cache,
			CacheNamespace: "shared-network-test",
			Limits:         generousLimits(),
		})
		if err != nil {
			t.Fatal(err)
		}
		before := privateHits.Load()
		if _, err := trusted.Resolve(context.Background(), server.URL+"/private"); err != nil {
			t.Fatal(err)
		}
		publicOnly, err := New(Options{
			HTTPClient:     server.Client(),
			Cache:          cache,
			CacheNamespace: "shared-network-test",
			Limits:         generousLimits(),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = publicOnly.Resolve(context.Background(), server.URL+"/private")
		if !errors.Is(err, netpolicy.ErrBlockedAddress) {
			t.Fatalf("Resolve error = %v, want ErrBlockedAddress", err)
		}
		if privateHits.Load() != before+1 {
			t.Fatalf("private endpoint received %d new requests, want one trusted request", privateHits.Load()-before)
		}
	})
}
