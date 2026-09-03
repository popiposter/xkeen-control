package components

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/netguard"
)

const (
	geodataArtifactHost         = "github.com"
	geodataArtifactUserAgent    = "xkeen-control-geodata-transaction/1"
	geodataArtifactTimeout      = 3 * time.Minute
	geodataArtifactMaxRedirects = 3
)

var (
	errGeodataArtifactHTTP     = errors.New("geodata artifact request failed")
	errGeodataArtifactStatus   = errors.New("geodata artifact response was rejected")
	errGeodataArtifactContent  = errors.New("geodata artifact content length was rejected")
	errGeodataArtifactRedirect = errors.New("geodata artifact redirect was rejected")
)

// GeodataArtifactClient downloads only catalog-bound GitHub release assets.
// It never accepts a URL, repository or filename from the caller.
type GeodataArtifactClient struct {
	http     *http.Client
	resolver netguard.IPResolver
}

func NewGeodataArtifactDownloader(resolver netguard.IPResolver, supplied *http.Client) *GeodataArtifactClient {
	client := supplied
	if client == nil {
		client = newGeodataArtifactHTTPClient(resolver)
	} else {
		copy := *client
		client = &copy
	}
	client.CheckRedirect = geodataArtifactRedirectPolicy
	return &GeodataArtifactClient{http: client, resolver: resolver}
}

func newGeodataArtifactHTTPClient(resolver netguard.IPResolver) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           netguard.Dialer{Resolver: resolver}.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       10 * time.Second,
		MaxIdleConns:          2,
	}
	return &http.Client{Transport: transport, Timeout: geodataArtifactTimeout}
}

func (c *GeodataArtifactClient) DownloadGeodata(ctx context.Context, identity GeodataReleaseIdentity, destination io.Writer) error {
	if c == nil || c.http == nil || destination == nil || !validCatalogGeodataIdentity(identity) {
		return ErrGeodataArtifactRejected
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestURL, err := geodataArtifactURL(identity)
	if err != nil {
		return ErrGeodataArtifactRejected
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil || request.URL.Scheme != "https" || request.URL.Host != geodataArtifactHost || !allowedHTTPSPort(request.URL.Port()) {
		return ErrGeodataArtifactRejected
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", geodataArtifactUserAgent)
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errGeodataArtifactHTTP
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errGeodataArtifactStatus
	}
	if response.ContentLength >= 0 && response.ContentLength != identity.SizeBytes {
		return errGeodataArtifactContent
	}
	count, err := io.CopyN(destination, response.Body, identity.SizeBytes)
	if err != nil || count != identity.SizeBytes {
		return errGeodataArtifactContent
	}
	var extra [1]byte
	read, readErr := response.Body.Read(extra[:])
	if read > 0 || readErr != io.EOF {
		return errGeodataArtifactContent
	}
	return nil
}

func validCatalogGeodataIdentity(identity GeodataReleaseIdentity) bool {
	for _, entry := range productGeodataCatalog {
		if identity.ID == entry.ID {
			return validGeodataIdentity(identity, entry)
		}
	}
	return false
}

func geodataArtifactURL(identity GeodataReleaseIdentity) (string, error) {
	if !validCatalogGeodataIdentity(identity) {
		return "", ErrGeodataArtifactRejected
	}
	return (&url.URL{
		Scheme: "https",
		Host:   geodataArtifactHost,
		Path:   "/" + identity.Repository + "/releases/download/" + identity.Tag + "/" + identity.AssetName,
	}).String(), nil
}

func geodataArtifactRedirectPolicy(request *http.Request, via []*http.Request) error {
	if request == nil || request.URL == nil || len(via) > geodataArtifactMaxRedirects {
		return errGeodataArtifactRedirect
	}
	if request.URL.Scheme != "https" || request.URL.User != nil || !allowedHTTPSPort(request.URL.Port()) || !isAllowedXrayArtifactHost(request.URL.Hostname()) {
		return errGeodataArtifactRedirect
	}
	if strings.EqualFold(request.URL.Hostname(), geodataArtifactHost) && (len(via) == 0 || via[0] == nil || via[0].URL == nil || request.URL.Path != via[0].URL.Path) {
		return errGeodataArtifactRedirect
	}
	return nil
}
