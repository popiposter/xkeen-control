package components

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/netguard"
)

const (
	CheckSchemaVersion = 1

	DefaultCheckCacheTTL         = 5 * time.Minute
	DefaultNegativeCheckCacheTTL = 15 * time.Second
	MaxCheckDuration             = 30 * time.Second
	MaxMetadataResponseBytes     = 2 << 20
	MaxCheckNetworkBytes         = 8 << 20
	MaxReleaseAssets             = 256
	MaxMetadataStringBytes       = 256
	MaxCandidateAssetBytes       = 128 << 20
	MaxConcurrentMetadata        = 2
	MaxXKeenDevArtifactBytes     = 8 << 20
	MaxXKeenDevCommitFiles       = 64
	MaxXKeenDevTreeEntries       = 4096

	metadataHost                  = "api.github.com"
	metadataBaseURL               = "https://" + metadataHost
	xrayMetadataPath              = "/repos/XTLS/Xray-core/releases/latest"
	xrayCandidateAsset            = "Xray-linux-arm64-v8a.zip"
	xkeenDevRepository            = "jameszeroX/XKeen"
	xkeenDevSourceID              = "github/" + xkeenDevRepository
	xkeenDevChannel               = "dev"
	xkeenDevArtifactPath          = "test/xkeen.tar.gz"
	xkeenDevCommitListPath        = "/repos/" + xkeenDevRepository + "/commits?path=test%2Fxkeen.tar.gz&sha=main&per_page=1"
	xkeenDevCommitPathPrefix      = "/repos/" + xkeenDevRepository + "/commits/"
	xkeenDevTreePathPrefix        = "/repos/" + xkeenDevRepository + "/git/trees/"
	xkeenDevTreePathSuffix        = "?recursive=1"
	xkeenDevBuildCommitMessage    = "[github-actions] automated compiling build"
	metadataUserAgent             = "xkeen-control-component-check/1"
	metadataRequestTimeout        = 10 * time.Second
	metadataTLSHandshakeTimeout   = 5 * time.Second
	metadataResponseHeaderTimeout = 8 * time.Second
)

