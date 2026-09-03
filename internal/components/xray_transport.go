package components

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/netguard"
)

const (
	xrayArtifactHost         = "github.com"
	xrayArtifactRepository   = "XTLS/Xray-core"
	xrayArtifactUserAgent    = "xkeen-control-xray-transaction/1"
	xrayArtifactTimeout      = 2 * time.Minute
	xrayArtifactMaxRedirects = 3
)

var (
	errXrayArtifactHTTP     = errors.New("xray artifact request failed")
	errXrayArtifactStatus   = errors.New("xray artifact response was rejected")
	errXrayArtifactContent  = errors.New("xray artifact content length was rejected")
	errXrayArtifactRedirect = errors.New("xray artifact redirect was rejected")
)

// XrayArtifactClient downloads only the fixed official arm64 Xray asset. It
// deliberately has no URL-taking method: the release identity is resolved
// by the server-owned resolver and the path is assembled here.
type XrayArtifactClient struct {
	http     *http.Client
	resolver netguard.IPResolver
}

// NewXrayArtifactDownloader constructs the purpose-specific artifact
// transport. A supplied HTTP client is a synthetic-fixture seam; production
// callers pass nil and receive the netguard-protected transport below.
func NewXrayArtifactDownloader(resolver netguard.IPResolver, supplied *http.Client) *XrayArtifactClient {
	client := supplied
	if client == nil {
		client = newXrayArtifactHTTPClient(resolver)
	} else {
		copy := *client
		client = &copy
	}
	client.CheckRedirect = xrayArtifactRedirectPolicy
	return &XrayArtifactClient{http: client, resolver: resolver}
}

func newXrayArtifactHTTPClient(resolver netguard.IPResolver) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           netguard.Dialer{Resolver: resolver}.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       10 * time.Second,
		MaxIdleConns:          2,
	}
	return &http.Client{Transport: transport, Timeout: xrayArtifactTimeout}
}

func (c *XrayArtifactClient) DownloadXray(ctx context.Context, identity XrayReleaseIdentity, destination io.Writer) error {
	if c == nil || c.http == nil || destination == nil || !validXrayIdentity(identity) {
		return ErrXrayArtifactRejected
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestURL, err := xrayArtifactURL(identity)
	if err != nil {
		return ErrXrayArtifactRejected
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil || request.URL.Scheme != "https" || request.URL.Host != xrayArtifactHost {
		return ErrXrayArtifactRejected
	}
	if !allowedHTTPSPort(request.URL.Port()) || !isAllowedXrayArtifactHost(request.URL.Hostname()) {
		return ErrXrayArtifactRejected
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", xrayArtifactUserAgent)
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errXrayArtifactHTTP
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errXrayArtifactStatus
	}
	if response.ContentLength >= 0 && response.ContentLength != identity.SizeBytes {
		return errXrayArtifactContent
	}
	count, err := io.CopyN(destination, response.Body, identity.SizeBytes)
	if err != nil || count != identity.SizeBytes {
		return errXrayArtifactContent
	}
	var extra [1]byte
	read, readErr := response.Body.Read(extra[:])
	if read > 0 || readErr != io.EOF {
		return errXrayArtifactContent
	}
	return nil
}

func xrayArtifactURL(identity XrayReleaseIdentity) (string, error) {
	if !validXrayIdentity(identity) {
		return "", ErrXrayArtifactRejected
	}
	return (&url.URL{
		Scheme: "https",
		Host:   xrayArtifactHost,
		Path:   "/" + xrayArtifactRepository + "/releases/download/" + identity.Tag + "/" + identity.AssetName,
	}).String(), nil
}

func xrayArtifactRedirectPolicy(request *http.Request, via []*http.Request) error {
	if request == nil || len(via) >= xrayArtifactMaxRedirects || request.URL == nil {
		return errXrayArtifactRedirect
	}
	if request.URL.Scheme != "https" || request.URL.User != nil || !allowedHTTPSPort(request.URL.Port()) || !isAllowedXrayArtifactHost(request.URL.Hostname()) {
		return errXrayArtifactRedirect
	}
	if strings.EqualFold(request.URL.Hostname(), xrayArtifactHost) && (len(via) == 0 || via[0] == nil || via[0].URL == nil || request.URL.Path != via[0].URL.Path) {
		return errXrayArtifactRedirect
	}
	return nil
}

func allowedHTTPSPort(port string) bool {
	return port == "" || port == strconv.Itoa(443)
}

func isAllowedXrayArtifactHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	default:
		return false
	}
}
