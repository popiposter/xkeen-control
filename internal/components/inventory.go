package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/appliance"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
)

const (
	SchemaVersion = 1

	DefaultXrayBinary             = "/opt/sbin/xray"
	DefaultXkeenBinary            = "/opt/sbin/xkeen"
	DefaultXkeenModuleDir         = "/opt/sbin/.xkeen"
	DefaultXkeenModuleImport      = "/opt/sbin/.xkeen/import.sh"
	DefaultXkeenRuntimeInit       = "/opt/etc/init.d/S05xkeen"
	DefaultXkeenLegacyRuntimeInit = "/opt/etc/init.d/S24xray"
	DefaultXkeenConfig            = "/opt/etc/xkeen/xkeen.json"
	DefaultXkeenPackageMetadata   = "/opt/lib/opkg/info/xkeen.control"
	DefaultGeodataDir             = "/opt/etc/xray/dat"
	DefaultAppliancePath          = "/opt/etc/xkeen-control/config/appliance.json"
	DefaultRoutingPath            = "/opt/etc/xray/configs/05_routing.json"
	DefaultDNSPath                = "/opt/etc/xray/configs/02_dns.json"
	DefaultKeeneticOSVersionPath  = "/etc/version"
	DefaultEntwareReleasePath     = "/opt/etc/entware_release"
	DefaultEntwareBinary          = "/opt/bin/opkg"
	DefaultInventoryTimeout       = 2 * time.Second
	DefaultXrayProbeTimeout       = 1 * time.Second
	MaxXrayProbeOutput            = 64 << 10
	MaxSignalBytes                = 4 << 10
	MaxPolicyBytes                = 2 << 20
	MaxGeodataFileBytes           = 64 << 20
	MaxInventoryReadBytes         = 256 << 20
	MaxGeodataItems               = 32
	DefaultXkeenGenerationMarker  = DefaultXKeenMarkerPath
)

var (
	errXrayVersionUnparseable = errors.New("xray version output is unparseable")
	errXrayOutputTooLarge     = errors.New("xray version output exceeds the limit")
	errInventoryBudget        = errors.New("component inventory budget exceeded")
	errFileTooLarge           = errors.New("component inventory file exceeds the limit")
	errPolicyShape            = errors.New("component policy shape is invalid")

	xrayVersionPattern     = regexp.MustCompile(`(?m)^\s*Xray\s+v?([0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)\s+\(`)
	xrayInlineBuildPattern = regexp.MustCompile(`(?m)^\s*Xray\s+v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?\s+\([^()\r\n]{1,128}\)\s+[A-Za-z0-9._+-]{1,128}\s+\(go[0-9]+(?:\.[0-9]+){1,3}\s+linux/([A-Za-z0-9_]+)\)\s*$`)
	xrayBuildPattern       = regexp.MustCompile(`(?m)^\s*go[0-9]+(?:\.[0-9]+){1,3}\s+linux/([A-Za-z0-9_]+)\s*$`)
	packageVersionPattern  = regexp.MustCompile(`(?m)^Version:[ \t]+(v?[0-9]+(?:\.[0-9]+){2,3}(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)[ \t]*$`)
	platformVersionPattern = regexp.MustCompile(`^v?([0-9]+(?:\.[0-9]+){1,3}(?:[-+][0-9A-Za-z.-]+)?)$`)
	logicalFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	logicalListPattern     = regexp.MustCompile(`^[A-Za-z0-9._!-]{1,128}$`)
)

// ComponentKind is intentionally a closed set. New lifecycle classes must be
// added as an explicit API change rather than accepted from the browser.
type ComponentKind string

const (
	KindPanel      ComponentKind = "panel"
	KindXKeen      ComponentKind = "xkeen"
	KindXray       ComponentKind = "xray"
	KindGeodata    ComponentKind = "geodata"
	KindKeeneticOS ComponentKind = "keeneticos"
	KindEntware    ComponentKind = "entware"
)

type PresenceState string

const (
	StatePresent PresenceState = "present"
	StateMissing PresenceState = "missing"
	StateUnknown PresenceState = "unknown"
)

type Capability string

const (
	CapabilitySupported     Capability = "supported"
	CapabilityInformational Capability = "informational"
	CapabilityUnsupported   Capability = "unsupported"
)

// Component is the common safe projection for a fixed component class. It has
// no path, command, raw metadata or update/candidate fields.
type Component struct {
	Kind           ComponentKind `json:"kind"`
	State          PresenceState `json:"state"`
	Present        bool          `json:"present"`
	Version        string        `json:"version,omitempty"`
	VersionUnknown bool          `json:"versionUnknown"`
	Architecture   string        `json:"architecture,omitempty"`
	SourceCommit   string        `json:"sourceCommit,omitempty"`
	Channel        string        `json:"channel,omitempty"`
	Capability     Capability    `json:"capability"`
	ReasonCode     string        `json:"reasonCode,omitempty"`
}

type GeodataComponent struct {
	Component
	Items []GeodataItem `json:"items"`
}

type GeodataItem struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Source     string        `json:"source"`
	State      PresenceState `json:"state"`
	Present    bool          `json:"present"`
	SizeBytes  int64         `json:"sizeBytes,omitempty"`
	MTime      string        `json:"mtime,omitempty"`
	SHA256     string        `json:"sha256,omitempty"`
	ReasonCode string        `json:"reasonCode,omitempty"`
}

