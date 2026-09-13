package netpolicy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func TestNonPublicAddressRanges(t *testing.T) {
	t.Parallel()
	for _, address := range []string{
		"0.0.0.1",
		"10.0.0.1",
		"100.100.100.200",
		"127.0.0.1",
		"168.63.129.16",
		"169.254.169.254",
		"172.16.0.1",
		"192.168.0.1",
		"198.18.0.1",
		"224.0.0.1",
		"255.255.255.255",
		"::",
		"::1",
		"::ffff:127.0.0.1",
		"64:ff9b::7f00:1",
		"2001::1",
		"2002:7f00:1::",
		"fc00::1",
		"fd00:ec2::254",
		"fe80::1",
		"ff02::1",
		"fec0::1",
	} {
		t.Run(address, func(t *testing.T) {
			if isPublicAddress(netip.MustParseAddr(address).Unmap()) {
				t.Fatalf("isPublicAddress(%s) = true", address)
			}
		})
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		t.Run("public_"+address, func(t *testing.T) {
			if !isPublicAddress(netip.MustParseAddr(address)) {
				t.Fatalf("isPublicAddress(%s) = false", address)
			}
		})
	}
}

func TestValidatedDialUsesOnlyResolvedNumericAddress(t *testing.T) {
	t.Parallel()
	lookupCalls := 0
	resolver := resolverFunc(func(_ context.Context, network, host string) ([]netip.Addr, error) {
		lookupCalls++
		if network != "ip" || host != "assets.example" {
			t.Fatalf("LookupNetIP(%q, %q)", network, host)
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	wantDialError := errors.New("dial stopped by test")
	var dialedAddress string
	base := &http.Client{Transport: &http.Transport{
		DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" {
				t.Fatalf("network = %q", network)
			}
			dialedAddress = address
			return nil, wantDialError
		},
	}}
	client, err := newHTTPClient(base, Policy{}, resolver, base.Transport.(*http.Transport).DialContext)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "assets.example:443")
	if !errors.Is(err, wantDialError) {
		t.Fatalf("DialContext error = %v", err)
	}
	if lookupCalls != 1 {
		t.Fatalf("lookup calls = %d, want 1", lookupCalls)
	}
	if dialedAddress != "93.184.216.34:443" {
		t.Fatalf("dialed address = %q, want validated numeric address", dialedAddress)
	}
}

func TestValidatedDialRejectsMixedResolutionBeforeDial(t *testing.T) {
	t.Parallel()
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("93.184.216.34"),
			netip.MustParseAddr("127.0.0.1"),
		}, nil
	})
	dialed := false
	base := &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("unexpected dial")
		},
	}}
	client, err := newHTTPClient(base, Policy{}, resolver, base.Transport.(*http.Transport).DialContext)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "assets.example:80")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("DialContext error = %v, want ErrBlockedAddress", err)
	}
	if dialed {
		t.Fatal("transport dialer was called for a mixed public/private resolution")
	}
}

func TestValidatedDialRechecksDNSForEachConnection(t *testing.T) {
	t.Parallel()
	lookupCalls := 0
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		lookupCalls++
		if lookupCalls == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	wantDialError := errors.New("dial stopped by test")
	dialCalls := 0
	policyDial := func(context.Context, string, string) (net.Conn, error) {
		dialCalls++
		return nil, wantDialError
	}
	client, err := newHTTPClient(nil, Policy{}, resolver, policyDial)
	if err != nil {
		t.Fatal(err)
	}
	dial := client.Transport.(*http.Transport).DialContext
	_, err = dial(context.Background(), "tcp", "assets.example:80")
	if !errors.Is(err, wantDialError) {
		t.Fatalf("first DialContext error = %v, want test dial error", err)
	}
	_, err = dial(context.Background(), "tcp", "assets.example:80")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("second DialContext error = %v, want ErrBlockedAddress", err)
	}
	if lookupCalls != 2 || dialCalls != 1 {
		t.Fatalf("lookups/dials = %d/%d, want 2/1", lookupCalls, dialCalls)
	}
}

