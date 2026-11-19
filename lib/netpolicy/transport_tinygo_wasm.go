//go:build tinygo && js && wasm

package netpolicy

import (
	"errors"
	"net/http"
)

// The browser owns DNS and opens connections. A public-only policy cannot
// validate the peer IP, so reject network requests while allowing data URIs.
type blockedTransport struct{}

func (blockedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("public-only network access is unavailable in TinyGo browser workers")
}

func publicOnlyTransport(http.RoundTripper, ipResolver, dialContextFunc) (http.RoundTripper, error) {
	return blockedTransport{}, nil
}

// CloneDefaultTransport returns independent settings for the browser transport.
func CloneDefaultTransport(base http.RoundTripper) *http.Transport {
	if transport, ok := base.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}