// Inventory is the versioned, fixed-shape Phase A response.
type Inventory struct {
	SchemaVersion int              `json:"schemaVersion"`
	Panel         Component        `json:"panel"`
	XKeen         Component        `json:"xkeen"`
	Xray          Component        `json:"xray"`
	Geodata       GeodataComponent `json:"geodata"`
	KeeneticOS    Component        `json:"keeneticos"`
	Entware       Component        `json:"entware"`
}

// ReadOnlyService is the narrow read-only dependency consumed by the HTTP layer.
type ReadOnlyService interface {
	Snapshot(context.Context) Inventory
}

// XrayVersionProbe is purpose-specific by design. It is not a general command
// runner and cannot be selected or parameterized by an HTTP request.
type XrayVersionProbe interface {
	ProbeXrayVersion(context.Context) XrayVersionResult
}

type XrayVersionResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

type XrayVersionSignal struct {
	Version      string
	Architecture string
}

type Config struct {
	Panel buildinfo.Info

	XrayBinary       string
	XrayVersionProbe XrayVersionProbe
	XrayProbeTimeout time.Duration

	XkeenBinary            string
	XkeenModuleDir         string
	XkeenModuleImport      string
	XkeenRuntimeInit       string
	XkeenLegacyRuntimeInit string
	XkeenSiblingModule     string
	XkeenInstallHelper     string
	XkeenConfig            string
	XkeenPackageMetadata   string
	XkeenGenerationMarker  string

	GeodataDir    string
	AppliancePath string
	RoutingPath   string
	DNSPath       string

	KeeneticOSVersionPath string
	EntwareReleasePath    string
	EntwareBinary         string

	InventoryTimeout time.Duration
}

type Service struct {
	config           Config
	xrayVersionProbe XrayVersionProbe
	latestMu         sync.RWMutex
	latest           Inventory
	hasLatest        bool
}

// NewService constructs a side-effect-free inventory service. It does not
// create directories, write cache files or validate any path at construction.
func NewService(config Config) *Service {
	if config.Panel.Product == "" {
		config.Panel = buildinfo.Current()
	}
	if config.XrayBinary == "" {
		config.XrayBinary = DefaultXrayBinary
	}
	if config.XkeenBinary == "" {
		config.XkeenBinary = DefaultXkeenBinary
	}
	if config.XkeenModuleDir == "" {
		config.XkeenModuleDir = DefaultXkeenModuleDir
	}
	if config.XkeenModuleImport == "" {
		config.XkeenModuleImport = DefaultXkeenModuleImport
	}
	if config.XkeenRuntimeInit == "" {
		config.XkeenRuntimeInit = DefaultXkeenRuntimeInit
	}
	if config.XkeenLegacyRuntimeInit == "" {
		config.XkeenLegacyRuntimeInit = DefaultXkeenLegacyRuntimeInit
	}
	if config.XkeenSiblingModule == "" {
		config.XkeenSiblingModule = filepath.Join(filepath.Dir(config.XkeenModuleDir), "_xkeen")
	}
	if config.XkeenInstallHelper == "" {
		config.XkeenInstallHelper = "/opt/root/install.sh"
	}
	if config.XkeenConfig == "" {
		config.XkeenConfig = DefaultXkeenConfig
	}
	if config.XkeenPackageMetadata == "" {
		config.XkeenPackageMetadata = DefaultXkeenPackageMetadata
	}
	if config.XkeenGenerationMarker == "" {
		config.XkeenGenerationMarker = DefaultXkeenGenerationMarker
	}
	if config.GeodataDir == "" {
		config.GeodataDir = DefaultGeodataDir
	}
	if config.AppliancePath == "" {
		config.AppliancePath = DefaultAppliancePath
	}
	if config.RoutingPath == "" {
		config.RoutingPath = DefaultRoutingPath
	}
	if config.DNSPath == "" {
		config.DNSPath = DefaultDNSPath
	}
	if config.KeeneticOSVersionPath == "" {
		config.KeeneticOSVersionPath = DefaultKeeneticOSVersionPath
	}
	if config.EntwareReleasePath == "" {
		config.EntwareReleasePath = DefaultEntwareReleasePath
	}
	if config.EntwareBinary == "" {
		config.EntwareBinary = DefaultEntwareBinary
	}
	if config.InventoryTimeout <= 0 {
		config.InventoryTimeout = DefaultInventoryTimeout
	}
	if config.XrayProbeTimeout <= 0 {
		config.XrayProbeTimeout = DefaultXrayProbeTimeout
	}
	probe := config.XrayVersionProbe
	if probe == nil {
		probe = commandXrayVersionProbe{binary: config.XrayBinary}
	}
	return &Service{config: config, xrayVersionProbe: probe}
}

// New is a short alias for callers that use the other internal service
// constructors' naming convention.
func New(config Config) *Service { return NewService(config) }

