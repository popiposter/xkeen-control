package components

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/netguard"
)

const (
	xkeenArtifactUserAgent = "xkeen-control-xkeen-transaction/1"
	xkeenArtifactTimeout   = 3 * time.Minute
)

var (
	errXKeenArtifactHTTP     = errors.New("XKeen artifact request failed")
	errXKeenArtifactStatus   = errors.New("XKeen artifact response was rejected")
	errXKeenArtifactContent  = errors.New("XKeen artifact content was rejected")
	errXKeenArtifactRedirect = errors.New("XKeen artifact redirect was rejected")
)

// XKeenArtifactClient reads one exact Git blob selected by the server-owned
// catalog. It deliberately has no URL-taking method and never follows a
// redirect.
type XKeenArtifactClient struct {
	metadata *metadataClient
}

func NewXKeenArtifactDownloader(resolver netguard.IPResolver, supplied *http.Client) *XKeenArtifactClient {
	client := supplied
	if client == nil {
		client = newXKeenArtifactHTTPClient(resolver)
	} else {
		copy := *client
		client = &copy
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errXKeenArtifactRedirect }
	return &XKeenArtifactClient{
		metadata: &metadataClient{http: client, slots: make(chan struct{}, MaxConcurrentMetadata)},
	}
}

func newXKeenArtifactHTTPClient(resolver netguard.IPResolver) *http.Client {
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
	return &http.Client{Transport: transport, Timeout: xkeenArtifactTimeout}
}

type githubXKeenBlob struct {
	SHA      string `json:"sha"`
	Size     int64  `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

func (c *XKeenArtifactClient) DownloadXKeen(ctx context.Context, identity XKeenReleaseIdentity, destination io.Writer) error {
	if c == nil || c.metadata == nil || destination == nil {
		return ErrXKeenArtifactRejected
	}
	entry, err := installableXKeenEntry(identity)
	if err != nil || !validXKeenIdentity(identity) {
		return ErrXKeenArtifactRejected
	}
	body, err := c.metadata.fetch(ctx, xkeenBlobPathPrefix+entry.BlobSHA, newNetworkBudget())
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return errXKeenArtifactHTTP
	}
	contents, err := decodeXKeenBlob(body, entry)
	if err != nil {
		return err
	}
	written, err := destination.Write(contents)
	if err != nil {
		return err
	}
	if written != len(contents) {
		return io.ErrShortWrite
	}
	return nil
}

func decodeXKeenBlob(body []byte, entry xkeenCompatibilityEntry) ([]byte, error) {
	if len(body) == 0 || len(body) > MaxMetadataResponseBytes || entry.BlobSHA == "" || entry.SizeBytes <= 0 || entry.SizeBytes > MaxXKeenArchiveBytes || entry.SHA256 == "" {
		return nil, errXKeenArtifactContent
	}
	var blob githubXKeenBlob
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if err := decoder.Decode(&blob); err != nil {
		return nil, errXKeenArtifactContent
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errXKeenArtifactContent
	}
	if blob.SHA == "" || !strings.EqualFold(blob.SHA, entry.BlobSHA) || blob.Encoding != "base64" || blob.Size != entry.SizeBytes {
		return nil, errXKeenArtifactContent
	}
	encoded := strings.Builder{}
	encoded.Grow(len(blob.Content))
	for _, character := range blob.Content {
		if character == '\r' || character == '\n' {
			continue
		}
		if character > 0x7f {
			return nil, errXKeenArtifactContent
		}
		encoded.WriteRune(character)
	}
	contents, err := base64.StdEncoding.Strict().DecodeString(encoded.String())
	if err != nil || int64(len(contents)) != entry.SizeBytes {
		return nil, errXKeenArtifactContent
	}
	gitDigest := sha1.New()
	_, _ = io.WriteString(gitDigest, "blob "+strconv.FormatInt(int64(len(contents)), 10)+"\x00")
	_, _ = gitDigest.Write(contents)
	if !strings.EqualFold(hex.EncodeToString(gitDigest.Sum(nil)), entry.BlobSHA) {
		return nil, errXKeenArtifactContent
	}
	archiveDigest := sha256.Sum256(contents)
	if !strings.EqualFold(hex.EncodeToString(archiveDigest[:]), entry.SHA256) {
		return nil, errXKeenArtifactContent
	}
	return contents, nil
}

func validXKeenIdentity(identity XKeenReleaseIdentity) bool {
	if identity.GenerationSHA256 != "" && identity.Generation != "" && !strings.EqualFold(identity.GenerationSHA256, identity.Generation) {
		return false
	}
	entry, ok := reviewedXKeenEntry(identity.CommitSHA, identity.AssetName)
	if !ok || validateXKeenCompatibilityEntry(entry) != nil || !entry.Installable {
		return false
	}
	generation := identity.GenerationSHA256
	if generation == "" {
		generation = identity.Generation
	}
	return identity.Repository == entry.Repository && identity.Channel == entry.Channel && identity.Tag == entry.Tag &&
		identity.Version == entry.Version && identity.CommitSHA == entry.CommitSHA && identity.SourceParentSHA == entry.SourceParentSHA &&
		identity.AssetName == entry.AssetName && identity.BlobSHA == entry.BlobSHA && identity.SizeBytes == entry.SizeBytes &&
		strings.EqualFold(identity.SHA256, entry.SHA256) && strings.EqualFold(generation, entry.GenerationSHA256)
}
