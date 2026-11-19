//go:build !tinygo || !js || !wasm

package netpolicy

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

func publicOnlyTransport(base http.RoundTripper, resolver ipResolver, policyDial dialContextFunc) (http.RoundTripper, error) {
	var transport *http.Transport
	switch baseTransport := base.(type) {
	case nil:
		transport = CloneDefaultTransport(http.DefaultTransport)
	case *http.Transport:
		// Clone copies exported configuration but deliberately excludes live
		// connection pools and handlers installed with RegisterProtocol.
		transport = baseTransport.Clone()
	default:
		return nil, fmt.Errorf("public-only network policy requires *http.Transport, got %T", base)
	}

	// A proxy resolves or connects to the target outside this transport's dial
	// boundary, so it cannot uphold the public-only policy. Trusted proxy users
	// can explicitly opt into private-network behavior.
	transport.Proxy = nil
	transport.OnProxyConnectResponse = nil
	transport.GetProxyConnectHeader = nil
	transport.ProxyConnectHeader = nil
	if resolver == nil || policyDial == nil {
		return nil, errors.New("public-only network policy requires its resolver and dialer")
	}
	// Never delegate a validated numeric destination to caller-provided dial
	// hooks: a hook can ignore that destination and connect to a private address.
	transport.Dial = nil
	transport.DialContext = validatedDialContext(resolver, policyDial)
	// Either hook bypasses DialContext for HTTPS.
	transport.DialTLS = nil
	transport.DialTLSContext = nil
	// A caller-provided TLSNextProto callback is an arbitrary RoundTripper and
	// can create connections outside DialContext. Clearing it still permits the
	// standard library to configure its built-in HTTP/2 implementation.
	transport.TLSNextProto = nil
	return transport, nil
}

// CloneDefaultTransport returns independent transport settings. If the process
// default was replaced with a custom RoundTripper, use standard defaults.
func CloneDefaultTransport(base http.RoundTripper) *http.Transport {
	if transport, ok := base.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