func (s *Service) Snapshot(parent context.Context) Inventory {
	if parent == nil {
		parent = context.Background()
	}
	if s == nil {
		return unavailableInventory()
	}
	ctx, cancel := context.WithTimeout(parent, s.config.InventoryTimeout)
	defer cancel()
	budget := &readBudget{remaining: MaxInventoryReadBytes}
	result := Inventory{
		SchemaVersion: SchemaVersion,
		Panel:         inventoryPanel(s.config.Panel),
		XKeen:         s.inventoryXKeen(ctx, budget),
		Xray:          s.inventoryXray(ctx),
		Geodata:       s.inventoryGeodata(ctx, budget),
		KeeneticOS:    s.inventoryKeeneticOS(ctx, budget),
		Entware:       s.inventoryEntware(ctx, budget),
	}
	s.latestMu.Lock()
	s.latest = cloneInventory(result)
	s.hasLatest = true
	s.latestMu.Unlock()
	return result
}

// Latest returns the most recently collected inventory without collecting any
// new signals. Component Check uses this RAM-only snapshot so a network check
// never invokes Xray, XKeen or opkg through the inventory path.
func (s *Service) Latest() (Inventory, bool) {
	if s == nil {
		return Inventory{}, false
	}
	s.latestMu.RLock()
	defer s.latestMu.RUnlock()
	if !s.hasLatest {
		return Inventory{}, false
	}
	return cloneInventory(s.latest), true
}

// Inventory is an explicit spelling for callers that prefer domain language
// over the Snapshot convention used by runtime projections.
func (s *Service) Inventory(ctx context.Context) Inventory { return s.Snapshot(ctx) }

func unavailableInventory() Inventory {
	unknown := func(kind ComponentKind) Component {
		return Component{Kind: kind, State: StateUnknown, VersionUnknown: true, Capability: CapabilityUnsupported, ReasonCode: "inventory-unavailable"}
	}
	return Inventory{
		SchemaVersion: SchemaVersion,
		Panel:         unknown(KindPanel),
		XKeen:         unknown(KindXKeen),
		Xray:          unknown(KindXray),
		Geodata:       GeodataComponent{Component: unknown(KindGeodata), Items: []GeodataItem{}},
		KeeneticOS:    unknown(KindKeeneticOS),
		Entware:       unknown(KindEntware),
	}
}

func cloneInventory(value Inventory) Inventory {
	clone := value
	if value.Geodata.Items != nil {
		clone.Geodata.Items = append([]GeodataItem(nil), value.Geodata.Items...)
	}
	return clone
}

func inventoryPanel(info buildinfo.Info) Component {
	component := Component{
		Kind:           KindPanel,
		State:          StatePresent,
		Present:        true,
		Version:        strings.TrimSpace(info.Version),
		VersionUnknown: strings.TrimSpace(info.Version) == "",
		SourceCommit:   strings.TrimSpace(info.SourceCommit),
		Channel:        strings.TrimSpace(info.Channel),
		Capability:     CapabilityInformational,
		ReasonCode:     "panel-update-owned",
	}
	if component.VersionUnknown {
		component.ReasonCode = "version-unavailable"
	}
	return component
}

func (s *Service) inventoryXray(ctx context.Context) Component {
	component := Component{Kind: KindXray, VersionUnknown: true, Capability: CapabilityUnsupported}
	path := inspectPath(s.config.XrayBinary, pathExecutable)
	if path.state == StateMissing {
		component.State = StateMissing
		component.ReasonCode = "not-installed"
		return component
	}
	component.State = path.state
	component.Present = path.present
	if path.state != StatePresent || !path.valid {
		component.ReasonCode = path.reason
		return component
	}

	probeContext, cancel := context.WithTimeout(ctx, s.config.XrayProbeTimeout)
	defer cancel()
	if s.xrayVersionProbe == nil {
		component.ReasonCode = "version-probe-unavailable"
		return component
	}
	result := s.xrayVersionProbe.ProbeXrayVersion(probeContext)
	if len(result.Stdout)+len(result.Stderr) > MaxXrayProbeOutput {
		component.ReasonCode = "version-output-too-large"
		return component
	}
	if probeContext.Err() != nil {
		component.ReasonCode = "version-probe-timeout"
		return component
	}
	if result.ExitCode != 0 || result.Err != nil {
		component.ReasonCode = "version-probe-failed"
		return component
	}
	signal, err := ParseXrayVersionOutput(result.Stdout, result.Stderr)
	if err != nil {
		if errors.Is(err, errXrayOutputTooLarge) {
			component.ReasonCode = "version-output-too-large"
		} else {
			component.ReasonCode = "version-unparseable"
		}
		return component
	}
	component.Version = signal.Version
	component.VersionUnknown = false
	component.Architecture = signal.Architecture
	if signal.Architecture != "arm64" {
		component.ReasonCode = "architecture-unsupported"
		return component
	}
	component.Capability = CapabilitySupported
	return component
}

