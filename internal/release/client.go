package release

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	RepositoryOwner  = "popiposter"
	RepositoryName   = "xkeen-control"
	maxManifestBody  = 128 << 10
	maxSignatureBody = 8 << 10
	maxScriptBody    = 1 << 20
	maxBinaryBody    = 64 << 20
)

type Client struct {
	baseURL   string
	publicKey ed25519.PublicKey
	http      *http.Client
	testHost  string
}

func NewClient() *Client {
	publicKey := []byte(nil)
	if decoded, err := hex.DecodeString(StablePublicKeyHex); err == nil && len(decoded) == ed25519.PublicKeySize {
		publicKey = decoded
	}
	return &Client{baseURL: "https://github.com/popiposter/xkeen-control", publicKey: ed25519.PublicKey(publicKey), http: newHTTPClient("")}
}

// NewClientForTest is only for synthetic HTTP/signature fixtures. It is not
// used by production constructors and accepts no user-controlled API value.
func NewClientForTest(baseURL string, publicKey ed25519.PublicKey) *Client {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return &Client{baseURL: "", publicKey: publicKey}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), publicKey: append(ed25519.PublicKey(nil), publicKey...), testHost: parsed.Host, http: newHTTPClient(parsed.Host)}
}

func (c *Client) FetchCandidate(ctx context.Context, channel, version string) (Candidate, error) {
	if channel != "stable" && channel != "beta" {
		return Candidate{}, errors.New("release channel is invalid")
	}
	if version != "" && !validSemver(strings.TrimPrefix(version, "v")) {
		return Candidate{}, errors.New("release version is invalid")
	}
	manifestBytes, err := c.download(ctx, c.assetURL(channel, version, "release-manifest.json"), maxManifestBody)
	if err != nil {
		return Candidate{}, err
	}
	manifest, err := ParseManifest(manifestBytes)
	if err != nil || manifest.Channel != channel || (version != "" && strings.TrimPrefix(manifest.Version, "v") != strings.TrimPrefix(version, "v")) {
		return Candidate{}, errors.New("release manifest is not the requested release")
	}
	signature, err := c.download(ctx, c.assetURL(channel, version, "release-manifest.sig"), maxSignatureBody)
	if err != nil {
		return Candidate{}, err
	}
	if err := Verify(manifestBytes, signature, c.publicKey); err != nil {
		return Candidate{}, err
	}
	assets := make(map[string][]byte, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		limit := int64(maxScriptBody)
		if artifact.Name == "xkeen-control-linux-arm64" {
			limit = maxBinaryBody
		}
		contents, err := c.download(ctx, c.assetURL(channel, manifest.Version, artifact.Name), limit)
		if err != nil {
			return Candidate{}, err
		}
		assets[artifact.Name] = contents
	}
	candidate := Candidate{Manifest: manifest, Signature: signature, Assets: assets}
	if err := VerifyCandidate(candidate); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
}

func (c *Client) Check(ctx context.Context, channel, version string) (Manifest, error) {
	if channel != "stable" && channel != "beta" {
		return Manifest{}, errors.New("release channel is invalid")
	}
	if version != "" && !validSemver(strings.TrimPrefix(version, "v")) {
		return Manifest{}, errors.New("release version is invalid")
	}
	manifestBytes, err := c.download(ctx, c.assetURL(channel, version, "release-manifest.json"), maxManifestBody)
	if err != nil {
		return Manifest{}, err
	}
	manifest, err := ParseManifest(manifestBytes)
	if err != nil || manifest.Channel != channel || (version != "" && strings.TrimPrefix(manifest.Version, "v") != strings.TrimPrefix(version, "v")) {
		return Manifest{}, errors.New("release manifest is not the requested release")
	}
	signature, err := c.download(ctx, c.assetURL(channel, manifest.Version, "release-manifest.sig"), maxSignatureBody)
	if err != nil {
		return Manifest{}, err
	}
	if err := Verify(manifestBytes, signature, c.publicKey); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (c *Client) assetURL(channel, version, name string) string {
	if channel == "stable" && version == "" {
		return c.baseURL + "/releases/latest/download/" + url.PathEscape(name)
	}
	return c.baseURL + "/releases/download/v" + url.PathEscape(strings.TrimPrefix(version, "v")) + "/" + url.PathEscape(name)
}

func (c *Client) download(ctx context.Context, target string, limit int64) ([]byte, error) {
	if c == nil || c.http == nil || c.baseURL == "" {
		return nil, errors.New("release client is unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.New("release request is invalid")
	}
	request.Header.Set("User-Agent", "xkeen-control-release-client/1")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, errors.New("release download failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release download returned status %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, errors.New("release response exceeds limit")
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(contents)) > limit {
		return nil, errors.New("release response exceeds limit")
	}
	return contents, nil
}

func newHTTPClient(testHost string) *http.Client {
	return &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(request *http.Request, _ []*http.Request) error {
		host := strings.ToLower(request.URL.Host)
		if testHost != "" && strings.EqualFold(host, testHost) {
			return nil
		}
		switch host {
		case "github.com", "api.github.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
			return nil
		default:
			return errors.New("release redirect host is not supported")
		}
	}}
}

func ParseChannel(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "stable":
		return "stable", nil
	case "beta":
		return "beta", nil
	default:
		return "", errors.New("channel must be stable or beta")
	}
}

func ParseChannelMust(value string) string {
	parsed, err := ParseChannel(value)
	if err != nil {
		return ""
	}
	return parsed
}

func ReleaseNotesURL(version string) string {
	if version == "" || strings.ContainsAny(version, "/\\") {
		return ""
	}
	return "https://github.com/" + RepositoryOwner + "/" + RepositoryName + "/releases/tag/v" + url.PathEscape(strings.TrimPrefix(version, "v"))
}

func StableKeyConfigured(client *Client) bool {
	return client != nil && len(client.publicKey) == ed25519.PublicKeySize
}

func parsePositiveInt(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid positive integer")
	}
	return parsed, nil
}
