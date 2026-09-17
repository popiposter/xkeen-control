package nodes

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
)

var (
	ErrPreviewExpired        = errors.New("preview is expired or invalid")
	ErrMissingAcceptance     = errors.New("preview contains missing subscription nodes")
	ErrNodeNotFound          = errors.New("node was not found")
	ErrOperationUnavailable  = errors.New("node operation unavailable")
	ErrPreviewStale          = errors.New("preview is stale")
	ErrSubscriptionRejected  = errors.New("subscription URL rejected")
	ErrSubscriptionFetch     = errors.New("subscription fetch failed")
	ErrSubscriptionContent   = errors.New("subscription content rejected")
	ErrSubscriptionDuplicate = errors.New("subscription contains duplicate node identity")
	ErrSubscriptionNode      = errors.New("subscription node rejected")
	ErrSubscriptionNotFound  = errors.New("subscription was not found")
	ErrSubscriptionDisabled  = errors.New("subscription is disabled")
	ErrPreviewCandidate      = errors.New("preview candidate is invalid")
	ErrSnapshotUnavailable   = errors.New("node registry snapshot unavailable")
	ErrBatchInvalid          = errors.New("node batch selection is invalid")
)

type Config struct {
	Store       Store
	LegacyPath  string
	Transaction Transaction
	// AuthorityLease is shared with backup export and appliance restore. The
	// Authority alias is retained as a readable compatibility option for
	// callers that use the shorter name.
	AuthorityLease *authority.Lease
	Authority      *authority.Lease
	Coordinator    interface {
		BeginApply(context.Context) (func(), error)
	}
	Fetcher     SubscriptionFetcher
	PreviewTTL  time.Duration
	MaxPreviews int
	Now         func() time.Time
}

const DefaultApplyGateWaitTimeout = 15 * time.Second

type Manager struct {
	store       Store
	legacyPath  string
	tx          Transaction
	fetcher     SubscriptionFetcher
	ttl         time.Duration
	maxPreviews int
	now         func() time.Time
	gateTimeout time.Duration
	coordinator interface {
		BeginApply(context.Context) (func(), error)
	}
	managedCoordinator interface {
		TryBeginManagedApply() (func(), error)
	}

	mu                    sync.Mutex
	authority             *authority.Lease
	previews              map[string]previewEntry
	autoRefreshStatusFunc func() map[string]AutoRefreshStatus
}