// ParseXrayVersionOutput accepts only tested Xray version statement shapes.
// Current official releases carry Go/OS/arch on the first line; the bounded
// standalone build line remains accepted for older supported layouts.
func ParseXrayVersionOutput(stdout, stderr []byte) (XrayVersionSignal, error) {
	if len(stdout)+len(stderr) > MaxXrayProbeOutput {
		return XrayVersionSignal{}, errXrayOutputTooLarge
	}
	output := make([]byte, 0, len(stdout)+len(stderr)+1)
	output = append(output, stdout...)
	output = append(output, '\n')
	output = append(output, stderr...)
	versions := xrayVersionPattern.FindAllSubmatch(output, -1)
	if len(versions) != 1 {
		return XrayVersionSignal{}, errXrayVersionUnparseable
	}
	architectures := xrayInlineBuildPattern.FindAllSubmatch(output, -1)
	if len(architectures) == 0 {
		architectures = xrayBuildPattern.FindAllSubmatch(output, -1)
	}
	if len(architectures) != 1 {
		return XrayVersionSignal{}, errXrayVersionUnparseable
	}
	return XrayVersionSignal{
		Version:      string(versions[0][1]),
		Architecture: string(architectures[0][1]),
	}, nil
}

func (s *Service) inventoryXKeen(ctx context.Context, budget *readBudget) Component {
	component := Component{Kind: KindXKeen, VersionUnknown: true, Capability: CapabilityUnsupported}
	coreChecks := []pathCheck{
		inspectPath(s.config.XkeenBinary, pathExecutable),
		inspectPath(s.config.XkeenModuleDir, pathDirectory),
		inspectPath(s.config.XkeenModuleImport, pathRegular),
		inspectPath(s.config.XkeenConfig, pathRegular),
	}
	currentInit := inspectPath(s.config.XkeenRuntimeInit, pathExecutable)
	legacyInit := inspectPath(s.config.XkeenLegacyRuntimeInit, pathExecutable)
	forbiddenArtifacts := []pathCheck{
		inspectPath(s.config.XkeenSiblingModule, pathRegular),
		inspectPath(s.config.XkeenInstallHelper, pathRegular),
	}
	checks := append(append([]pathCheck{}, coreChecks...), currentInit)
	anyPresent := false
	anyUnknown := false
	allValid := true
	firstReason := ""
	for _, check := range append(append([]pathCheck{}, checks...), legacyInit) {
		anyPresent = anyPresent || check.present
		anyUnknown = anyUnknown || check.state == StateUnknown
	}
	for _, check := range forbiddenArtifacts {
		anyPresent = anyPresent || check.present
		anyUnknown = anyUnknown || check.state == StateUnknown
	}
	for _, check := range checks {
		if check.state != StatePresent || !check.valid {
			allValid = false
			if firstReason == "" {
				firstReason = check.reason
			}
		}
	}
	component.Present = anyPresent
	switch {
	case anyUnknown:
		component.State = StateUnknown
	case anyPresent:
		component.State = StatePresent
	default:
		component.State = StateMissing
	}
	if !anyPresent && !anyUnknown {
		component.ReasonCode = "not-installed"
		return component
	}
	if currentInit.state == StatePresent {
		switch {
		case legacyInit.state == StateUnknown:
			component.State = StateUnknown
			component.ReasonCode = "legacy-layout-unavailable"
			return component
		case legacyInit.present:
			// A surviving legacy init alongside S05xkeen is mixed lifecycle
			// ownership, not a supported dev layout. Do not expose a usable
			// channel or version until migration is handled separately.
			component.State = StatePresent
			component.Present = true
			component.ReasonCode = "mixed-layout"
			return component
		}
	}
	// S24xray is retained only as an explicit legacy/migration signal. It is
	// never accepted as the current XKeen lifecycle dependency and cannot
	// make the component supported or expose a dev candidate.
	if legacyInit.present && currentInit.state != StatePresent {
		if currentInit.state == StateUnknown {
			component.State = StateUnknown
			component.ReasonCode = "current-layout-unavailable"
			return component
		}
		component.State = StatePresent
		component.Present = true
		if legacyInit.state == StatePresent && legacyInit.valid {
			coreValid := true
			for _, check := range coreChecks {
				if check.state != StatePresent || !check.valid {
					coreValid = false
					if firstReason == "" {
						firstReason = check.reason
					}
				}
			}
			if coreValid {
				component.ReasonCode = "legacy-layout"
				return component
			}
		}
		component.ReasonCode = "legacy-layout-incomplete"
		return component
	}
	for _, check := range forbiddenArtifacts {
		if check.state == StateUnknown {
			component.State = StateUnknown
			component.ReasonCode = "lifecycle-boundary-unavailable"
			return component
		}
		if check.present {
			component.State = StatePresent
			component.Present = true
			component.ReasonCode = "lifecycle-incompatible"
			return component
		}
	}
	if !allValid {
		if firstReason == "" {
			firstReason = "layout-incomplete"
		}
		component.ReasonCode = firstReason
		return component
	}
	component.Channel = "dev"

	marker, markerContents, _, markerErr := readXKeenMarker(s.config.XkeenGenerationMarker)
	if markerErr == nil {
		generation, generationErr := readXKeenGeneration(s.config.XkeenBinary, s.config.XkeenModuleDir)
		if generationErr != nil || !strings.EqualFold(generation.Generation, marker.GenerationSHA256) {
			component.ReasonCode = "managed-drift"
			return component
		}
		_ = markerContents
		component.Version = marker.Version
		component.VersionUnknown = false
		component.SourceCommit = marker.BuildCommitSHA
		component.Channel = marker.Channel
		component.Capability = CapabilitySupported
		component.ReasonCode = "managed-marker-qualified"
		return component
	}
	if !errors.Is(markerErr, os.ErrNotExist) {
		// A present managed marker is authoritative. Never fall back to stale
		// opkg metadata after marker validation fails.
		if _, markerPathErr := os.Lstat(s.config.XkeenGenerationMarker); markerPathErr == nil {
			component.ReasonCode = "managed-marker-invalid"
			return component
		}
		component.ReasonCode = "version-unavailable"
		return component
	}

	metadata, metadataCheck := readRegularBytes(ctx, budget, s.config.XkeenPackageMetadata, MaxSignalBytes)
	if metadataCheck.state != StatePresent || !metadataCheck.valid {
		component.ReasonCode = "version-unavailable"
		return component
	}
	version := parsePackageVersion(metadata)
	if version == "" {
		component.ReasonCode = "version-unparseable"
		return component
	}
	component.Version = version
	component.VersionUnknown = false
	component.Capability = CapabilitySupported
	return component
}

