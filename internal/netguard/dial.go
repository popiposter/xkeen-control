// Package netguard contains the small, purpose-specific network guard shared
// by product-owned outbound metadata/subscription clients. It is not a
// general-purpose browser or proxy transport.
package netguard

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"
)

const (
	// MaxDNSAnswers keeps a malicious or unexpectedly broad DNS response from
	// turning one bounded request into unbounded connection work.
	MaxDNSAnswers         = 32
	defaultConnectTimeout = 5 * time.Second
)

type IPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type DefaultResolver struct{}

func (DefaultResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// Dialer resolves hostnames itself, rejects every non-public answer before
// connecting, and fails closed if any answer in the bounded response is
// unsafe. The same guard must be used for every product-owned public fetch.
type Dialer struct {
	Resolver       IPResolver
	ConnectTimeout time.Duration
	MaxDNSAnswers  int
}

func (d Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return nil, errors.New("invalid network destination")
	}
	if ip := net.ParseIP(host); ip != nil {
		if BlockedDestination(ip) {
			return nil, errors.New("network destination is not public")
		}
		return d.dial(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	if strings.Contains(host, "%") {
		return nil, errors.New("network destination is invalid")
	}
	resolver := d.Resolver
	if resolver == nil {
		resolver = DefaultResolver{}
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	maxAnswers := d.MaxDNSAnswers
	if maxAnswers <= 0 {
		maxAnswers = MaxDNSAnswers
	}
	if err != nil || len(addresses) == 0 || len(addresses) > maxAnswers {
		return nil, errors.New("network destination lookup failed")
	}
	for _, candidate := range addresses {
		if BlockedDestination(candidate.IP) {
			return nil, errors.New("network destination is not public")
		}
	}
	for _, candidate := range addresses {
		connection, err := d.dial(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if err == nil {
			return connection, nil
		}
	}
	return nil, errors.New("network destination connection failed")
}

func (d Dialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	timeout := d.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}
	return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, address)
}

var publicIPv6 = netip.MustParsePrefix("2000::/3")

// IANA IPv4/IPv6 special-purpose registries, intentionally treated as a
// denylist even where a special anycast entry is globally reachable. Product
// metadata/subscription endpoints must be ordinary public-routable targets.
var specialUsePrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("100:0:0:1::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}

func BlockedDestination(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	return !ok || !PublicRoutable(address)
}

func PublicRoutable(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	if address.Is6() && !publicIPv6.Contains(address) {
		return false
	}
	for _, prefix := range specialUsePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
