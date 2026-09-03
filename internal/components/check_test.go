package components

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixedCheckResolver struct {
	addresses []net.IPAddr
}

func (r fixedCheckResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return append([]net.IPAddr(nil), r.addresses...), nil
}

type metadataFixtureTransport struct {
	mu        sync.Mutex
	responses map[string][]byte
	calls     []string
	current   atomic.Int32
	max       atomic.Int32
	delay     time.Duration
	started   chan struct{}
	release   <-chan struct{}
}

func (transport *metadataFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != metadataHost {
		return nil, errors.New("unexpected metadata destination")
	}
	transport.mu.Lock()
	transport.calls = append(transport.calls, request.URL.Path)
	body, ok := transport.responses[request.URL.Path]
	transport.mu.Unlock()
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Request: request}, nil
	}
	current := transport.current.Add(1)
	defer transport.current.Add(-1)
	for {
		old := transport.max.Load()
		if current <= old || transport.max.CompareAndSwap(old, current) {
			break
		}
	}
	if transport.started != nil {
		select {
		case transport.started <- struct{}{}:
		default:
		}
	}
	if transport.release != nil {
		select {
		case <-transport.release:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
	}
	if transport.delay > 0 {
		timer := time.NewTimer(transport.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
	}
	return &http.Response{
		StatusCode:    http.StatusOK,
		Body:          io.NopCloser(bytes.NewReader(body)),
		Header:        make(http.Header),
		ContentLength: int64(len(body)),
		Request:       request,
	}, nil
}

func checkDigest(value byte) string {
	return "sha256:" + strings.Repeat(string(value), 64)
}

func releaseMetadata(t *testing.T, tag string, assets ...githubAssetMetadata) []byte {
	t.Helper()
	draft, prerelease := false, false
	contents, err := json.Marshal(githubReleaseMetadata{Draft: &draft, Prerelease: &prerelease, TagName: tag, Assets: assets})
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func testAsset(name string, size int64, digest string) githubAssetMetadata {
	return githubAssetMetadata{Name: name, Size: size, State: "uploaded", Digest: digest}
}

func newFixtureChecker(t *testing.T, responses map[string][]byte, installed InstalledSnapshot) (*Checker, *metadataFixtureTransport) {
	t.Helper()
	transport := &metadataFixtureTransport{responses: responses}
	checker := NewChecker(CheckerConfig{
		HTTPClient:        &http.Client{Transport: transport},
		InstalledSnapshot: installed,
	})
	return checker, transport
}

func transportCalls(transport *metadataFixtureTransport) []string {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]string(nil), transport.calls...)
}

func TestXrayCheckUsesFixedMetadataAndTypedVersionComparison(t *testing.T) {
	responses := map[string][]byte{
		xrayMetadataPath: releaseMetadata(t, "v1.10.0",
			testAsset(xrayCandidateAsset, 1234, checkDigest('a')),
		),
	}
	checker, transport := newFixtureChecker(t, responses, func() (Inventory, bool) {
		return Inventory{Xray: Component{State: StatePresent, Version: "1.9.9"}}, true
	})
	result, err := checker.Check(context.Background(), CheckRequest{Component: KindXray, Channel: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Eligible || result.MutationAvailable || result.ReasonCode != "supported-for-preview" {
		t.Fatalf("xray result = %+v", result)
	}
	if result.Candidate == nil || result.Candidate.Version != "1.10.0" || result.Candidate.AssetName != xrayCandidateAsset || result.Candidate.SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("xray candidate = %+v", result.Candidate)
	}
	if result.InstalledState != "update-available" {
		t.Fatalf("xray installed comparison = %q", result.InstalledState)
	}
	if got := transportCalls(transport); len(got) != 1 || got[0] != xrayMetadataPath {
		t.Fatalf("xray metadata calls = %v", got)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"browser_download_url", "download_url", "release body", "http://", "https://"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("xray result contains %q: %s", forbidden, encoded)
		}
	}
}