func parsePackageVersion(raw []byte) string {
	matches := packageVersionPattern.FindAllSubmatch(raw, -1)
	if len(matches) != 1 {
		return ""
	}
	return strings.TrimPrefix(string(matches[0][1]), "v")
}

type catalogEntry struct {
	ID         string
	Kind       string
	Name       string
	Repository string
	Asset      string
}

var productGeodataCatalog = []catalogEntry{
	{ID: "geosite-refilter", Kind: "geosite", Name: "geosite_refilter.dat", Repository: "1andrevich/Re-filter-lists", Asset: "geosite.dat"},
	{ID: "geosite-v2fly", Kind: "geosite", Name: "geosite_v2fly.dat", Repository: "v2fly/domain-list-community", Asset: "dlc.dat"},
	{ID: "geosite-zkeen", Kind: "geosite", Name: "geosite_zkeen.dat", Repository: "jameszeroX/zkeen-domains", Asset: "zkeen.dat"},
	{ID: "geoip-refilter", Kind: "geoip", Name: "geoip_refilter.dat", Repository: "1andrevich/Re-filter-lists", Asset: "geoip.dat"},
	{ID: "geoip-v2fly", Kind: "geoip", Name: "geoip_v2fly.dat", Repository: "Loyalsoldier/v2ray-rules-dat", Asset: "geoip.dat"},
	{ID: "geoip-zkeenip", Kind: "geoip", Name: "geoip_zkeenip.dat", Repository: "jameszeroX/zkeen-ip", Asset: "zkeenip.dat"},
}

type policyExpression struct {
	kind  string
	value string
}

type policyExpressionResult struct {
	expressions []policyExpression
	reason      string
}

type geodataRequirement struct {
	catalogEntry
	source      string
	unsupported bool
}

func (s *Service) inventoryGeodata(ctx context.Context, budget *readBudget) GeodataComponent {
	items := make([]GeodataItem, 0, len(productGeodataCatalog))
	requirements, policyReason := s.requiredGeodata(ctx, budget)
	directory := inspectPath(s.config.GeodataDir, pathDirectory)
	knownCount := 0
	presentCount := 0
	missingCount := 0
	unknownCount := 0
	for _, requirement := range requirements {
		item := GeodataItem{ID: requirement.ID, Name: requirement.Name, Source: requirement.source}
		if requirement.unsupported {
			item.State = StateUnknown
			item.ReasonCode = "manual-unsupported"
			unknownCount++
			items = append(items, item)
			continue
		}
		knownCount++
		if directory.state == StateMissing {
			item.State = StateMissing
			item.ReasonCode = "geodata-directory-missing"
			missingCount++
			items = append(items, item)
			continue
		}
		if directory.state != StatePresent || !directory.valid {
			item.State = StateUnknown
			item.ReasonCode = "geodata-directory-unavailable"
			unknownCount++
			items = append(items, item)
			continue
		}

		file := inspectPath(filepath.Join(s.config.GeodataDir, requirement.Name), pathRegular)
		item.State = file.state
		item.Present = file.present
		if file.state == StateMissing {
			item.ReasonCode = "file-missing"
			missingCount++
			items = append(items, item)
			continue
		}
		if file.state != StatePresent || !file.valid {
			item.ReasonCode = file.reason
			unknownCount++
			items = append(items, item)
			continue
		}
		presentCount++
		item.SizeBytes = file.info.Size()
		item.MTime = file.info.ModTime().UTC().Format(time.RFC3339Nano)
		if file.info.Size() > MaxGeodataFileBytes {
			item.ReasonCode = "file-too-large"
			unknownCount++
			items = append(items, item)
			continue
		}
		hash, err := hashRegularFile(ctx, budget, filepath.Join(s.config.GeodataDir, requirement.Name), file.info)
		if err != nil {
			switch {
			case errors.Is(err, errFileTooLarge):
				item.ReasonCode = "file-too-large"
			case errors.Is(err, errInventoryBudget):
				item.ReasonCode = "inventory-budget-exceeded"
			case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
				item.ReasonCode = "inventory-timeout"
			default:
				item.ReasonCode = "file-unavailable"
			}
			unknownCount++
			item.State = StateUnknown
			items = append(items, item)
			continue
		}
		item.SHA256 = hash
		items = append(items, item)
	}

	component := GeodataComponent{
		Component: Component{
			Kind:           KindGeodata,
			VersionUnknown: true,
			Capability:     CapabilityUnsupported,
		},
		Items: items,
	}
	component.Present = presentCount > 0
	switch {
	case unknownCount > 0:
		component.State = StateUnknown
	case presentCount == 0 && missingCount == knownCount:
		component.State = StateMissing
		component.ReasonCode = "required-files-missing"
	case missingCount > 0:
		component.State = StatePresent
		component.ReasonCode = "required-file-missing"
	default:
		component.State = StatePresent
		component.Capability = CapabilitySupported
	}
	if component.ReasonCode == "" && unknownCount > 0 {
		component.ReasonCode = "inventory-incomplete"
	}
	if policyReason != "" {
		component.State = StateUnknown
		component.Capability = CapabilityUnsupported
		component.ReasonCode = policyReason
	}
	return component
}