type previewEntry struct {
	Binding            string
	Registry           Registry
	BaseDigest         [sha256.Size]byte
	Operation          string
	Changes            []Change
	RequiresAcceptance bool
	Noop               bool
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

type Preview struct {
	Token              string    `json:"previewToken"`
	Operation          string    `json:"operation"`
	ExpiresAt          time.Time `json:"expiresAt"`
	Changes            []Change  `json:"changes"`
	RequiresAcceptance bool      `json:"requiresAcceptance"`
	Noop               bool      `json:"noop"`
}

type Change struct {
	Action      string `json:"action"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	OutboundTag string `json:"outboundTag"`
	SourceType  string `json:"sourceType"`
	Before      string `json:"before"`
	After       string `json:"after"`
}

type ApplyResult struct {
	Operation string       `json:"operation"`
	Nodes     []PublicNode `json:"nodes"`
	Changes   []Change     `json:"changes"`
}

func NewManager(config Config) *Manager {
	if config.PreviewTTL <= 0 {
		config.PreviewTTL = 7 * time.Minute
	}
	if config.MaxPreviews <= 0 {
		config.MaxPreviews = 8
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Fetcher == nil {
		config.Fetcher = HTTPSubscriptionFetcher{}
	}
	if config.Transaction.Store.Path == "" {
		config.Transaction.Store = config.Store
	}
	lease := config.AuthorityLease
	if lease == nil {
		lease = config.Authority
	}
	if lease == nil {
		lease = authority.NewLease()
	}
	manager := &Manager{
		store: config.Store, legacyPath: config.LegacyPath, tx: config.Transaction,
		fetcher: config.Fetcher, ttl: config.PreviewTTL, maxPreviews: config.MaxPreviews, now: config.Now,
		gateTimeout: DefaultApplyGateWaitTimeout,
		coordinator: config.Coordinator, authority: lease, previews: make(map[string]previewEntry),
	}
	if managed, ok := config.Coordinator.(interface {
		TryBeginManagedApply() (func(), error)
	}); ok {
		manager.managedCoordinator = managed
	}
	return manager
}

func (m *Manager) List() ([]PublicNode, error) {
	registry, err := m.current()
	if err != nil {
		return nil, err
	}
	return registry.PublicNodes(), nil
}

func (m *Manager) ListSubscriptions() ([]PublicSubscription, error) {
	registry, err := m.current()
	if err != nil {
		return nil, err
	}
	result := registry.PublicSubscriptions()
	m.mu.Lock()
	statusFunc := m.autoRefreshStatusFunc
	m.mu.Unlock()
	if statusFunc == nil {
		return result, nil
	}
	statuses := statusFunc()
	for index := range result {
		if status, ok := statuses[result[index].ID]; ok {
			copy := status
			result[index].AutoRefresh = &copy
		}
	}
	return result, nil
}

// SetAutoRefreshStatusProvider connects the RAM-only refresher projection to
// the existing safe subscription list. The provider is read-only from the
// Manager's perspective and must never return registry or provider secrets.
func (m *Manager) SetAutoRefreshStatusProvider(provider func() map[string]AutoRefreshStatus) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.autoRefreshStatusFunc = provider
	m.mu.Unlock()
}

// Snapshot returns a validated copy of the committed registry. It takes the
// same gate as Apply, but deliberately does not enter the runtime coordinator:
// backup reads must wait for an Apply to commit or roll back without cancelling
// benchmark or supervisor work. Unlike ordinary empty-registry node flows,
// backup export requires the authoritative file to exist and fails closed when
// it is missing.
func (m *Manager) Snapshot(ctx context.Context) (Registry, error) {
	if m == nil {
		return Registry{}, ErrSnapshotUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	gateTimeout := m.gateTimeout
	if gateTimeout <= 0 {
		gateTimeout = DefaultApplyGateWaitTimeout
	}
	release, err := m.authority.Acquire(ctx, gateTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return Registry{}, ctx.Err()
		}
		return Registry{}, ErrSnapshotUnavailable
	}
	defer release()

	registry, err := m.store.Load()
	if err != nil {
		return Registry{}, ErrSnapshotUnavailable
	}
	copy, err := cloneRegistry(registry)
	if err != nil || copy.Validate() != nil {
		return Registry{}, ErrSnapshotUnavailable
	}
	return copy, nil
}

// SnapshotUnderLease returns the same validated committed snapshot as
// Snapshot, but assumes the caller already owns the configured shared lease.
// It is used by backup export to snapshot appliance and nodes as one logical
// generation without attempting a nested acquisition.
func (m *Manager) SnapshotUnderLease(context.Context) (Registry, error) {
	if m == nil {
		return Registry{}, ErrSnapshotUnavailable
	}
	registry, err := m.store.Load()
	if err != nil {
		return Registry{}, ErrSnapshotUnavailable
	}
	copy, err := cloneRegistry(registry)
	if err != nil || copy.Validate() != nil {
		return Registry{}, ErrSnapshotUnavailable
	}
	return copy, nil
}

func (m *Manager) PreviewImport(binding, profiles string) (Preview, error) {
	registry, err := m.current()
	if err != nil {
		return Preview{}, err
	}
	before, err := cloneRegistry(registry)
	if err != nil {
		return Preview{}, err
	}
	parsed, err := ParseProfiles(profiles)
	if err != nil {
		return Preview{}, err
	}
	for _, profile := range parsed {
		node, err := NewNode(profile.VLESS, profile.Name, Source{Type: "manual"})
		if err != nil {
			return Preview{}, errors.New("profile rejected")
		}
		registry.Nodes = append(registry.Nodes, node)
	}
	return m.createPreview(binding, before, registry, "import", false)
}

func (m *Manager) PreviewReplace(binding, id, profile string) (Preview, error) {
	registry, err := m.current()
	if err != nil {
		return Preview{}, err
	}
	before, err := cloneRegistry(registry)
	if err != nil {
		return Preview{}, err
	}
	parsed, err := ParseProfiles(profile)
	if err != nil || len(parsed) != 1 {
		return Preview{}, errors.New("exactly one profile is required")
	}
	for index := range registry.Nodes {
		if registry.Nodes[index].ID != id {
			continue
		}
		node := registry.Nodes[index]
		node.VLESS = parsed[0].VLESS
		if parsed[0].Name != "" && parsed[0].Name != "Imported node" {
			node.Name = parsed[0].Name
		}
		node.SourceKey = nodeSourceKey(node.VLESS, node.Name, node.Source)
		node.Stale, node.Missing = false, false
		registry.Nodes[index] = node
		return m.createPreview(binding, before, registry, "replace", false)
	}
	return Preview{}, ErrNodeNotFound
}

func (m *Manager) PreviewState(binding, id string, enabled bool) (Preview, error) {
	registry, err := m.current()
	if err != nil {
		return Preview{}, err
	}
	before, err := cloneRegistry(registry)
	if err != nil {
		return Preview{}, err
	}
	for index := range registry.Nodes {
		if registry.Nodes[index].ID == id {
			if enabled && registry.Nodes[index].Source.Type == "subscription" {
				for _, subscription := range registry.Subscriptions {
					if subscription.ID == registry.Nodes[index].Source.SubscriptionID && !subscription.Enabled {
						return Preview{}, ErrSubscriptionDisabled
					}
				}
			}
			registry.Nodes[index].Enabled = enabled
			return m.createPreview(binding, before, registry, "enable-disable", false)
		}
	}
	return Preview{}, ErrNodeNotFound
}

// PreviewBatchState builds one candidate for the complete selected node set.
// Validation happens before the candidate is cloned or mutated so a malformed,
// duplicate, unknown, or subscription-inadmissible selection cannot create a
// partially valid preview.
func (m *Manager) PreviewBatchState(binding string, nodeIDs []string, enabled bool) (Preview, error) {
	registry, err := m.current()
	if err != nil {
		return Preview{}, err
	}
	selected, err := validateBatchNodeIDs(registry, nodeIDs)
	if err != nil {
		return Preview{}, err
	}
	if enabled && batchHasDisabledSubscription(registry, selected) {
		return Preview{}, ErrSubscriptionDisabled
	}
	before, err := cloneRegistry(registry)
	if err != nil {
		return Preview{}, err
	}
	for index := range registry.Nodes {
		if _, ok := selected[registry.Nodes[index].ID]; ok {
			registry.Nodes[index].Enabled = enabled
		}
	}
	operation := "batch-disable"
	if enabled {
		operation = "batch-enable"
	}
	return m.createPreview(binding, before, registry, operation, false)
}

func (m *Manager) PreviewSubscriptionState(binding, id string, enabled bool) (Preview, error) {
	registry, err := m.current()
	if err != nil {
		return Preview{}, err
	}
	before, err := cloneRegistry(registry)
	if err != nil {
		return Preview{}, err
	}
	found := false
	for index := range registry.Subscriptions {
		if registry.Subscriptions[index].ID != id {
			continue
		}
		registry.Subscriptions[index].Enabled = enabled
		found = true
		break
	}
	if !found {
		return Preview{}, ErrSubscriptionNotFound
	}
	for index := range registry.Nodes {
		if registry.Nodes[index].Source.Type == "subscription" && registry.Nodes[index].Source.SubscriptionID == id {
			registry.Nodes[index].Enabled = enabled
		}
	}
	return m.createPreview(binding, before, registry, "subscription-enable-disable", false)
}

func (m *Manager) PreviewSubscriptionRemove(binding, id string) (Preview, error) {
	registry, err := m.current()
	if err != nil {
		return Preview{}, err
	}
	before, err := cloneRegistry(registry)
	if err != nil {
		return Preview{}, err
	}
	found := false
	filteredSubscriptions := registry.Subscriptions[:0]
	for _, subscription := range registry.Subscriptions {
		if subscription.ID == id {
			found = true
			continue
		}
		filteredSubscriptions = append(filteredSubscriptions, subscription)
	}
	if !found {
		return Preview{}, ErrSubscriptionNotFound
	}
	registry.Subscriptions = filteredSubscriptions
	filteredNodes := registry.Nodes[:0]
	for _, node := range registry.Nodes {
		if node.Source.Type == "subscription" && node.Source.SubscriptionID == id {
			continue
		}
		filteredNodes = append(filteredNodes, node)
	}
	registry.Nodes = filteredNodes
	return m.createPreview(binding, before, registry, "subscription-remove", false)
}

func (m *Manager) PreviewRemove(binding, id string) (Preview, error) {
	registry, err := m.current()
	if err != nil {
		return Preview{}, err
	}
	before, err := cloneRegistry(registry)
	if err != nil {
		return Preview{}, err
	}
	for index := range registry.Nodes {
		if registry.Nodes[index].ID == id {
			registry.Nodes = append(registry.Nodes[:index], registry.Nodes[index+1:]...)
			return m.createPreview(binding, before, registry, "remove", false)
		}
	}
	return Preview{}, ErrNodeNotFound
}

// PreviewBatchRemove builds one candidate that removes exactly the selected
// nodes. Subscription records are intentionally retained; a later explicit
// refresh may reconcile their membership from a complete provider snapshot.
func (m *Manager) PreviewBatchRemove(binding string, nodeIDs []string) (Preview, error) {
	registry, err := m.current()
	if err != nil {
		return Preview{}, err
	}
	selected, err := validateBatchNodeIDs(registry, nodeIDs)
	if err != nil {
		return Preview{}, err
	}
	before, err := cloneRegistry(registry)
	if err != nil {
		return Preview{}, err
	}
	filtered := make([]Node, 0, len(registry.Nodes)-len(selected))
	for _, node := range registry.Nodes {
		if _, ok := selected[node.ID]; !ok {
			filtered = append(filtered, node)
		}
	}
	registry.Nodes = filtered
	return m.createPreview(binding, before, registry, "batch-remove", false)
}

func (m *Manager) PreviewRefresh(ctx context.Context, binding, subscriptionID, name, rawURL string) (Preview, error) {
	registry, err := m.current()
	if err != nil {
		return Preview{}, err
	}
	before, err := cloneRegistry(registry)
	if err != nil {
		return Preview{}, err
	}
	existingSubscription := -1
	if subscriptionID != "" {
		if !validSubscriptionID(subscriptionID) {
			return Preview{}, errors.New("invalid subscription identity")
		}
		for index := range registry.Subscriptions {
			if registry.Subscriptions[index].ID == subscriptionID {
				existingSubscription = index
				break
			}
		}
		if existingSubscription < 0 {
			return Preview{}, ErrSubscriptionNotFound
		}
		if rawURL == "" {
			rawURL = registry.Subscriptions[existingSubscription].URL
		}
		if name == "" {
			name = registry.Subscriptions[existingSubscription].Name
		}
	}
	if err := validateSubscriptionURL(rawURL); err != nil {
		return Preview{}, ErrSubscriptionRejected
	}
	body, err := m.fetcher.Fetch(ctx, rawURL)
	if err != nil {
		return Preview{}, ErrSubscriptionFetch
	}
	parsed, err := ParseSubscriptionBody(body)
	if err != nil {
		return Preview{}, ErrSubscriptionContent
	}
	if subscriptionID == "" {
		subscriptionID, err = randomSubscriptionID()
		if err != nil {
			return Preview{}, ErrOperationUnavailable
		}
	}
	if !validSubscriptionID(subscriptionID) {
		return Preview{}, errors.New("invalid subscription identity")
	}
	name = safeName(name, "Subscription")
	target := Subscription{ID: subscriptionID, Name: name, URL: rawURL, Enabled: true}
	for index := range registry.Subscriptions {
		if registry.Subscriptions[index].ID == subscriptionID {
			target.Enabled = registry.Subscriptions[index].Enabled
			break
		}
	}
	candidate, err := buildSubscriptionCandidate(before, target, parsed)
	if err != nil {
		return Preview{}, err
	}
	return m.createPreview(binding, before, candidate, "subscription-refresh", false)
}

// buildSubscriptionCandidate reconciles one complete parsed subscription
// snapshot against a committed registry. It is deliberately package-local and
// free of fetching, persistence, coordination, leases and goroutines so an
// automatic refresher can reuse the same candidate boundary later.
func buildSubscriptionCandidate(before Registry, target Subscription, parsed []ParsedProfile) (Registry, error) {
	if !validSubscriptionID(target.ID) || !validDisplay(target.Name, MaxNameLength) || target.URL == "" {
		return Registry{}, ErrPreviewCandidate
	}
	if len(parsed) == 0 || len(parsed) > MaxProfileCount {
		return Registry{}, ErrSubscriptionContent
	}
	candidate, err := cloneRegistry(before)
	if err != nil {
		return Registry{}, err
	}
	if err := before.Validate(); err != nil {
		return Registry{}, ErrPreviewCandidate
	}

	targetIndex := -1
	for index := range candidate.Subscriptions {
		if candidate.Subscriptions[index].ID == target.ID {
			targetIndex = index
			break
		}
	}
	if targetIndex >= 0 {
		candidate.Subscriptions[targetIndex] = target
	} else {
		candidate.Subscriptions = append(candidate.Subscriptions, target)
	}

	type incomingProfile struct {
		profile ParsedProfile
		key     string
	}
	incoming := make([]incomingProfile, 0, len(parsed))
	seen := make(map[string]struct{}, len(parsed))
	for _, profile := range parsed {
		if err := profile.VLESS.Validate(); err != nil {
			return Registry{}, ErrSubscriptionNode
		}
		name := safeName(profile.Name, "Imported node")
		key := subscriptionSourceKey(profile.VLESS, name)
		if _, exists := seen[key]; exists {
			return Registry{}, ErrSubscriptionDuplicate
		}
		seen[key] = struct{}{}
		incoming = append(incoming, incomingProfile{profile: ParsedProfile{VLESS: profile.VLESS, Name: name}, key: key})
	}
	sort.Slice(incoming, func(left, right int) bool { return incoming[left].key < incoming[right].key })

	currentByKey := make(map[string][]int)
	for index, node := range before.Nodes {
		if node.Source.Type == "subscription" && node.Source.SubscriptionID == target.ID {
			currentByKey[node.SourceKey] = append(currentByKey[node.SourceKey], index)
		}
	}
	updatedByKey := make(map[string]Node, len(incoming))
	newNodes := make([]Node, 0, len(incoming))
	for _, item := range incoming {
		matches := currentByKey[item.key]
		if len(matches) > 1 {
			return Registry{}, ErrSubscriptionDuplicate
		}
		if len(matches) == 1 {
			node := before.Nodes[matches[0]]
			node.VLESS = item.profile.VLESS
			node.SourceKey = item.key
			node.Enabled = target.Enabled
			node.Stale, node.Missing = false, false
			if item.profile.Name != "Imported node" {
				node.Name = item.profile.Name
			}
			updatedByKey[item.key] = node
			continue
		}
		node, err := NewNode(item.profile.VLESS, item.profile.Name, Source{Type: "subscription", SubscriptionID: target.ID})
		if err != nil {
			return Registry{}, ErrSubscriptionNode
		}
		node.Enabled = target.Enabled
		newNodes = append(newNodes, node)
	}

	filtered := make([]Node, 0, len(before.Nodes)+len(newNodes))
	for _, node := range before.Nodes {
		if node.Source.Type != "subscription" || node.Source.SubscriptionID != target.ID {
			filtered = append(filtered, node)
			continue
		}
		updated, exists := updatedByKey[node.SourceKey]
		if exists {
			filtered = append(filtered, updated)
		}
	}
	candidate.Nodes = append(filtered, newNodes...)
	if err := candidate.Validate(); err != nil {
		return Registry{}, ErrPreviewCandidate
	}
	return candidate, nil
}

func (m *Manager) Apply(ctx context.Context, binding, token string, acceptMissing bool) (ApplyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	releaseCoordinator := func() {}
	if m.coordinator != nil {
		var err error
		releaseCoordinator, err = m.coordinator.BeginApply(ctx)
		if err != nil {
			return ApplyResult{}, errors.New("runtime coordinator busy")
		}
		defer releaseCoordinator()
	}
	gateTimeout := m.gateTimeout
	if gateTimeout <= 0 {
		gateTimeout = DefaultApplyGateWaitTimeout
	}
	releaseAuthority, err := m.authority.Acquire(ctx, gateTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return ApplyResult{}, ctx.Err()
		}
		return ApplyResult{}, errors.New("node activation gate busy")
	}
	defer releaseAuthority()
	// The transaction budget starts only after the serialized apply slot is
	// acquired, preserving the full recovery reserve for persistent mutations.
	applyContext, cancelApply := context.WithTimeout(ctx, m.tx.totalTimeout())
	defer cancelApply()
	m.mu.Lock()
	entry, ok := m.previews[token]
	if !ok {
		m.mu.Unlock()
		return ApplyResult{}, ErrPreviewExpired
	}
	if entry.Binding != binding {
		m.mu.Unlock()
		return ApplyResult{}, ErrPreviewExpired
	}
	if !m.now().Before(entry.ExpiresAt) {
		if ok {
			delete(m.previews, token)
		}
		m.mu.Unlock()
		return ApplyResult{}, ErrPreviewExpired
	}
	if entry.RequiresAcceptance && !acceptMissing {
		m.mu.Unlock()
		return ApplyResult{}, ErrMissingAcceptance
	}
	current, err := m.current()
	if err != nil {
		m.mu.Unlock()
		return ApplyResult{}, ErrPreviewStale
	}
	if registryDigest(current) != entry.BaseDigest {
		delete(m.previews, token)
		m.mu.Unlock()
		return ApplyResult{}, ErrPreviewStale
	}
	delete(m.previews, token)
	m.mu.Unlock()
	if entry.Noop {
		return ApplyResult{Operation: entry.Operation, Nodes: entry.Registry.PublicNodes(), Changes: entry.Changes}, nil
	}
	if err := m.tx.Apply(applyContext, entry.Registry); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Operation: entry.Operation, Nodes: entry.Registry.PublicNodes(), Changes: entry.Changes}, nil
}

func (m *Manager) Invalidate(binding string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for token, entry := range m.previews {
		if entry.Binding == binding {
			delete(m.previews, token)
		}
	}
}

func (m *Manager) Cancel(binding, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.previews[token]; ok && entry.Binding == binding {
		delete(m.previews, token)
	}
}

func (m *Manager) MigrateLegacy(ctx context.Context) error {
	if m.legacyPath == "" {
		return errors.New("legacy path is not configured")
	}
	if _, err := m.store.Load(); err == nil {
		return errors.New("registry already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("existing registry is invalid")
	}
	contents, err := ReadBoundedFile(m.legacyPath, MaxLegacyDocument)
	if err != nil {
		return errors.New("legacy outbound file is unavailable")
	}
	registry, err := MigrateLegacy(contents)
	if err != nil {
		return err
	}
	return m.tx.Apply(ctx, registry)
}

func (m *Manager) RenderStored() ([]byte, error) {
	registry, err := m.current()
	if err != nil {
		return nil, err
	}
	return Render(registry)
}

func (m *Manager) ValidateStored() error {
	registry, err := m.current()
	if err != nil {
		return err
	}
	_, err = Render(registry)
	return err
}

func (m *Manager) current() (Registry, error) {
	registry, err := m.store.Load()
	if err == nil {
		return registry, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return NewRegistry(), nil
	}
	return Registry{}, errors.New("registry unavailable")
}

func (m *Manager) createPreview(binding string, before, registry Registry, operation string, requiresAcceptance bool) (Preview, error) {
	if binding == "" {
		return Preview{}, errors.New("session binding is required")
	}
	if err := registry.Validate(); err != nil {
		return Preview{}, ErrPreviewCandidate
	}
	changes := diffChanges(before, registry, operation)
	token, err := randomToken()
	if err != nil {
		return Preview{}, ErrOperationUnavailable
	}
	expires := m.now().Add(m.ttl)
	noop := sameRegistry(before, registry)
	created := m.now()
	entry := previewEntry{Binding: binding, Registry: registry, BaseDigest: registryDigest(before), Operation: operation, Changes: changes, RequiresAcceptance: requiresAcceptance, Noop: noop, CreatedAt: created, ExpiresAt: expires}
	m.mu.Lock()
	m.purgeExpiredLocked(created)
	for oldToken, old := range m.previews {
		if old.Binding == binding {
			delete(m.previews, oldToken)
		}
	}
	for len(m.previews) >= m.maxPreviews {
		m.evictOldestLocked()
	}
	m.previews[token] = entry
	m.mu.Unlock()
	return Preview{Token: token, Operation: operation, ExpiresAt: expires, Changes: changes, RequiresAcceptance: requiresAcceptance, Noop: noop}, nil
}

func validateBatchNodeIDs(registry Registry, nodeIDs []string) (map[string]struct{}, error) {
	if len(nodeIDs) < 1 || len(nodeIDs) > MaxNodes {
		return nil, ErrBatchInvalid
	}
	selected := make(map[string]struct{}, len(nodeIDs))
	available := make(map[string]struct{}, len(registry.Nodes))
	for _, node := range registry.Nodes {
		available[node.ID] = struct{}{}
	}
	for _, id := range nodeIDs {
		if !validIdentifier(id) {
			return nil, ErrBatchInvalid
		}
		if _, exists := selected[id]; exists {
			return nil, ErrBatchInvalid
		}
		if _, exists := available[id]; !exists {
			return nil, ErrBatchInvalid
		}
		selected[id] = struct{}{}
	}
	return selected, nil
}

func batchHasDisabledSubscription(registry Registry, selected map[string]struct{}) bool {
	subscriptions := make(map[string]bool, len(registry.Subscriptions))
	for _, subscription := range registry.Subscriptions {
		subscriptions[subscription.ID] = subscription.Enabled
	}
	for _, node := range registry.Nodes {
		if _, ok := selected[node.ID]; !ok || node.Source.Type != "subscription" {
			continue
		}
		if !subscriptions[node.Source.SubscriptionID] {
			return true
		}
	}
	return false
}

func (m *Manager) purgeExpiredLocked(now time.Time) {
	for token, entry := range m.previews {
		if !now.Before(entry.ExpiresAt) {
			delete(m.previews, token)
		}
	}
}

func (m *Manager) evictOldestLocked() {
	oldestToken := ""
	var oldest time.Time
	for token, entry := range m.previews {
		if oldestToken == "" || entry.CreatedAt.Before(oldest) || (entry.CreatedAt.Equal(oldest) && token < oldestToken) {
			oldestToken, oldest = token, entry.CreatedAt
		}
	}
	if oldestToken != "" {
		delete(m.previews, oldestToken)
	}
}

func registryDigest(registry Registry) [sha256.Size]byte {
	encoded, err := json.Marshal(registry)
	if err != nil {
		return [sha256.Size]byte{}
	}
	return sha256.Sum256(encoded)
}

func sameRegistry(left, right Registry) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func diffChanges(before, after Registry, operation string) []Change {
	result := make([]Change, 0, len(after.Nodes)+len(before.Nodes))
	previous := make(map[string]Node, len(before.Nodes))
	for _, node := range before.Nodes {
		previous[node.ID] = node
	}
	seen := make(map[string]struct{}, len(after.Nodes))
	for _, node := range after.SortedNodes() {
		beforeNode, exists := previous[node.ID]
		if exists && sameNode(beforeNode, node) {
			seen[node.ID] = struct{}{}
			continue
		}
		beforeState := "absent"
		if exists {
			beforeState = nodeState(beforeNode)
		}
		result = append(result, Change{Action: operation, ID: node.ID, Name: node.Name, OutboundTag: node.OutboundTag, SourceType: node.Source.Type, Before: beforeState, After: nodeState(node)})
		seen[node.ID] = struct{}{}
	}
	for _, node := range before.SortedNodes() {
		if _, exists := seen[node.ID]; !exists {
			result = append(result, Change{Action: operation, ID: node.ID, Name: node.Name, OutboundTag: node.OutboundTag, SourceType: node.Source.Type, Before: nodeState(node), After: "removed"})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].OutboundTag < result[j].OutboundTag })
	return result
}

func sameNode(left, right Node) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func nodeState(node Node) string {
	if node.Stale || node.Missing {
		return "stale/missing"
	}
	if node.Enabled {
		return "enabled"
	}
	return "disabled"
}

func randomSubscriptionID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "sub-" + hex.EncodeToString(raw[:]), nil
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
