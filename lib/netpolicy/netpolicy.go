// Package netpolicy applies a network-boundary policy to HTTP clients used to
// fetch untrusted resources.
package netpolicy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// ErrBlockedAddress means a request resolved to a non-public address.
var ErrBlockedAddress = errors.New("network policy blocked a non-public address")

// Policy controls which network destinations an HTTP client may reach. The
// zero value permits only public IP addresses. AllowPrivateNetworks is intended
// for trusted diagrams in environments that deliberately serve assets from a
// private network.
type Policy struct {
	AllowPrivateNetworks bool
}

type policyContextKey struct{}

// WithPolicy records a policy in ctx for render pipelines that construct their
// asset resolver below a public API boundary.
func WithPolicy(ctx context.Context, policy Policy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, policyContextKey{}, policy)
}

// FromContext returns the policy stored in ctx, or the public-only zero value.
func FromContext(ctx context.Context) Policy {
	if ctx == nil {
		return Policy{}
	}
	policy, _ := ctx.Value(policyContextKey{}).(Policy)
	return policy
}

type ipResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// NewHTTPClient clones base and applies policy without mutating the caller's
// client or transport. Public-only clients resolve each hostname themselves,
// reject the entire resolution if any address is non-public, and pass only a
// validated numeric address to a policy-owned transport dialer. Caller proxy,
// dial, and alternate-protocol hooks are not retained. Every new connection
// resolves and validates its destination, so redirects and DNS rebinding cannot
// bypass the policy.
//
// A public-only client requires a standard *http.Transport. Custom protocol
// RoundTrippers can opt into trusted private-network behavior explicitly.
func NewHTTPClient(base *http.Client, policy Policy) (*http.Client, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return newHTTPClient(base, policy, net.DefaultResolver, dialer.DialContext)
}

// newHTTPClient keeps resolver and dial injection unexported for deterministic
// tests. NewHTTPClient always supplies a package-owned net.Dialer.
func newHTTPClient(base *http.Client, policy Policy, resolver ipResolver, policyDial dialContextFunc) (*http.Client, error) {
	if base == nil {
		base = &http.Client{}
	}
	client := *base
	if policy.AllowPrivateNetworks {
		return &client, nil
	}

	var transport *http.Transport
	switch baseTransport := client.Transport.(type) {
	case nil:
		transport = defaultTransport()
	case *http.Transport:
		// Clone copies exported configuration but deliberately excludes live
		// connection pools and handlers installed with RegisterProtocol.
		transport = baseTransport.Clone()
	default:
		return nil, fmt.Errorf("public-only network policy requires *http.Transport, got %T", client.Transport)
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
	client.Transport = transport
	return &client, nil
}

func defaultTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

func validatedDialContext(resolver ipResolver, dial dialContextFunc) dialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("network policy could not parse destination: %w", err)
		}
		addresses, err := resolvePublicAddresses(ctx, resolver, network, host)
		if err != nil {
			return nil, err
		}

		var dialErrors []error
		for _, resolved := range addresses {
			conn, err := dial(ctx, network, net.JoinHostPort(resolved.String(), port))
			if err == nil {
				return conn, nil
			}
			dialErrors = append(dialErrors, err)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errors.Join(dialErrors...)
	}
}

func resolvePublicAddresses(ctx context.Context, resolver ipResolver, network, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(host, ".")
	if isLocalHostname(host) {
		return nil, ErrBlockedAddress
	}

	var addresses []netip.Addr
	if literal, err := netip.ParseAddr(host); err == nil {
		addresses = []netip.Addr{literal}
	} else {
		lookupNetwork := "ip"
		if strings.HasSuffix(network, "4") {
			lookupNetwork = "ip4"
		} else if strings.HasSuffix(network, "6") {
			lookupNetwork = "ip6"
		}
		resolved, err := resolver.LookupNetIP(ctx, lookupNetwork, host)
		if err != nil {
			return nil, err
		}
		addresses = resolved
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("network policy: destination resolved to no addresses")
	}

	// Validate every answer before dialing any one of them. A hostname that
	// mixes public and private answers is rejected instead of falling through to
	// a private answer after a public connection failure.
	unique := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !isPublicAddress(address) {
			return nil, ErrBlockedAddress
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		unique = append(unique, address)
	}
	return unique, nil
}

func isLocalHostname(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "metadata.google.internal" || strings.HasSuffix(host, ".metadata.google.internal") ||
		host == "metadata.goog" || strings.HasSuffix(host, ".metadata.goog") ||
		host == "metadata.azure.internal" || strings.HasSuffix(host, ".metadata.azure.internal") ||
		host == "instance-data.ec2.internal" || strings.HasSuffix(host, ".instance-data.ec2.internal")
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("168.63.129.16/32"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

func isPublicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" || !address.IsGlobalUnicast() ||
		address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