func (s *Service) requiredGeodata(ctx context.Context, budget *readBudget) ([]geodataRequirement, string) {
	byName := make(map[string]catalogEntry, len(productGeodataCatalog))
	for _, entry := range productGeodataCatalog {
		byName[entry.Name] = entry
	}
	requirements := make(map[string]geodataRequirement, len(productGeodataCatalog))
	for _, entry := range productGeodataCatalog {
		requirements[entry.ID] = geodataRequirement{catalogEntry: entry, source: "product-catalog"}
	}
	unknown := make(map[string]geodataRequirement)
	policy := s.policyExpressions(ctx, budget)
	for _, expression := range policy.expressions {
		if !strings.HasPrefix(expression.value, "ext:") {
			continue
		}
		entry, ok := byName[logicalFilename(expression.value)]
		if ok && entry.Kind == expression.kind && logicalExtExpression(expression.value) {
			requirements[entry.ID] = geodataRequirement{catalogEntry: entry, source: "product-catalog"}
			continue
		}
		manual := manualRequirement(expression.kind, logicalFilename(expression.value))
		unknown[manual.ID] = manual
	}
	unknownIDs := make([]string, 0, len(unknown))
	for id := range unknown {
		unknownIDs = append(unknownIDs, id)
	}
	sort.Strings(unknownIDs)
	for _, id := range unknownIDs {
		if len(requirements) >= MaxGeodataItems {
			break
		}
		requirements[id] = unknown[id]
	}
	ids := make([]string, 0, len(requirements))
	for id := range requirements {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]geodataRequirement, 0, len(ids))
	for _, id := range ids {
		result = append(result, requirements[id])
	}
	return result, policy.reason
}

func logicalExtExpression(value string) bool {
	parts := strings.Split(strings.TrimPrefix(value, "ext:"), ":")
	return len(parts) == 2 && logicalFilenamePattern.MatchString(parts[0]) && logicalListPattern.MatchString(parts[1])
}

func logicalFilename(value string) string {
	if !strings.HasPrefix(value, "ext:") {
		return "unsupported"
	}
	parts := strings.Split(strings.TrimPrefix(value, "ext:"), ":")
	if len(parts) == 2 && logicalFilenamePattern.MatchString(parts[0]) {
		return parts[0]
	}
	return "unsupported"
}

func manualRequirement(kind, name string) geodataRequirement {
	if kind != "geosite" && kind != "geoip" {
		kind = "unknown"
	}
	if !logicalFilenamePattern.MatchString(name) {
		name = "unsupported"
	}
	return geodataRequirement{
		catalogEntry: catalogEntry{ID: "manual-" + kind + "-" + name, Kind: kind, Name: name},
		source:       "manual/unsupported",
		unsupported:  true,
	}
}

func (s *Service) policyExpressions(ctx context.Context, budget *readBudget) policyExpressionResult {
	raw, check := readRegularBytes(ctx, budget, s.config.AppliancePath, MaxPolicyBytes)
	switch {
	case check.state == StatePresent && check.valid:
		value, err := appliance.Parse(raw)
		if err != nil {
			return policyExpressionResult{reason: "appliance-authority-invalid"}
		}
		return policyExpressionResult{expressions: applianceExpressions(value)}
	case check.state == StatePresent:
		return policyExpressionResult{reason: "appliance-authority-invalid"}
	case check.state != StateMissing:
		return policyExpressionResult{reason: "appliance-authority-unavailable"}
	}

	result := make([]policyExpression, 0, 32)
	if raw, check := readRegularBytes(ctx, budget, s.config.DNSPath, MaxPolicyBytes); check.state == StatePresent {
		if !check.valid {
			return policyExpressionResult{reason: "legacy-policy-unavailable"}
		}
		expressions, err := dnsExpressions(raw)
		if err != nil {
			return policyExpressionResult{reason: "legacy-policy-invalid"}
		}
		result = append(result, expressions...)
	} else if check.state != StateMissing {
		return policyExpressionResult{reason: "legacy-policy-unavailable"}
	}
	if raw, check := readRegularBytes(ctx, budget, s.config.RoutingPath, MaxPolicyBytes); check.state == StatePresent {
		if !check.valid {
			return policyExpressionResult{reason: "legacy-policy-unavailable"}
		}
		expressions, err := routingExpressions(raw)
		if err != nil {
			return policyExpressionResult{reason: "legacy-policy-invalid"}
		}
		result = append(result, expressions...)
	} else if check.state != StateMissing {
		return policyExpressionResult{reason: "legacy-policy-unavailable"}
	}
	return policyExpressionResult{expressions: result}
}

