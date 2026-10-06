package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"golang.org/x/net/proxy"
)

const telegramHost = "api.telegram.org"
const telegramAddress = telegramHost + ":443"
const requestTimeout = 5 * time.Second
const maxResponseBytes = 16 << 10

type telegram struct{ client *http.Client }

type lookupIP func(context.Context, string) ([]net.IPAddr, error)
type dialIP func(context.Context, string, string) (net.Conn, error)

func newTelegram() *telegram {
	return newTelegramWithTimeout(requestTimeout)
}
func newTelegramWithTimeout(timeout time.Duration) *telegram {
	t, _ := newTelegramWithSOCKS(timeout, "")
	return t
}

// Only an explicit numeric loopback SOCKS endpoint is accepted. Environment
// proxies and direct fallback are intentionally excluded from VPN transport.
func newTelegramWithSOCKS(timeout time.Duration, address string) (*telegram, error) {
	dial, err := telegramTransportDial(address, net.DefaultResolver.LookupIPAddr)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy:               nil, // Deliberately ignores HTTP(S)_PROXY environment variables.
		DialContext:         dial,
		TLSClientConfig:     &tls.Config{ServerName: telegramHost, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: requestTimeout, ResponseHeaderTimeout: timeout,
		DisableKeepAlives: true, MaxResponseHeaderBytes: maxResponseBytes,
	}
	return &telegram{client: &http.Client{Transport: transport, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return Error("provider-rejected") },
	}}, nil
}

func telegramTransportDial(address string, lookup lookupIP) (dialIP, error) {
	dial := dialIP((&net.Dialer{Timeout: requestTimeout}).DialContext)
	if address != "" {
		endpoint, err := netip.ParseAddrPort(address)
		if err != nil || !endpoint.Addr().IsLoopback() || endpoint.Addr().Zone() != "" || endpoint.Port() == 0 {
			return nil, Error("unsafe-destination")
		}
		socks, err := proxy.SOCKS5("tcp", address, nil, &net.Dialer{Timeout: requestTimeout})
		if err != nil {
			return nil, Error("unsafe-destination")
		}
		contextDialer, ok := socks.(proxy.ContextDialer)
		if !ok {
			return nil, Error("unsafe-destination")
		}
		dial = contextDialer.DialContext
	}
	return telegramDial(lookup, dial), nil
}

func telegramDial(lookup lookupIP, dial dialIP) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != telegramAddress || network != "tcp" {
			return nil, Error("unsafe-destination")
		}
		ips, err := lookup(ctx, telegramHost)
		if err != nil {
			if ctx.Err() != nil {
				return nil, Error("timeout")
			}
			return nil, Error("dns-unavailable")
		}
		if len(ips) == 0 || len(ips) > 32 {
			return nil, Error("dns-unavailable")
		}
		// Reject the entire answer if any entry is unsafe. Connect only to a checked
		// numeric IP, preventing a second DNS lookup/rebinding between check and dial.
		for _, ip := range ips {
			if ip.Zone != "" || !publicDestination(ip.IP) {
				return nil, Error("unsafe-destination")
			}
		}
		for _, ip := range ips {
			conn, err := dial(ctx, "tcp", net.JoinHostPort(ip.IP.String(), "443"))
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, Error("timeout")
			}
		}
		return nil, Error("transport-failed")
	}
}

var specialPrefixes = func() []netip.Prefix {
	// Source-owned exclusions include globally reachable special-purpose
	// allocations too: https://www.iana.org/assignments/iana-ipv4-special-registry
	// and https://www.iana.org/assignments/iana-ipv6-special-registry .
	values := []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.31.196.0/24", "192.52.193.0/24", "192.88.99.0/24", "192.168.0.0/16", "192.175.48.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "2620:4f:8000::/48", "3fff::/20"}
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}()

func publicDestination(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() {
		return false
	}
	if address.Is6() && !netip.MustParsePrefix("2000::/3").Contains(address) {
		return false
	}
	for _, prefix := range specialPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func (t *telegram) send(parent context.Context, value authority, message string) error {
	if !validCredentials(value.BotToken, value.ChatID) || len(message) == 0 || len(message) > 512 {
		return Error("provider-rejected")
	}
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	data, _ := json.Marshal(struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}{value.ChatID, message})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+telegramAddress+"/bot"+value.BotToken+"/sendMessage", bytes.NewReader(data))
	if err != nil {
		return Error("transport-failed")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := t.client.Do(request)
	if err != nil {
		// Do not return url.Error: its URL contains the secret bot token.
		var safe Error
		if errors.As(err, &safe) {
			return safe
		}
		var timeout net.Error
		if ctx.Err() != nil || (errors.As(err, &timeout) && timeout.Timeout()) {
			return Error("timeout")
		}
		return Error("transport-failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return Error("timeout")
		}
		return Error("transport-failed")
	}
	if len(body) > maxResponseBytes || response.StatusCode != http.StatusOK {
		return Error("provider-rejected")
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(body, &result) != nil || !result.OK {
		return Error("provider-rejected")
	}
	return nil
}