func TestPublicOnlyClientChecksEveryRedirect(t *testing.T) {
	t.Parallel()
	var firstHits atomic.Int32
	var secondHits atomic.Int32
	var privateHits atomic.Int32

	var port string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/first":
			firstHits.Add(1)
			http.Redirect(response, request, "http://second.example:"+port+"/second", http.StatusFound)
		case "/second":
			secondHits.Add(1)
			http.Redirect(response, request, "http://127.0.0.1:"+port+"/private", http.StatusFound)
		case "/private":
			privateHits.Add(1)
			_, _ = io.WriteString(response, "private")
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	var err error
	_, port, err = net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	resolver := resolverFunc(func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
		switch host {
		case "first.example":
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		case "second.example":
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		case "127.0.0.1":
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		default:
			return nil, errors.New("unexpected test hostname")
		}
	})
	dialer := &net.Dialer{}
	policyDial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	}
	client, err := newHTTPClient(nil, Policy{}, resolver, policyDial)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get("http://first.example:" + port + "/first")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("Get error = %v, want ErrBlockedAddress", err)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 {
		t.Fatalf("redirect hits = %d/%d, want 1/1", firstHits.Load(), secondHits.Load())
	}
	if privateHits.Load() != 0 {
		t.Fatalf("private redirect endpoint received %d requests", privateHits.Load())
	}
}

func TestNewHTTPClientIgnoresCallerDialHooks(t *testing.T) {
	t.Parallel()
	callerDialed := false
	base := &http.Client{Transport: &http.Transport{
		Dial: func(string, string) (net.Conn, error) {
			callerDialed = true
			return nil, errors.New("caller Dial used")
		},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			callerDialed = true
			return nil, errors.New("caller DialContext used")
		},
		DialTLS: func(string, string) (net.Conn, error) {
			callerDialed = true
			return nil, errors.New("caller DialTLS used")
		},
		DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
			callerDialed = true
			return nil, errors.New("caller DialTLSContext used")
		},
	}}
	client, err := NewHTTPClient(base, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(*http.Transport)
	if transport.Dial != nil || transport.DialTLS != nil || transport.DialTLSContext != nil {
		t.Fatal("public-only transport retained a caller dial hook")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = transport.DialContext(ctx, "tcp", "8.8.8.8:443")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DialContext error = %v, want context.Canceled from policy dialer", err)
	}
	if callerDialed {
		t.Fatal("public-only transport invoked a caller dial hook")
	}
}

func TestPublicOnlyTransportClearsBypassHooks(t *testing.T) {
	t.Parallel()
	proxyCalled := false
	tlsNextProtoCalled := false
	baseTransport := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) {
			proxyCalled = true
			return nil, errors.New("caller proxy used")
		},
		OnProxyConnectResponse: func(context.Context, *url.URL, *http.Request, *http.Response) error {
			proxyCalled = true
			return errors.New("caller proxy response hook used")
		},
		GetProxyConnectHeader: func(context.Context, *url.URL, string) (http.Header, error) {
			proxyCalled = true
			return nil, errors.New("caller proxy header hook used")
		},
		ProxyConnectHeader: http.Header{"X-Test": {"unsafe"}},
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{
			"bypass": func(string, *tls.Conn) http.RoundTripper {
				tlsNextProtoCalled = true
				return roundTripperFunc(func(*http.Request) (*http.Response, error) {
					return nil, errors.New("caller TLSNextProto used")
				})
			},
		},
	}
	baseTransport.RegisterProtocol("http", roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("registered bypass")),
		}, nil
	}))

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "normal transport")
	}))
	defer server.Close()
	localAddress := server.Listener.Addr().String()
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	dialer := &net.Dialer{}
	policyDial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, localAddress)
	}
	client, err := newHTTPClient(&http.Client{Transport: baseTransport}, Policy{}, resolver, policyDial)
	if err != nil {
		t.Fatal(err)
	}
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.OnProxyConnectResponse != nil || transport.GetProxyConnectHeader != nil || transport.ProxyConnectHeader != nil {
		t.Fatal("public-only transport retained proxy hooks")
	}
	if transport.TLSNextProto != nil {
		t.Fatal("public-only transport retained TLSNextProto callbacks")
	}
	response, err := client.Get("http://assets.example/image")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "normal transport" {
		t.Fatalf("response body = %q, want normal transport", body)
	}
	if proxyCalled || tlsNextProtoCalled {
		t.Fatalf("caller hook invoked: proxy=%v TLSNextProto=%v", proxyCalled, tlsNextProtoCalled)
	}
}

func TestPrivateNetworkOptInPreservesCustomRoundTripper(t *testing.T) {
	t.Parallel()
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("stopped by test")
	})
	base := &http.Client{Transport: transport}
	client, err := NewHTTPClient(base, Policy{AllowPrivateNetworks: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.Transport.(roundTripperFunc); !ok {
		t.Fatal("trusted opt-in replaced the caller's custom transport")
	}
	if _, err := NewHTTPClient(base, Policy{}); err == nil {
		t.Fatal("public-only policy accepted a custom protocol RoundTripper")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