func applianceExpressions(value appliance.Appliance) []policyExpression {
	result := make([]policyExpression, 0, 32)
	for _, server := range value.DNS.Servers {
		for _, domain := range server.Domains {
			result = append(result, policyExpression{kind: "geosite", value: domain})
		}
	}
	for _, rule := range value.Routing.Rules {
		for _, domain := range rule.Domain {
			result = append(result, policyExpression{kind: "geosite", value: domain})
		}
		for _, ip := range rule.IP {
			result = append(result, policyExpression{kind: "geoip", value: ip})
		}
	}
	return result
}

func dnsExpressions(raw []byte) ([]policyExpression, error) {
	var document struct {
		DNS *struct {
			Servers []json.RawMessage `json:"servers"`
		} `json:"dns"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if document.DNS == nil || document.DNS.Servers == nil {
		return nil, errPolicyShape
	}
	result := make([]policyExpression, 0, 16)
	for _, rawServer := range document.DNS.Servers {
		trimmed := bytes.TrimSpace(rawServer)
		if len(trimmed) == 0 {
			return nil, errPolicyShape
		}
		switch trimmed[0] {
		case '"':
			var address string
			if err := json.Unmarshal(trimmed, &address); err != nil || strings.TrimSpace(address) == "" {
				return nil, errPolicyShape
			}
			continue
		case '{':
			var server struct {
				Domains []string `json:"domains"`
			}
			if err := json.Unmarshal(trimmed, &server); err != nil {
				return nil, err
			}
			for _, domain := range server.Domains {
				result = append(result, policyExpression{kind: "geosite", value: domain})
			}
		default:
			return nil, errPolicyShape
		}
	}
	return result, nil
}

func routingExpressions(raw []byte) ([]policyExpression, error) {
	var document struct {
		Routing *struct {
			Rules []struct {
				Domain []string `json:"domain"`
				IP     []string `json:"ip"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if document.Routing == nil || document.Routing.Rules == nil {
		return nil, errPolicyShape
	}
	result := make([]policyExpression, 0, 32)
	for _, rule := range document.Routing.Rules {
		for _, domain := range rule.Domain {
			result = append(result, policyExpression{kind: "geosite", value: domain})
		}
		for _, ip := range rule.IP {
			result = append(result, policyExpression{kind: "geoip", value: ip})
		}
	}
	return result, nil
}

func (s *Service) inventoryKeeneticOS(ctx context.Context, budget *readBudget) Component {
	component := Component{Kind: KindKeeneticOS, VersionUnknown: true, Capability: CapabilityInformational}
	raw, check := readRegularBytes(ctx, budget, s.config.KeeneticOSVersionPath, MaxSignalBytes)
	component.State = check.state
	component.Present = check.present
	if check.state == StateMissing {
		component.State = StateUnknown
		component.ReasonCode = "version-unavailable"
		return component
	}
	if check.state != StatePresent || !check.valid {
		component.ReasonCode = check.reason
		return component
	}
	version := parsePlatformVersion(raw)
	if version == "" {
		component.ReasonCode = "version-unparseable"
		return component
	}
	component.Version = version
	component.VersionUnknown = false
	return component
}

func parsePlatformVersion(raw []byte) string {
	value := strings.TrimSpace(string(raw))
	match := platformVersionPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func (s *Service) inventoryEntware(ctx context.Context, budget *readBudget) Component {
	component := Component{Kind: KindEntware, VersionUnknown: true, Capability: CapabilityInformational}
	binary := inspectPath(s.config.EntwareBinary, pathRegular)
	release, releaseCheck := readRegularBytes(ctx, budget, s.config.EntwareReleasePath, MaxSignalBytes)
	component.Present = binary.present || releaseCheck.present
	if !component.Present && (binary.state == StateUnknown || releaseCheck.state == StateUnknown) {
		component.State = StateUnknown
		component.ReasonCode = "signal-unavailable"
		return component
	}
	if !component.Present {
		component.State = StateMissing
		component.ReasonCode = "not-installed"
		return component
	}
	component.State = StatePresent
	if binary.state == StateUnknown || releaseCheck.state == StateUnknown {
		component.State = StateUnknown
		component.ReasonCode = "signal-unavailable"
		return component
	}
	if releaseCheck.state != StatePresent || !releaseCheck.valid {
		component.ReasonCode = "version-unavailable"
		return component
	}
	version := parsePlatformVersion(release)
	if version == "" {
		component.ReasonCode = "version-unparseable"
		return component
	}
	component.Version = version
	component.VersionUnknown = false
	return component
}

type pathKind uint8

const (
	pathRegular pathKind = iota
	pathExecutable
	pathDirectory
)

type pathCheck struct {
	state   PresenceState
	present bool
	valid   bool
	reason  string
	info    os.FileInfo
}

func inspectPath(path string, kind pathKind) pathCheck {
	if strings.TrimSpace(path) == "" {
		return pathCheck{state: StateUnknown, reason: "path-unavailable"}
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return pathCheck{state: StateMissing, reason: "not-installed"}
		}
		return pathCheck{state: StateUnknown, present: true, reason: "path-unavailable"}
	}
	check := pathCheck{state: StatePresent, present: true, info: info}
	if info.Mode()&os.ModeSymlink != 0 {
		check.valid = false
		check.reason = "not-regular"
		if kind == pathDirectory {
			check.reason = "not-directory"
		}
		return check
	}
	switch kind {
	case pathRegular:
		check.valid = info.Mode().IsRegular()
		if !check.valid {
			check.reason = "not-regular"
		}
	case pathExecutable:
		check.valid = info.Mode().IsRegular() && executableMode(info.Mode())
		if !info.Mode().IsRegular() {
			check.reason = "not-regular"
		} else if !check.valid {
			check.reason = "not-executable"
		}
	case pathDirectory:
		check.valid = info.IsDir()
		if !check.valid {
			check.reason = "not-directory"
		}
	}
	return check
}

func executableMode(mode os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return mode.IsRegular()
	}
	return mode.Perm()&0o111 != 0
}