var (
	ErrInvalidCheckRequest = errors.New("component check request is invalid")
	ErrCheckUnavailable    = errors.New("component check is unavailable")
	ErrCheckBusy           = errors.New("component check is busy")
	ErrCheckTimeout        = errors.New("component check timed out")
	ErrUpstreamRejected    = errors.New("component metadata was rejected")

	errMetadataRedirect       = errors.New("metadata redirect rejected")
	errMetadataBodyTooLarge   = errors.New("metadata body exceeds the limit")
	errMetadataBudgetExceeded = errors.New("component check network budget exceeded")

	metadataGenerationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	metadataVersionPattern    = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

// CheckRequest is intentionally smaller than the inventory model. The API
// accepts only this closed component/channel tuple; all upstream identities
// remain server-owned constants.
type CheckRequest struct {
	Component ComponentKind `json:"component"`
	Channel   string        `json:"channel"`
}

func ValidateCheckRequest(request CheckRequest) error {
	switch request.Component {
	case KindXray, KindGeodata:
		if request.Channel == "stable" {
			return nil
		}
	case KindXKeen:
		if request.Channel == xkeenDevChannel {
			return nil
		}
	default:
		// Fall through to the common closed-tuple rejection below.
	}
	return ErrInvalidCheckRequest
}

// CheckService is deliberately separate from ReadOnlyService. A check may
// use a previously collected RAM inventory comparison, but it must never turn
// GET /api/v1/components into a network-capable operation.
type CheckService interface {
	Check(context.Context, CheckRequest) (CheckResult, error)
}

type CheckCandidate struct {
	Version         string `json:"version,omitempty"`
	Generation      string `json:"generation,omitempty"`
	AssetName       string `json:"assetName,omitempty"`
	SizeBytes       int64  `json:"sizeBytes,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	BuildCommitSHA  string `json:"buildCommitSha,omitempty"`
	SourceCommitSHA string `json:"sourceCommitSha,omitempty"`
	BlobSHA         string `json:"blobSha,omitempty"`
}

// XrayReleaseIdentity is the server-owned identity of one exact Xray
// release asset. It is intentionally not a URL or a caller-provided download
// descriptor. Phase C re-resolves this identity immediately before staging so
// a cached Phase B result can never authorize activation.
type XrayReleaseIdentity struct {
	Tag       string
	Version   string
	AssetName string
	SizeBytes int64
	SHA256    string
}

// XrayCandidateResolver is purpose-specific. It resolves only the fixed
// official Xray release source and cannot be used as a generic URL fetcher.
type XrayCandidateResolver interface {
	ResolveXray(context.Context) (XrayReleaseIdentity, error)
}

// XrayResolver performs an uncached, server-owned Xray release resolution.
// The Phase B Checker deliberately does not share its RAM cache with this
// primitive.
type XrayResolver struct {
	client *metadataClient
}

// NewXrayResolver constructs the fixed official Xray resolver. A supplied
// client is a test seam only; production callers pass nil and receive the
// netguard-protected transport.
func NewXrayResolver(resolver netguard.IPResolver, supplied *http.Client) *XrayResolver {
	return &XrayResolver{client: newMetadataClient(resolver, supplied)}
}

// ResolveXray always performs a fresh metadata request. It returns the exact
// upstream tag as well as the normalized typed version and asset digest.
func (r *XrayResolver) ResolveXray(ctx context.Context) (XrayReleaseIdentity, error) {
	if r == nil || r.client == nil {
		return XrayReleaseIdentity{}, ErrXrayResolutionUnavailable
	}
	body, err := r.client.fetch(ctx, xrayMetadataPath, newNetworkBudget())
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return XrayReleaseIdentity{}, ctx.Err()
		}
		return XrayReleaseIdentity{}, ErrXrayResolutionUnavailable
	}
	release, failure := decodeReleaseMetadata(body)
	if failure != nil {
		return XrayReleaseIdentity{}, ErrXrayCandidateRejected
	}
	if failure = validateReleaseMetadata(release); failure != nil {
		return XrayReleaseIdentity{}, ErrXrayCandidateRejected
	}
	version, ok := parseStrictVersion(release.TagName)
	if !ok {
		return XrayReleaseIdentity{}, ErrXrayCandidateRejected
	}
	asset, failure := selectMetadataAsset(release.Assets, xrayCandidateAsset, true)
	if failure != nil {
		return XrayReleaseIdentity{}, ErrXrayCandidateRejected
	}
	return XrayReleaseIdentity{
		Tag:       release.TagName,
		Version:   version.String(),
		AssetName: xrayCandidateAsset,
		SizeBytes: asset.Size,
		SHA256:    asset.SHA256,
	}, nil
}

type CheckItem struct {
	ID             string `json:"id"`
	SourceID       string `json:"sourceId"`
	Generation     string `json:"generation,omitempty"`
	AssetName      string `json:"assetName,omitempty"`
	SizeBytes      int64  `json:"sizeBytes,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	InstalledState string `json:"installedState"`
	Eligible       bool   `json:"eligible"`
	ReasonCode     string `json:"reasonCode,omitempty"`
}

// CheckResult is a safe, bounded metadata projection. It contains no URL,
// release body, command output, local path or upstream error text.
type CheckResult struct {
	SchemaVersion     int             `json:"schemaVersion"`
	Component         ComponentKind   `json:"component"`
	Channel           string          `json:"channel"`
	SourceID          string          `json:"sourceId"`
	CheckedAt         time.Time       `json:"checkedAt"`
	Candidate         *CheckCandidate `json:"candidate,omitempty"`
	Items             []CheckItem     `json:"items,omitempty"`
	InstalledState    string          `json:"installedState"`
	Eligible          bool            `json:"eligible"`
	MutationAvailable bool            `json:"mutationAvailable"`
	ReasonCode        string          `json:"reasonCode,omitempty"`
}

type InstalledSnapshot func() (Inventory, bool)

type CheckerConfig struct {
	InstalledSnapshot InstalledSnapshot
	Resolver          netguard.IPResolver
	HTTPClient        *http.Client
	CacheTTL          time.Duration
	NegativeCacheTTL  time.Duration
	Now               func() time.Time
}

type Checker struct {
	client            *metadataClient
	installedSnapshot InstalledSnapshot
	cacheTTL          time.Duration
	negativeCacheTTL  time.Duration
	now               func() time.Time

	mu       sync.Mutex
	cache    map[string]checkCacheEntry
	inFlight map[string]*checkFlight
}

type checkCacheEntry struct {
	result  CheckResult
	err     error
	expires time.Time
}

type checkFlight struct {
	done   chan struct{}
	result CheckResult
	err    error
}

func NewChecker(config CheckerConfig) *Checker {
	cacheTTL := config.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = DefaultCheckCacheTTL
	}
	negativeTTL := config.NegativeCacheTTL
	if negativeTTL <= 0 {
		negativeTTL = DefaultNegativeCheckCacheTTL
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	client := newMetadataClient(config.Resolver, config.HTTPClient)
	return &Checker{
		client:            client,
		installedSnapshot: config.InstalledSnapshot,
		cacheTTL:          cacheTTL,
		negativeCacheTTL:  negativeTTL,
		now:               now,
		cache:             make(map[string]checkCacheEntry, 3),
		inFlight:          make(map[string]*checkFlight, 3),
	}
}

// NewCheckService is an explicit constructor alias for callers that describe
// the dependency by its HTTP-facing service role.
func NewCheckService(config CheckerConfig) *Checker { return NewChecker(config) }

func (c *Checker) Check(ctx context.Context, request CheckRequest) (CheckResult, error) {
	if err := ValidateCheckRequest(request); err != nil {
		return CheckResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := string(request.Component) + ":" + request.Channel
	now := c.clock()

	c.mu.Lock()
	if entry, ok := c.cache[key]; ok && now.Before(entry.expires) {
		result, err := cloneCheckResult(entry.result), entry.err
		c.mu.Unlock()
		return result, err
	}
	if flight, ok := c.inFlight[key]; ok {
		c.mu.Unlock()
		select {
		case <-flight.done:
			return cloneCheckResult(flight.result), flight.err
		case <-ctx.Done():
			return CheckResult{}, ErrCheckTimeout
		}
	}
	flight := &checkFlight{done: make(chan struct{})}
	c.inFlight[key] = flight
	c.mu.Unlock()

	checkContext, cancel := context.WithTimeout(ctx, MaxCheckDuration)
	result, err := c.check(checkContext, request)
	cancel()
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		err = ErrCheckTimeout
	}

	c.mu.Lock()
	delete(c.inFlight, key)
	flight.result = cloneCheckResult(result)
	flight.err = err
	if err == nil {
		c.cache[key] = checkCacheEntry{result: cloneCheckResult(result), expires: c.clock().Add(c.cacheTTL)}
	} else if !errors.Is(err, ErrInvalidCheckRequest) {
		c.cache[key] = checkCacheEntry{err: err, expires: c.clock().Add(c.negativeCacheTTL)}
	}
	close(flight.done)
	c.mu.Unlock()
	return result, err
}

func (c *Checker) clock() time.Time {
	if c == nil || c.now == nil {
		return time.Now().UTC()
	}
	return c.now().UTC()
}

func (c *Checker) check(ctx context.Context, request CheckRequest) (CheckResult, error) {
	installed, haveInstalled := c.installed()
	switch request.Component {
	case KindXray:
		return c.checkXray(ctx, request.Channel, installed, haveInstalled)
	case KindXKeen:
		return c.checkXKeen(ctx, request.Channel, installed, haveInstalled)
	case KindGeodata:
		return c.checkGeodata(ctx, request.Channel, installed, haveInstalled)
	default:
		return CheckResult{}, ErrInvalidCheckRequest
	}
}

func (c *Checker) installed() (Inventory, bool) {
	if c == nil || c.installedSnapshot == nil {
		return Inventory{}, false
	}
	return c.installedSnapshot()
}

func newCheckResult(component ComponentKind, channel, sourceID string, checkedAt time.Time) CheckResult {
	return CheckResult{
		SchemaVersion:     CheckSchemaVersion,
		Component:         component,
		Channel:           channel,
		SourceID:          sourceID,
		CheckedAt:         checkedAt.UTC(),
		InstalledState:    "unknown",
		MutationAvailable: false,
	}
}

func (c *Checker) checkXray(ctx context.Context, channel string, installed Inventory, haveInstalled bool) (CheckResult, error) {
	result := newCheckResult(KindXray, channel, "github/XTLS/Xray-core", c.clock())
	budget := newNetworkBudget()
	body, err := c.client.fetch(ctx, xrayMetadataPath, budget)
	if err != nil {
		var failure *metadataFailure
		if errors.As(err, &failure) {
			result.ReasonCode = failure.reason
			return result, nil
		}
		return CheckResult{}, err
	}
	release, failure := decodeReleaseMetadata(body)
	if failure != nil {
		result.ReasonCode = failure.reason
		return result, nil
	}
	if failure = validateReleaseMetadata(release); failure != nil {
		result.ReasonCode = failure.reason
		return result, nil
	}
	version, ok := parseStrictVersion(release.TagName)
	if !ok {
		result.ReasonCode = "version-invalid"
		return result, nil
	}
	asset, failure := selectMetadataAsset(release.Assets, xrayCandidateAsset, true)
	if failure != nil {
		if failure.reason == "asset-missing" && hasWrongXrayAsset(release.Assets) {
			failure.reason = "asset-wrong-architecture"
		}
		result.ReasonCode = failure.reason
		return result, nil
	}
	result.Candidate = &CheckCandidate{
		Version:   version.String(),
		AssetName: xrayCandidateAsset,
		SizeBytes: asset.Size,
		SHA256:    asset.SHA256,
	}
	if haveInstalled {
		result.InstalledState = compareInstalledVersion(installed.Xray, version)
	}
	result.Eligible = true
	result.ReasonCode = "supported-for-preview"
	return result, nil
}

func (c *Checker) checkXKeen(ctx context.Context, channel string, installed Inventory, haveInstalled bool) (CheckResult, error) {
	result := newCheckResult(KindXKeen, channel, xkeenDevSourceID, c.clock())
	budget := newNetworkBudget()
	identity, failure, err := c.resolveXKeenDevBuild(ctx, budget)
	if err != nil {
		var metadataFailureValue *metadataFailure
		if errors.As(err, &metadataFailureValue) {
			result.ReasonCode = metadataFailureValue.reason
			return result, nil
		}
		return CheckResult{}, err
	}
	if failure != nil {
		result.ReasonCode = failure.reason
		return result, nil
	}
	entry, qualified := reviewedXKeenDevBuild(identity)
	result.Candidate = &CheckCandidate{
		AssetName:       xkeenDevArtifactPath,
		SizeBytes:       identity.SizeBytes,
		BuildCommitSHA:  identity.BuildCommitSHA,
		SourceCommitSHA: identity.SourceCommitSHA,
		BlobSHA:         identity.BlobSHA,
	}
	if haveInstalled {
		result.InstalledState = compareInstalledXKeenDev(installed.XKeen, identity.BuildCommitSHA)
	}
	if !qualified {
		// Keep the moving build identity visible, but never inherit trust from a
		// catalog entry whose commit, source parent, blob or size differs.
		result.ReasonCode = "compatibility-catalog-required"
		return result, nil
	}
	result.Candidate.Version = entry.Version
	result.Candidate.Generation = entry.GenerationSHA256
	result.Candidate.SHA256 = entry.SHA256
	result.Eligible = true
	result.ReasonCode = "catalog-qualified"
	return result, nil
}

func reviewedXKeenDevBuild(identity xkeenDevBuildIdentity) (xkeenCompatibilityEntry, bool) {
	entry, ok := reviewedXKeenEntry(identity.BuildCommitSHA, xkeenDevArtifactPath)
	if !ok || validateXKeenCompatibilityEntry(entry) != nil || !entry.Installable {
		return xkeenCompatibilityEntry{}, false
	}
	if entry.Repository != xkeenDevRepository || entry.Channel != xkeenDevChannel ||
		entry.SourceParentSHA != identity.SourceCommitSHA || entry.BlobSHA != identity.BlobSHA ||
		entry.SizeBytes != identity.SizeBytes {
		return xkeenCompatibilityEntry{}, false
	}
	return entry, true
}

type xkeenDevBuildIdentity struct {
	BuildCommitSHA  string
	SourceCommitSHA string
	BlobSHA         string
	SizeBytes       int64
}

type githubXKeenCommitListItem struct {
	SHA string `json:"sha"`
}

type githubXKeenCommitFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
	SHA      string `json:"sha"`
}

