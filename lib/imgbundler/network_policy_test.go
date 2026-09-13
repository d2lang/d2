package imgbundler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/simplelog"
)

func TestBundleRemoteNetworkPolicy(t *testing.T) {
	var privateHits atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/private":
			privateHits.Add(1)
			response.Header().Set("Content-Type", "image/png")
			_, _ = response.Write(testPNGFile)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	ctx := log.With(context.Background(), testlog.New(t))
	logger := simplelog.FromLibLog(ctx)

	t.Run("blocks direct private target", func(t *testing.T) {
		_, err := BundleRemote(ctx, logger, remoteImageSVG(server.URL+"/private"), false)
		if err == nil {
			t.Fatal("expected direct private-network request to fail")
		}
		if privateHits.Load() != 0 {
			t.Fatalf("private endpoint received %d requests", privateHits.Load())
		}
	})

	t.Run("explicit opt-in allows trusted private target", func(t *testing.T) {
		before := privateHits.Load()
		output, err := BundleRemoteWithPolicy(ctx, logger, remoteImageSVG(server.URL+"/private"), false, netpolicy.Policy{AllowPrivateNetworks: true})
		if err != nil {
			t.Fatal(err)
		}
		if privateHits.Load() != before+1 || !strings.Contains(string(output), "data:image/png;base64,") {
			t.Fatalf("private hits = %d, output = %s", privateHits.Load(), output)
		}
	})

	t.Run("cache is separated by policy", func(t *testing.T) {
		before := privateHits.Load()
		_, err := BundleRemoteWithPolicy(ctx, logger, remoteImageSVG(server.URL+"/private"), true, netpolicy.Policy{AllowPrivateNetworks: true})
		if err != nil {
			t.Fatal(err)
		}
		_, err = BundleRemote(ctx, logger, remoteImageSVG(server.URL+"/private"), true)
		if err == nil {
			t.Fatal("public-only request reused a private-policy cache entry")
		}
		if privateHits.Load() != before+1 {
			t.Fatalf("private endpoint received %d new requests, want one trusted request", privateHits.Load()-before)
		}
	})
}

func remoteImageSVG(source string) []byte {
	return []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg"><image href="%s"/></svg>`, source))
}