func TestXrayCheckFailsClosedForReleaseAndAssetProblems(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		wantCode string
	}{
		{name: "draft", body: func() []byte {
			draft, prerelease := true, false
			contents, _ := json.Marshal(githubReleaseMetadata{Draft: &draft, Prerelease: &prerelease, TagName: "1.2.3", Assets: []githubAssetMetadata{testAsset(xrayCandidateAsset, 1, checkDigest('a'))}})
			return contents
		}(), wantCode: "release-draft"},
		{name: "prerelease", body: func() []byte {
			draft, prerelease := false, true
			contents, _ := json.Marshal(githubReleaseMetadata{Draft: &draft, Prerelease: &prerelease, TagName: "1.2.3", Assets: []githubAssetMetadata{testAsset(xrayCandidateAsset, 1, checkDigest('a'))}})
			return contents
		}(), wantCode: "release-prerelease"},
		{name: "malformed version", body: releaseMetadata(t, "1.2", testAsset(xrayCandidateAsset, 1, checkDigest('a'))), wantCode: "version-invalid"},
		{name: "missing asset", body: releaseMetadata(t, "1.2.3", testAsset("Xray-linux-amd64.zip", 1, checkDigest('a'))), wantCode: "asset-wrong-architecture"},
		{name: "duplicate asset", body: releaseMetadata(t, "1.2.3", testAsset(xrayCandidateAsset, 1, checkDigest('a')), testAsset(xrayCandidateAsset, 2, checkDigest('b'))), wantCode: "asset-duplicate"},
		{name: "missing digest", body: releaseMetadata(t, "1.2.3", testAsset(xrayCandidateAsset, 1, "")), wantCode: "digest-unavailable"},
		{name: "bad digest", body: releaseMetadata(t, "1.2.3", testAsset(xrayCandidateAsset, 1, "sha256:not-a-digest")), wantCode: "digest-invalid"},
		{name: "zero size", body: releaseMetadata(t, "1.2.3", testAsset(xrayCandidateAsset, 0, checkDigest('a'))), wantCode: "asset-size-invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checker, _ := newFixtureChecker(t, map[string][]byte{xrayMetadataPath: test.body}, nil)
			result, err := checker.Check(context.Background(), CheckRequest{Component: KindXray, Channel: "stable"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Eligible || result.MutationAvailable || result.ReasonCode != test.wantCode {
				t.Fatalf("xray rejection = %+v", result)
			}
		})
	}

	t.Run("oversized metadata", func(t *testing.T) {
		checker, _ := newFixtureChecker(t, map[string][]byte{xrayMetadataPath: []byte(strings.Repeat("x", MaxMetadataResponseBytes+1))}, nil)
		result, err := checker.Check(context.Background(), CheckRequest{Component: KindXray, Channel: "stable"})
		if err != nil {
			t.Fatal(err)
		}
		if result.ReasonCode != "metadata-too-large" || result.Eligible {
			t.Fatalf("oversized metadata result = %+v", result)
		}
	})
}