type githubXKeenCommitMetadata struct {
	SHA     string `json:"sha"`
	Parents []struct {
		SHA string `json:"sha"`
	} `json:"parents"`
	Commit struct {
		Message      string `json:"message"`
		Verification struct {
			Verified bool `json:"verified"`
		} `json:"verification"`
	} `json:"commit"`
	Files []githubXKeenCommitFile `json:"files"`
}

type githubXKeenTreeMetadata struct {
	Truncated bool `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
		Size int64  `json:"size"`
	} `json:"tree"`
}

func (c *Checker) resolveXKeenDevBuild(ctx context.Context, budget *networkBudget) (xkeenDevBuildIdentity, *metadataFailure, error) {
	body, err := c.client.fetch(ctx, xkeenDevCommitListPath, budget)
	if err != nil {
		return xkeenDevBuildIdentity{}, nil, err
	}
	var commits []githubXKeenCommitListItem
	if err := json.Unmarshal(body, &commits); err != nil || len(commits) != 1 || !isGitSHA1(commits[0].SHA) {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-build-invalid"}, nil
	}
	buildCommit := strings.ToLower(commits[0].SHA)

	commitPath := xkeenDevCommitPathPrefix + buildCommit
	body, err = c.client.fetch(ctx, commitPath, budget)
	if err != nil {
		return xkeenDevBuildIdentity{}, nil, err
	}
	var commit githubXKeenCommitMetadata
	if err := json.Unmarshal(body, &commit); err != nil || !strings.EqualFold(commit.SHA, buildCommit) {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-build-invalid"}, nil
	}
	if !commit.Commit.Verification.Verified {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-build-unverified"}, nil
	}
	if strings.TrimSpace(commit.Commit.Message) != xkeenDevBuildCommitMessage {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-build-not-automated"}, nil
	}
	if len(commit.Parents) != 1 || !isGitSHA1(commit.Parents[0].SHA) {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-build-shape-invalid"}, nil
	}
	if len(commit.Files) > MaxXKeenDevCommitFiles || len(commit.Files) != 1 {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-build-shape-invalid"}, nil
	}
	changedFile := commit.Files[0]
	if changedFile.Filename != xkeenDevArtifactPath || changedFile.Status != "modified" || !isGitSHA1(changedFile.SHA) {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-build-shape-invalid"}, nil
	}
	sourceCommit := strings.ToLower(commit.Parents[0].SHA)

	treePath := xkeenDevTreePathPrefix + buildCommit + xkeenDevTreePathSuffix
	body, err = c.client.fetch(ctx, treePath, budget)
	if err != nil {
		return xkeenDevBuildIdentity{}, nil, err
	}
	var tree githubXKeenTreeMetadata
	if err := json.Unmarshal(body, &tree); err != nil || tree.Truncated || len(tree.Tree) > MaxXKeenDevTreeEntries {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-artifact-tree-invalid"}, nil
	}
	matches := 0
	var artifact struct {
		Path string
		Mode string
		Type string
		SHA  string
		Size int64
	}
	for _, entry := range tree.Tree {
		if entry.Path != xkeenDevArtifactPath {
			continue
		}
		matches++
		artifact.Path = entry.Path
		artifact.Mode = entry.Mode
		artifact.Type = entry.Type
		artifact.SHA = entry.SHA
		artifact.Size = entry.Size
	}
	if matches != 1 || artifact.Type != "blob" || artifact.Mode != "100644" || !isGitSHA1(artifact.SHA) {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-artifact-invalid"}, nil
	}
	if artifact.Size <= 0 {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "asset-size-invalid"}, nil
	}
	if artifact.Size > MaxXKeenDevArtifactBytes {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "asset-size-too-large"}, nil
	}
	if !strings.EqualFold(changedFile.SHA, artifact.SHA) {
		return xkeenDevBuildIdentity{}, &metadataFailure{reason: "dev-build-shape-invalid"}, nil
	}
	return xkeenDevBuildIdentity{
		BuildCommitSHA:  buildCommit,
		SourceCommitSHA: sourceCommit,
		BlobSHA:         strings.ToLower(artifact.SHA),
		SizeBytes:       artifact.Size,
	}, nil, nil
}

func compareInstalledXKeenDev(installed Component, candidateBuild string) string {
	if installed.State == StateMissing {
		return "not-installed"
	}
	if installed.SourceCommit == "" || !isGitSHA1(installed.SourceCommit) {
		return "unknown"
	}
	if strings.EqualFold(installed.SourceCommit, candidateBuild) {
		return "current"
	}
	return "changed"
}

func (c *Checker) checkGeodata(ctx context.Context, channel string, installed Inventory, haveInstalled bool) (CheckResult, error) {
	result := newCheckResult(KindGeodata, channel, "github/product-geodata-catalog", c.clock())
	result.Items = make([]CheckItem, len(productGeodataCatalog))
	budget := newNetworkBudget()

	paths := make([]string, 0, len(productGeodataCatalog))
	seenPaths := make(map[string]struct{}, len(productGeodataCatalog))
	for _, entry := range productGeodataCatalog {
		path := geodataMetadataPath(entry)
		if _, seen := seenPaths[path]; seen {
			continue
		}
		seenPaths[path] = struct{}{}
		paths = append(paths, path)
	}
	type job struct{ path string }
	type outcome struct {
		path    string
		release githubReleaseMetadata
		failure *metadataFailure
		err     error
	}
	jobs := make(chan job)
	outcomes := make(chan outcome, len(paths))
	workerCount := MaxConcurrentMetadata
	if workerCount > len(paths) {
		workerCount = len(paths)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for task := range jobs {
				body, err := c.client.fetch(ctx, task.path, budget)
				if err != nil {
					outcomes <- outcome{path: task.path, err: err}
					continue
				}
				release, failure := decodeReleaseMetadata(body)
				outcomes <- outcome{path: task.path, release: release, failure: failure}
			}
		}()
	}
	go func() {
		for _, path := range paths {
			jobs <- job{path: path}
		}
		close(jobs)
		workers.Wait()
		close(outcomes)
	}()

	metadataByPath := make(map[string]outcome, len(paths))
	for item := range outcomes {
		metadataByPath[item.path] = item
	}
	fetchedSources := 0
	for index, entry := range productGeodataCatalog {
		metadata := metadataByPath[geodataMetadataPath(entry)]
		if metadata.err != nil {
			result.Items[index] = CheckItem{
				ID:             entry.ID,
				SourceID:       "github/" + entry.Repository,
				InstalledState: "unknown",
				ReasonCode:     metadataErrorReason(metadata.err),
			}
			continue
		}
		fetchedSources++
		if metadata.failure != nil {
			result.Items[index] = CheckItem{
				ID:             entry.ID,
				SourceID:       "github/" + entry.Repository,
				InstalledState: "unknown",
				ReasonCode:     metadata.failure.reason,
			}
			continue
		}
		if failure := validateReleaseMetadata(metadata.release); failure != nil {
			result.Items[index] = CheckItem{
				ID:             entry.ID,
				SourceID:       "github/" + entry.Repository,
				InstalledState: "unknown",
				ReasonCode:     failure.reason,
			}
			continue
		}
		result.Items[index] = c.checkGeodataRelease(entry, metadata.release, installed, haveInstalled)
	}
	if fetchedSources == 0 {
		for _, metadata := range metadataByPath {
			if errors.Is(metadata.err, ErrCheckTimeout) {
				return CheckResult{}, ErrCheckTimeout
			}
		}
		return CheckResult{}, ErrUpstreamRejected
	}

	result.Eligible = true
	for _, item := range result.Items {
		if !item.Eligible {
			result.Eligible = false
		}
	}
	result.InstalledState = aggregateInstalledState(result.Items)
	if result.Eligible {
		result.ReasonCode = "supported-for-preview"
	} else {
		result.ReasonCode = "required-candidate-ineligible"
	}
	return result, nil
}

func (c *Checker) checkGeodataRelease(entry catalogEntry, release githubReleaseMetadata, installed Inventory, haveInstalled bool) CheckItem {
	item := CheckItem{
		ID:             entry.ID,
		SourceID:       "github/" + entry.Repository,
		InstalledState: "unknown",
	}
	if !metadataGenerationPattern.MatchString(release.TagName) {
		item.ReasonCode = "generation-invalid"
		return item
	}
	asset, failure := selectMetadataAsset(release.Assets, entry.Asset, true)
	if failure != nil {
		item.ReasonCode = failure.reason
		return item
	}
	item.Generation = release.TagName
	item.AssetName = entry.Asset
	item.SizeBytes = asset.Size
	item.SHA256 = asset.SHA256
	item.Eligible = true
	if haveInstalled {
		item.InstalledState = compareInstalledGeodata(installed.Geodata, entry.ID, asset.SHA256)
	}
	return item
}

func geodataMetadataPath(entry catalogEntry) string {
	return "/repos/" + entry.Repository + "/releases/latest"
}

func aggregateInstalledState(items []CheckItem) string {
	if len(items) == 0 {
		return "unknown"
	}
	allCurrent := true
	anyChanged := false
	anyNotInstalled := false
	for _, item := range items {
		switch item.InstalledState {
		case "changed":
			anyChanged = true
			allCurrent = false
		case "not-installed":
			anyNotInstalled = true
			allCurrent = false
		case "current":
		default:
			allCurrent = false
		}
	}
	if allCurrent {
		return "current"
	}
	if anyChanged {
		return "changed"
	}
	if anyNotInstalled {
		return "not-installed"
	}
	return "unknown"
}

type githubReleaseMetadata struct {
	Draft      *bool                 `json:"draft"`
	Prerelease *bool                 `json:"prerelease"`
	TagName    string                `json:"tag_name"`
	Assets     []githubAssetMetadata `json:"assets"`
}

type githubAssetMetadata struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	State  string `json:"state"`
	Digest string `json:"digest"`
}

type metadataFailure struct {
	reason string
}

func (f *metadataFailure) Error() string { return "component metadata rejected" }

func decodeReleaseMetadata(body []byte) (githubReleaseMetadata, *metadataFailure) {
	if len(body) == 0 || len(body) > MaxMetadataResponseBytes {
		return githubReleaseMetadata{}, &metadataFailure{reason: "metadata-too-large"}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var release githubReleaseMetadata
	if err := decoder.Decode(&release); err != nil {
		return githubReleaseMetadata{}, &metadataFailure{reason: "metadata-invalid"}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return githubReleaseMetadata{}, &metadataFailure{reason: "metadata-invalid"}
	}
	if len(release.Assets) > MaxReleaseAssets {
		return githubReleaseMetadata{}, &metadataFailure{reason: "asset-count-exceeded"}
	}
	if len(release.TagName) > MaxMetadataStringBytes {
		return githubReleaseMetadata{}, &metadataFailure{reason: "metadata-string-too-long"}
	}
	for _, asset := range release.Assets {
		if len(asset.Name) > MaxMetadataStringBytes || len(asset.State) > MaxMetadataStringBytes || len(asset.Digest) > MaxMetadataStringBytes {
			return githubReleaseMetadata{}, &metadataFailure{reason: "metadata-string-too-long"}
		}
	}
	return release, nil
}

func validateReleaseMetadata(release githubReleaseMetadata) *metadataFailure {
	switch {
	case release.Draft == nil || release.Prerelease == nil:
		return &metadataFailure{reason: "metadata-invalid"}
	case *release.Draft:
		return &metadataFailure{reason: "release-draft"}
	case *release.Prerelease:
		return &metadataFailure{reason: "release-prerelease"}
	case release.TagName == "":
		return &metadataFailure{reason: "generation-missing"}
	default:
		return nil
	}
}

type selectedMetadataAsset struct {
	Size   int64
	SHA256 string
}

func selectMetadataAsset(assets []githubAssetMetadata, expected string, digestRequired bool) (selectedMetadataAsset, *metadataFailure) {
	matches := 0
	var selected githubAssetMetadata
	for _, asset := range assets {
		if asset.Name == expected {
			matches++
			selected = asset
		}
	}
	if matches == 0 {
		return selectedMetadataAsset{}, &metadataFailure{reason: "asset-missing"}
	}
	if matches != 1 {
		return selectedMetadataAsset{}, &metadataFailure{reason: "asset-duplicate"}
	}
	if selected.State != "uploaded" {
		return selectedMetadataAsset{}, &metadataFailure{reason: "asset-not-uploaded"}
	}
	if selected.Size <= 0 {
		return selectedMetadataAsset{}, &metadataFailure{reason: "asset-size-invalid"}
	}
	if selected.Size > MaxCandidateAssetBytes {
		return selectedMetadataAsset{}, &metadataFailure{reason: "asset-size-too-large"}
	}
	sha, ok := normalizeMetadataSHA256(selected.Digest)
	if !ok {
		if digestRequired {
			if selected.Digest == "" {
				return selectedMetadataAsset{}, &metadataFailure{reason: "digest-unavailable"}
			}
			return selectedMetadataAsset{}, &metadataFailure{reason: "digest-invalid"}
		}
		if selected.Digest != "" {
			return selectedMetadataAsset{}, &metadataFailure{reason: "digest-invalid"}
		}
	}
	return selectedMetadataAsset{Size: selected.Size, SHA256: sha}, nil
}

func hasWrongXrayAsset(assets []githubAssetMetadata) bool {
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.HasPrefix(name, "xray-linux-") && name != strings.ToLower(xrayCandidateAsset) {
			return true
		}
	}
	return false
}

func normalizeMetadataSHA256(value string) (string, bool) {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return "", false
	}
	decoded, err := hex.DecodeString(value[len("sha256:"):])
	if err != nil || len(decoded) != 32 {
		return "", false
	}
	return strings.ToLower(value[len("sha256:"):]), true
}

type semanticVersion struct {
	Major uint64
	Minor uint64
	Patch uint64
}

func (v semanticVersion) String() string {
	return strconv.FormatUint(v.Major, 10) + "." + strconv.FormatUint(v.Minor, 10) + "." + strconv.FormatUint(v.Patch, 10)
}

func parseStrictVersion(value string) (semanticVersion, bool) {
	matches := metadataVersionPattern.FindStringSubmatch(value)
	if len(matches) != 4 {
		return semanticVersion{}, false
	}
	major, errMajor := strconv.ParseUint(matches[1], 10, 64)
	minor, errMinor := strconv.ParseUint(matches[2], 10, 64)
	patch, errPatch := strconv.ParseUint(matches[3], 10, 64)
	if errMajor != nil || errMinor != nil || errPatch != nil {
		return semanticVersion{}, false
	}
	return semanticVersion{Major: major, Minor: minor, Patch: patch}, true
}

func compareSemanticVersion(left, right semanticVersion) int {
	if left.Major != right.Major {
		if left.Major < right.Major {
			return -1
		}
		return 1
	}
	if left.Minor != right.Minor {
		if left.Minor < right.Minor {
			return -1
		}
		return 1
	}
	if left.Patch < right.Patch {
		return -1
	}
	if left.Patch > right.Patch {
		return 1
	}
	return 0
}

func compareInstalledVersion(installed Component, candidate semanticVersion) string {
	if installed.State == StateMissing {
		return "not-installed"
	}
	if installed.VersionUnknown {
		return "unknown"
	}
	current, ok := parseStrictVersion(installed.Version)
	if !ok {
		return "unknown"
	}
	switch compareSemanticVersion(current, candidate) {
	case 0:
		return "current"
	case -1:
		return "update-available"
	default:
		return "candidate-older"
	}
}

func compareInstalledOpaque(installed Component, candidate string) string {
	if installed.State == StateMissing {
		return "not-installed"
	}
	if installed.VersionUnknown || installed.Version == "" {
		return "unknown"
	}
	if strings.TrimPrefix(installed.Version, "v") == strings.TrimPrefix(candidate, "v") {
		return "current"
	}
	return "changed"
}

func compareInstalledGeodata(geodata GeodataComponent, id, candidateSHA string) string {
	for _, item := range geodata.Items {
		if item.ID != id {
			continue
		}
		if item.State == StateMissing {
			return "not-installed"
		}
		if item.SHA256 == "" || !isHexSHA256(item.SHA256) {
			return "unknown"
		}
		if strings.EqualFold(item.SHA256, candidateSHA) {
			return "current"
		}
		return "changed"
	}
	return "not-installed"
}

func isHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isGitSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 20
}

func cloneCheckResult(value CheckResult) CheckResult {
	clone := value
	if value.Candidate != nil {
		candidate := *value.Candidate
		clone.Candidate = &candidate
	}
	if value.Items != nil {
		clone.Items = append([]CheckItem(nil), value.Items...)
	}
	return clone
}

type metadataClient struct {
	http  *http.Client
	slots chan struct{}
}

func newMetadataClient(resolver netguard.IPResolver, supplied *http.Client) *metadataClient {
	client := supplied
	if client == nil {
		client = newMetadataHTTPClient(resolver)
	} else {
		copy := *client
		client = &copy
	}
	// Even a test-injected client must not follow metadata redirects. The
	// production client also rejects them at the transport boundary.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errMetadataRedirect }
	return &metadataClient{http: client, slots: make(chan struct{}, MaxConcurrentMetadata)}
}

func newMetadataHTTPClient(resolver netguard.IPResolver) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           netguard.Dialer{Resolver: resolver}.DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   metadataTLSHandshakeTimeout,
		ResponseHeaderTimeout: metadataResponseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       5 * time.Second,
		MaxIdleConns:          MaxConcurrentMetadata,
	}
	return &http.Client{Transport: transport, Timeout: metadataRequestTimeout}
}

func (c *metadataClient) fetch(ctx context.Context, path string, budget *networkBudget) ([]byte, error) {
	if c == nil || c.http == nil || !isFixedMetadataPath(path) {
		return nil, ErrCheckUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if budget == nil {
		budget = newNetworkBudget()
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, ErrCheckTimeout
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataBaseURL+path, nil)
	if err != nil {
		return nil, ErrCheckUnavailable
	}
	if !isFixedMetadataURL(request.URL) {
		return nil, ErrCheckUnavailable
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", metadataUserAgent)
	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrCheckTimeout
		}
		if errors.Is(err, errMetadataRedirect) {
			return nil, &metadataFailure{reason: "redirect-rejected"}
		}
		if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
			return nil, ErrCheckTimeout
		}
		return nil, ErrUpstreamRejected
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
			return nil, &metadataFailure{reason: "redirect-rejected"}
		}
		return nil, ErrUpstreamRejected
	}
	if response.ContentLength > MaxMetadataResponseBytes {
		return nil, &metadataFailure{reason: "metadata-too-large"}
	}
	if response.ContentLength > budget.remaining() {
		return nil, errMetadataBudgetExceeded
	}
	body, err := budget.read(ctx, response.Body, MaxMetadataResponseBytes)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, ErrCheckTimeout
		case errors.Is(err, errMetadataBodyTooLarge):
			return nil, &metadataFailure{reason: "metadata-too-large"}
		case errors.Is(err, errMetadataBudgetExceeded):
			return nil, errMetadataBudgetExceeded
		default:
			return nil, ErrUpstreamRejected
		}
	}
	return body, nil
}

func isFixedMetadataPath(path string) bool {
	if path == xrayMetadataPath || path == xkeenDevCommitListPath || isFixedXKeenMetadataPath(path) {
		return true
	}
	if strings.HasPrefix(path, xkeenDevCommitPathPrefix) && isGitSHA1(strings.TrimPrefix(path, xkeenDevCommitPathPrefix)) {
		return true
	}
	if strings.HasPrefix(path, xkeenDevTreePathPrefix) && strings.HasSuffix(path, xkeenDevTreePathSuffix) {
		commit := strings.TrimSuffix(strings.TrimPrefix(path, xkeenDevTreePathPrefix), xkeenDevTreePathSuffix)
		if isGitSHA1(commit) {
			return true
		}
	}
	for _, entry := range productGeodataCatalog {
		if path == geodataMetadataPath(entry) {
			return true
		}
	}
	return false
}

func isFixedXKeenMetadataPath(value string) bool {
	if value == xkeenBuildCommitPath || value == xkeenBuildTreePath {
		return true
	}
	for _, entry := range reviewedXKeenCompatibility {
		if isHexSHA1(entry.BlobSHA) && value == xkeenBlobPathPrefix+entry.BlobSHA {
			return true
		}
	}
	return false
}

func isFixedMetadataURL(value *url.URL) bool {
	if value == nil || value.Scheme != "https" || value.Host != metadataHost {
		return false
	}
	path := value.EscapedPath()
	if value.RawQuery != "" {
		path += "?" + value.RawQuery
	}
	return isFixedMetadataPath(path)
}

func metadataErrorReason(err error) string {
	var failure *metadataFailure
	if errors.As(err, &failure) && failure.reason != "" {
		return failure.reason
	}
	switch {
	case errors.Is(err, errMetadataBudgetExceeded):
		return "network-budget-exceeded"
	case errors.Is(err, ErrCheckTimeout):
		return "check-timeout"
	case errors.Is(err, ErrUpstreamRejected):
		return "upstream-rejected"
	default:
		return "metadata-unavailable"
	}
}

type networkBudget struct {
	mu             sync.Mutex
	remainingBytes int64
}

func newNetworkBudget() *networkBudget {
	return &networkBudget{remainingBytes: MaxCheckNetworkBytes}
}

func (b *networkBudget) remaining() int64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remainingBytes
}

func (b *networkBudget) read(ctx context.Context, body io.Reader, limit int64) ([]byte, error) {
	if b == nil {
		return nil, errMetadataBudgetExceeded
	}
	var output bytes.Buffer
	buffer := make([]byte, 32<<10)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b.mu.Lock()
		remainingBudget := b.remainingBytes
		remainingBody := limit - int64(output.Len())
		if remainingBudget > 0 && remainingBody > 0 {
			allowed := int64(len(buffer))
			if allowed > remainingBudget {
				allowed = remainingBudget
			}
			if allowed > remainingBody {
				allowed = remainingBody
			}
			n, err := body.Read(buffer[:allowed])
			if n > 0 {
				b.remainingBytes -= int64(n)
				_, _ = output.Write(buffer[:n])
			}
			b.mu.Unlock()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return output.Bytes(), nil
				}
				return nil, err
			}
			if n == 0 {
				return nil, errors.New("metadata body made no progress")
			}
			continue
		}
		b.mu.Unlock()

		// Probe one byte only to distinguish an exactly-at-limit body from a
		// body that would exceed the bound. The probe is never retained.
		var probe [1]byte
		n, err := body.Read(probe[:])
		if n > 0 {
			if remainingBody <= 0 {
				return nil, errMetadataBodyTooLarge
			}
			return nil, errMetadataBudgetExceeded
		}
		if errors.Is(err, io.EOF) {
			return output.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
	}
}
