package nodes

import (
	"context"
	"net"
	"strings"
	"testing"
)

type fixedResolver struct{ addresses []net.IPAddr }

func (r fixedResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return append([]net.IPAddr(nil), r.addresses...), nil
}

func TestSubscriptionURLAndDestinationSSRFGuards(t *testing.T) {
	for _, rawURL := range []string{
		"http://subscription.example/token",
		"file:///etc/passwd",
		"https://user:password@subscription.example/token",
		"https://subscription.example:bad/token",
	} {
		if err := validateSubscriptionURL(rawURL); err == nil {
			t.Fatalf("unsafe subscription URL accepted: %q", rawURL)
		}
	}
	for _, ip := range []string{
		"0.0.0.1", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.169.254", "172.16.0.1",
		"192.0.0.1", "192.0.0.9", "192.0.2.1", "192.31.196.1", "192.52.193.1", "192.88.99.2",
		"192.168.0.1", "192.175.48.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "64:ff9b::1", "64:ff9b:1::1", "100::1", "100:0:0:1::1",
		"2001::1", "2001:2::1", "2001:db8::1", "2002::1", "3fff::1", "5f00::1", "fc00::1", "fe80::1", "ff02::1",
	} {
		if !blockedDestination(net.ParseIP(ip)) {
			t.Fatalf("reserved destination accepted: %s", ip)
		}
	}
	for _, ip := range []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "2001:4860:4860::8888", "2606:4700:4700::1111"} {
		if blockedDestination(net.ParseIP(ip)) {
			t.Fatalf("public destination rejected: %s", ip)
		}
	}
	if _, err := (safeDialer{Resolver: fixedResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}}}).DialContext(context.Background(), "tcp", "subscription.example:443"); err == nil {
		t.Fatal("private DNS answer was not rejected")
	}
	if _, err := (safeDialer{Resolver: fixedResolver{addresses: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}, {IP: net.ParseIP("192.168.1.1")}}}}).DialContext(context.Background(), "tcp", "subscription.example:443"); err == nil {
		t.Fatal("mixed public/private DNS answer was not rejected fail-closed")
	}
}

func TestSubscriptionFetcherNeverUsesAmbientProxyAndReturnsSanitizedErrors(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "https://proxy.example.invalid:8443")
	_, err := (HTTPSubscriptionFetcher{Resolver: fixedResolver{addresses: []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}}}).Fetch(context.Background(), "https://subscription.example/secret-token")
	if err == nil || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "subscription.example") {
		t.Fatalf("fetch error was not sanitized: %v", err)
	}
}