func TestXKeenLatestIsInformationalWithoutProductTrust(t *testing.T) {
	checker, transport := newFixtureChecker(t, map[string][]byte{
		xkeenMetadataPath: releaseMetadata(t, "1.1.3", testAsset(xkeenCandidateAsset, 4567, "")),
	}, func() (Inventory, bool) {
		return Inventory{XKeen: Component{State: StatePresent, Version: "1.0.0"}}, true
	})
	result, err := checker.Check(context.Background(), CheckRequest{Component: KindXKeen, Channel: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidate == nil || result.Candidate.Version != "1.1.3" || result.Candidate.AssetName != xkeenCandidateAsset || result.Candidate.SizeBytes != 4567 {
		t.Fatalf("xkeen candidate = %+v", result.Candidate)
	}
	if result.Eligible || result.MutationAvailable || result.ReasonCode != "compatibility-catalog-required" || result.InstalledState != "changed" {
		t.Fatalf("xkeen result = %+v", result)
	}
	if got := transportCalls(transport); len(got) != 1 || got[0] != xkeenMetadataPath {
		t.Fatalf("xkeen metadata calls = %v", got)
	}
}

func TestGeodataCheckUsesFixedFiveSourceRequestsAndComparesDigests(t *testing.T) {
	digests := make(map[string]string, len(productGeodataCatalog))
	for index, entry := range productGeodataCatalog {
		digests[entry.ID] = strings.Repeat(string(rune('a'+index)), 64)
	}
	responses := make(map[string][]byte)
	responses[geodataMetadataPath(productGeodataCatalog[0])] = releaseMetadata(t, "2026-09-03",
		testAsset("geosite.dat", 10, "sha256:"+digests[productGeodataCatalog[0].ID]),
		testAsset("geoip.dat", 11, "sha256:"+digests[productGeodataCatalog[3].ID]),
	)
	for index, entry := range productGeodataCatalog {
		if index == 0 || index == 3 {
			continue
		}
		responses[geodataMetadataPath(entry)] = releaseMetadata(t, "2026-09-03", testAsset(entry.Asset, int64(index+20), "sha256:"+digests[entry.ID]))
	}
	installedItems := make([]GeodataItem, 0, len(productGeodataCatalog))
	for _, entry := range productGeodataCatalog {
		sha := digests[entry.ID]
		if entry.ID == "geoip-v2fly" {
			sha = strings.Repeat("f", 64)
		}
		installedItems = append(installedItems, GeodataItem{ID: entry.ID, State: StatePresent, Present: true, SHA256: sha})
	}
	checker, transport := newFixtureChecker(t, responses, func() (Inventory, bool) {
		return Inventory{Geodata: GeodataComponent{Items: installedItems}}, true
	})
	result, err := checker.Check(context.Background(), CheckRequest{Component: KindGeodata, Channel: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Eligible || result.MutationAvailable || result.ReasonCode != "supported-for-preview" || result.InstalledState != "changed" {
		t.Fatalf("geodata result = %+v", result)
	}
	if len(result.Items) != 6 {
		t.Fatalf("geodata item count = %d", len(result.Items))
	}
	for index, item := range result.Items {
		entry := productGeodataCatalog[index]
		if item.ID != entry.ID || item.SourceID != "github/"+entry.Repository || item.AssetName != entry.Asset || !item.Eligible || item.Generation != "2026-09-03" {
			t.Fatalf("geodata item %d = %+v", index, item)
		}
		wantState := "current"
		if entry.ID == "geoip-v2fly" {
			wantState = "changed"
		}
		if item.InstalledState != wantState {
			t.Fatalf("geodata item %s installed state = %q", item.ID, item.InstalledState)
		}
	}
	got := transportCalls(transport)
	if len(got) != 5 {
		t.Fatalf("geodata metadata call count = %d calls=%v", len(got), got)
	}
	if transport.max.Load() > MaxConcurrentMetadata {
		t.Fatalf("geodata metadata concurrency = %d", transport.max.Load())
	}
	for _, path := range got {
		if !isFixedMetadataPath(path) {
			t.Fatalf("non-catalog metadata path = %q", path)
		}
	}
}

func TestGeodataCheckFailsCompleteSetClosedAndIgnoresManualInventoryEntries(t *testing.T) {
	responses := make(map[string][]byte)
	for index, entry := range productGeodataCatalog {
		digest := checkDigest(byte('a' + index))
		asset := testAsset(entry.Asset, int64(index+1), digest)
		if entry.ID == "geosite-v2fly" {
			asset.Digest = "sha256:bad"
		}
		responses[geodataMetadataPath(entry)] = releaseMetadata(t, "opaque-release", asset)
	}
	manualPath := "/repos/manual-user/inferred/releases/latest"
	installed := func() (Inventory, bool) {
		return Inventory{Geodata: GeodataComponent{Items: []GeodataItem{{ID: "manual-geosite-custom.dat", Source: "manual/unsupported", Name: "custom.dat", State: StatePresent, SHA256: strings.Repeat("a", 64)}}}}, true
	}
	checker, transport := newFixtureChecker(t, responses, installed)
	result, err := checker.Check(context.Background(), CheckRequest{Component: KindGeodata, Channel: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Eligible || result.ReasonCode != "required-candidate-ineligible" {
		t.Fatalf("ineligible geodata result = %+v", result)
	}
	for _, item := range result.Items {
		if item.ID == "geosite-v2fly" && item.ReasonCode != "digest-invalid" {
			t.Fatalf("invalid geodata item = %+v", item)
		}
	}
	for _, path := range transportCalls(transport) {
		if path == manualPath {
			t.Fatalf("manual geodata path was requested: %q", path)
		}
	}
}

func TestCheckCacheCoalescesAndExpiresWithoutPersistentWrites(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	transport := &metadataFixtureTransport{
		responses: map[string][]byte{xrayMetadataPath: releaseMetadata(t, "1.2.3", testAsset(xrayCandidateAsset, 1, checkDigest('a')))},
		started:   started,
		release:   release,
	}
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	checker := NewChecker(CheckerConfig{
		HTTPClient: &http.Client{Transport: transport},
		CacheTTL:   time.Minute,
		Now:        func() time.Time { return now },
	})
	filesystemPath := t.TempDir()
	before := directoryEntries(t, filesystemPath)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := checker.Check(context.Background(), CheckRequest{Component: KindXray, Channel: "stable"})
			results <- err
		}()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("coalesced check did not start")
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if got := len(transportCalls(transport)); got != 1 {
		t.Fatalf("coalesced metadata calls = %d", got)
	}
	if _, err := checker.Check(context.Background(), CheckRequest{Component: KindXray, Channel: "stable"}); err != nil {
		t.Fatal(err)
	}
	if got := len(transportCalls(transport)); got != 1 {
		t.Fatalf("cached metadata calls = %d", got)
	}
	now = now.Add(2 * time.Minute)
	if _, err := checker.Check(context.Background(), CheckRequest{Component: KindXray, Channel: "stable"}); err != nil {
		t.Fatal(err)
	}
	if got := len(transportCalls(transport)); got != 2 {
		t.Fatalf("expired metadata calls = %d", got)
	}
	if after := directoryEntries(t, filesystemPath); len(before) != len(after) {
		t.Fatalf("check changed unrelated filesystem state: before=%v after=%v", before, after)
	}
}

func directoryEntries(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name())
	}
	return result
}

func TestMetadataTransportIsPinnedAndRejectsPrivateDNSAndRedirects(t *testing.T) {
	client := newMetadataHTTPClient(fixedCheckResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}})
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("metadata transport type = %T", client.Transport)
	}
	proxySet := transport.Proxy != nil
	tlsConfigured := transport.TLSClientConfig != nil
	minVersion := uint16(0)
	if tlsConfigured {
		minVersion = transport.TLSClientConfig.MinVersion
	}
	if proxySet || !tlsConfigured || minVersion < tls.VersionTLS12 {
		t.Fatalf("metadata transport security = proxySet=%t tlsConfigured=%t minVersion=%d", proxySet, tlsConfigured, minVersion)
	}
	if _, err := transport.DialContext(context.Background(), "tcp", metadataHost+":443"); err == nil {
		t.Fatal("private metadata DNS answer was accepted")
	}

	redirectCalls := atomic.Int32{}
	redirectClient := newMetadataClient(nil, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirectCalls.Add(1)
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://evil.example/next"}},
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Request:    request,
		}, nil
	})})
	_, err := redirectClient.fetch(context.Background(), xrayMetadataPath, newNetworkBudget())
	if !errors.Is(err, &metadataFailure{reason: "redirect-rejected"}) && metadataErrorReason(err) != "redirect-rejected" {
		t.Fatalf("redirect error = %v", err)
	}
	if redirectCalls.Load() != 1 {
		t.Fatalf("redirect was followed, calls=%d", redirectCalls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestMetadataBodyAndAggregateBudgetsAreBounded(t *testing.T) {
	tooLarge := &metadataClient{
		http: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", MaxMetadataResponseBytes+1))), ContentLength: -1, Request: request}, nil
		})},
		slots: make(chan struct{}, MaxConcurrentMetadata),
	}
	_, err := tooLarge.fetch(context.Background(), xrayMetadataPath, newNetworkBudget())
	if metadataErrorReason(err) != "metadata-too-large" {
		t.Fatalf("oversized body error = %v", err)
	}

	responses := make(map[string][]byte)
	perResponse := strings.Repeat("x", 1700000)
	for _, entry := range productGeodataCatalog {
		responses[geodataMetadataPath(entry)] = []byte(perResponse)
	}
	checker, _ := newFixtureChecker(t, responses, nil)
	result, err := checker.Check(context.Background(), CheckRequest{Component: KindGeodata, Channel: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Eligible || result.ReasonCode != "required-candidate-ineligible" {
		t.Fatalf("aggregate budget result = %+v", result)
	}
	foundBudget := false
	for _, item := range result.Items {
		if item.ReasonCode == "network-budget-exceeded" {
			foundBudget = true
		}
	}
	if !foundBudget {
		t.Fatalf("aggregate budget was not reported safely: %+v", result.Items)
	}
}

func TestCheckRejectsNonFixedRequestValues(t *testing.T) {
	for _, request := range []CheckRequest{
		{Component: KindPanel, Channel: "stable"},
		{Component: KindXray, Channel: "beta"},
		{Component: ComponentKind("unknown"), Channel: "stable"},
	} {
		if err := ValidateCheckRequest(request); !errors.Is(err, ErrInvalidCheckRequest) {
			t.Fatalf("request %+v error = %v", request, err)
		}
	}
	checker, transport := newFixtureChecker(t, map[string][]byte{}, nil)
	if _, err := checker.Check(context.Background(), CheckRequest{Component: KindPanel, Channel: "stable"}); !errors.Is(err, ErrInvalidCheckRequest) {
		t.Fatalf("invalid check error = %v", err)
	}
	if len(transportCalls(transport)) != 0 {
		t.Fatal("invalid request caused metadata traffic")
	}
	if _, err := checker.client.fetch(context.Background(), "/repos/attacker/repo/releases/latest", newNetworkBudget()); !errors.Is(err, ErrCheckUnavailable) {
		// Keep this assertion below the public API check so a server-owned path
		// cannot accidentally become a generic fetcher.
		t.Fatalf("non-fixed path error = %v", err)
	}
}