func readRegularBytes(ctx context.Context, budget *readBudget, path string, limit int64) ([]byte, pathCheck) {
	check := inspectPath(path, pathRegular)
	if check.state != StatePresent || !check.valid {
		return nil, check
	}
	if check.info.Size() < 0 || check.info.Size() > limit {
		check.reason = "file-too-large"
		return nil, check
	}
	file, err := os.Open(path)
	if err != nil {
		check.state = StateUnknown
		check.reason = "file-unavailable"
		return nil, check
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(check.info, opened) {
		check.state = StateUnknown
		check.reason = "file-changed"
		return nil, check
	}
	contents, err := readWithBudget(ctx, budget, file, limit)
	if err != nil {
		check.state = StateUnknown
		switch {
		case errors.Is(err, errFileTooLarge):
			check.reason = "file-too-large"
		case errors.Is(err, errInventoryBudget):
			check.reason = "inventory-budget-exceeded"
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			check.reason = "inventory-timeout"
		default:
			check.reason = "file-unavailable"
		}
		return nil, check
	}
	return contents, check
}

func hashRegularFile(ctx context.Context, budget *readBudget, path string, expected os.FileInfo) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		return "", errors.New("file changed")
	}
	hash := sha256.New()
	if _, err := copyWithBudget(ctx, budget, hash, file, MaxGeodataFileBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func readWithBudget(ctx context.Context, budget *readBudget, reader *os.File, limit int64) ([]byte, error) {
	var output bytes.Buffer
	_, err := copyWithBudget(ctx, budget, &output, reader, limit)
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func copyWithBudget(ctx context.Context, budget *readBudget, destination interface{ Write([]byte) (int, error) }, source *os.File, limit int64) (int64, error) {
	buffer := make([]byte, 32<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, err := source.Read(buffer)
		if read > 0 {
			if total+int64(read) > limit {
				return total, errFileTooLarge
			}
			if budget != nil && !budget.take(int64(read)) {
				return total, errInventoryBudget
			}
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, errors.New("short inventory write")
			}
		}
		if errors.Is(err, os.ErrClosed) {
			return total, err
		}
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return total, err
			}
			if errors.Is(err, io.EOF) {
				return total, nil
			}
			return total, err
		}
	}
}

type readBudget struct {
	remaining int64
}

func (b *readBudget) take(amount int64) bool {
	if amount < 0 || amount > b.remaining {
		return false
	}
	b.remaining -= amount
	return true
}

type commandXrayVersionProbe struct {
	binary string
}

func (p commandXrayVersionProbe) ProbeXrayVersion(ctx context.Context) XrayVersionResult {
	if ctx == nil {
		ctx = context.Background()
	}
	limit := &outputLimit{remaining: MaxXrayProbeOutput}
	stdout := &boundedOutput{limit: limit}
	stderr := &boundedOutput{limit: limit}
	command := exec.CommandContext(ctx, p.binary, "version")
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
	}
	if limit.exceeded() {
		return XrayVersionResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode, Err: errXrayOutputTooLarge}
	}
	return XrayVersionResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode, Err: err}
}

type outputLimit struct {
	mu        sync.Mutex
	remaining int64
	tooLarge  bool
}

func (l *outputLimit) exceeded() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tooLarge
}

type boundedOutput struct {
	limit  *outputLimit
	buffer bytes.Buffer
}

func (w *boundedOutput) Write(value []byte) (int, error) {
	w.limit.mu.Lock()
	defer w.limit.mu.Unlock()
	if w.limit.remaining <= 0 {
		w.limit.tooLarge = true
		return 0, errXrayOutputTooLarge
	}
	count := int64(len(value))
	if count > w.limit.remaining {
		value = value[:w.limit.remaining]
		w.limit.remaining = 0
		w.limit.tooLarge = true
		_, _ = w.buffer.Write(value)
		return len(value), errXrayOutputTooLarge
	}
	w.limit.remaining -= count
	return w.buffer.Write(value)
}

func (w *boundedOutput) Bytes() []byte { return w.buffer.Bytes() }
