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

	"github.com/popiposter/xkeen-control/internal/netguard"
)

const (
	MaxSubscriptionURL  = 2048
	MaxSubscriptionBody = 1 << 20
)

type IPResolver = netguard.IPResolver
type defaultIPResolver = netguard.DefaultResolver

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
		DialContext:           netguard.Dialer{Resolver: resolver}.DialContext,
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
	return netguard.Dialer{Resolver: d.Resolver}.DialContext(ctx, network, address)
}

func blockedDestination(ip net.IP) bool {
	return netguard.BlockedDestination(ip)
}

// publicRoutable remains a package-local compatibility helper for the
// existing subscription tests; the implementation is shared with all other
// product-owned network clients through netguard.
func publicRoutable(address netip.Addr) bool {
	return netguard.PublicRoutable(address)
}
