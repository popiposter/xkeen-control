package nodes

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	MaxSubscriptionURL  = 2048
	MaxSubscriptionBody = 1 << 20
)

type IPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type defaultIPResolver struct{}

func (defaultIPResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

type SubscriptionFetcher interface {
	Fetch(context.Context, string) ([]byte, error)
}

type HTTPSubscriptionFetcher struct {
	Resolver IPResolver
	Timeout  time.Duration
}

func (f HTTPSubscriptionFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	if err := validateSubscriptionURL(rawURL); err != nil {
		return nil, err
	}
	resolver := f.Resolver
	if resolver == nil {
		resolver = defaultIPResolver{}
	}
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           safeDialer{Resolver: resolver}.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       5 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(next *http.Request, _ []*http.Request) error {
			if err := validateSubscriptionURL(next.URL.String()); err != nil {
				return errors.New("subscription redirect rejected")
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("invalid subscription request")
	}
	request.Header.Set("Accept", "text/plain, text/uri-list;q=0.9, */*;q=0.1")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("subscription fetch failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, errors.New("subscription fetch failed")
	}
	if response.ContentLength > MaxSubscriptionBody {
		return nil, errors.New("subscription response exceeds bounded size")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxSubscriptionBody+1))
	if err != nil || len(body) > MaxSubscriptionBody {
		return nil, errors.New("subscription response exceeds bounded size")
	}
	return body, nil
}

func validateSubscriptionURL(rawURL string) error {
	if len(rawURL) == 0 || len(rawURL) > MaxSubscriptionURL {
		return errors.New("subscription URL exceeds bounded size")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || parsed.User != nil {
		return errors.New("subscription URL must use HTTPS")
	}
	if parsed.Port() != "" {
		port, err := net.LookupPort("tcp", parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("invalid subscription URL port")
		}
	}
	return nil
}

type safeDialer struct {
	Resolver IPResolver
}

func (d safeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return nil, errors.New("invalid subscription destination")
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedDestination(ip) {
			return nil, errors.New("subscription destination is not public")
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	if d.Resolver == nil {
		return nil, errors.New("subscription resolver unavailable")
	}
	addresses, err := d.Resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 || len(addresses) > 32 {
		return nil, errors.New("subscription destination lookup failed")
	}
	for _, address := range addresses {
		if blockedDestination(address.IP) {
			return nil, errors.New("subscription destination is not public")
		}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	for _, address := range addresses {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if err == nil {
			return connection, nil
		}
	}
	return nil, errors.New("subscription destination connection failed")
}

var publicIPv6 = netip.MustParsePrefix("2000::/3")

// IANA IPv4/IPv6 special-purpose registries, intentionally treated as a
// denylist even where a special anycast entry is globally reachable. A
// subscription endpoint must be an ordinary public-routable destination.
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

func blockedDestination(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	return !ok || !publicRoutable(address)
}

func publicRoutable(address netip.Addr) bool {
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
