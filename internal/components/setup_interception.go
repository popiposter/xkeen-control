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
	"runtime"
	"strings"
)

const (
	setupInterceptionSchemaVersion = 1
	setupInterceptionOwner         = "xkeen-control"
	setupInterceptionGeneration    = "hybrid-v1"
	setupInterceptionPort          = 61219
	setupMaxInterceptionBytes      = 256 << 10

	setupInterceptionHookMarker     = "xkeen-control-hybrid"
	setupReviewedLegacyHookMarker   = "XKeen: Auto-generated file. DO NOT EDIT!"
	setupReviewedLegacyScheduleMark = "XKeen: re-sync deny MAC ipset on schedule start/stop. Auto-generated. DO NOT EDIT!"
)

var (
	ErrSetupInterceptionUnavailable = errors.New("setup interception owner is unavailable")
	ErrSetupInterceptionConflict    = errors.New("setup interception state is unknown or conflicting")
)

// SetupInterceptionGeneration is the only interception shape that Setup can
// create. It is deliberately a closed value: there is no caller-selected
// command, table, chain, port, mark, or policy field.
type SetupInterceptionGeneration struct {
	SchemaVersion   int          `json:"schemaVersion"`
	Owner           string       `json:"owner"`
	Generation      string       `json:"generation"`
	TCPRedirectPort int          `json:"tcpRedirectPort"`
	UDP             TProxyTarget `json:"udpTproxy"`
}

// TProxyTarget is a typed description of the fixed UDP flow. It is not a
// generic firewall rule and is not accepted from HTTP/UI input.
type TProxyTarget struct {
	Protocol string `json:"protocol"`
	Action   string `json:"action"`
	Port     int    `json:"port"`
}

// SetupInterceptionPlan is the safe Preview projection of the target data
// plane. It exposes only the two fixed flows and the source-owned owner.
type SetupInterceptionPlan struct {
	Owner           string `json:"owner"`
	Generation      string `json:"generation"`
	TCPRedirectPort int    `json:"tcpRedirectPort"`
	UDPTProxyPort   int    `json:"udpTproxyPort"`
}

// SetupInterceptionEvidence is bounded, typed pre-state. It never contains
// raw iptables/ipset output or hook bodies. The digest binds it to Preview.
type SetupInterceptionEvidence struct {
	SchemaVersion  int    `json:"schemaVersion"`
	Owner          string `json:"owner,omitempty"`
	Generation     string `json:"generation,omitempty"`
	TCPRedirect    bool   `json:"tcpRedirect"`
	UDPTProxy      bool   `json:"udpTproxy"`
	LegacyHook     bool   `json:"legacyHook"`
	LegacySchedule bool   `json:"legacySchedule"`
	LegacyRules    bool   `json:"legacyRules"`
	LegacyIPSets   bool   `json:"legacyIpSets"`
	Complete       bool   `json:"complete"`
	Digest         string `json:"digest"`
}

// SetupInterceptionOwner is the one typed interception authority used by
// Setup. Implementations may use a fixed platform adapter internally, but no
// generic command or firewall surface is exposed to callers.
type SetupInterceptionOwner interface {
	Inspect(context.Context) (SetupInterceptionEvidence, error)
	Snapshot(context.Context) ([]byte, error)
	RetireLegacy(context.Context, SetupInterceptionEvidence) error
	Apply(context.Context, SetupInterceptionGeneration) error
	Verify(context.Context, SetupInterceptionGeneration) error
	Restore(context.Context, []byte) error
	VerifyRestored(context.Context, []byte) error
}

func setupHybridInterceptionGeneration() SetupInterceptionGeneration {
	return SetupInterceptionGeneration{
		SchemaVersion:   setupInterceptionSchemaVersion,
		Owner:           setupInterceptionOwner,
		Generation:      setupInterceptionGeneration,
		TCPRedirectPort: setupInterceptionPort,
		UDP:             TProxyTarget{Protocol: "udp", Action: "tproxy", Port: setupInterceptionPort},
	}
}

func validSetupInterceptionGeneration(generation SetupInterceptionGeneration) bool {
	return generation.SchemaVersion == setupInterceptionSchemaVersion &&
		generation.Owner == setupInterceptionOwner &&
		generation.Generation == setupInterceptionGeneration &&
		generation.TCPRedirectPort == setupInterceptionPort &&
		generation.UDP.Protocol == "udp" && generation.UDP.Action == "tproxy" && generation.UDP.Port == setupInterceptionPort
}

func setupInterceptionPlan(generation SetupInterceptionGeneration) SetupInterceptionPlan {
	return SetupInterceptionPlan{Owner: generation.Owner, Generation: generation.Generation, TCPRedirectPort: generation.TCPRedirectPort, UDPTProxyPort: generation.UDP.Port}
}

func setupInterceptionEvidenceDigest(evidence SetupInterceptionEvidence) string {
	evidence.Digest = ""
	contents, _ := json.Marshal(evidence)
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

func finalizeSetupInterceptionEvidence(evidence SetupInterceptionEvidence) SetupInterceptionEvidence {
	evidence.SchemaVersion = setupInterceptionSchemaVersion
	evidence.Digest = setupInterceptionEvidenceDigest(evidence)
	return evidence
}

func validSetupInterceptionEvidence(evidence SetupInterceptionEvidence) bool {
	if evidence.SchemaVersion != setupInterceptionSchemaVersion || !isHexSHA256(evidence.Digest) || setupInterceptionEvidenceDigest(evidence) != evidence.Digest {
		return false
	}
	if evidence.Owner == "" {
		return evidence.Generation == "" && !evidence.Complete && !evidence.LegacyHook && !evidence.LegacySchedule && !evidence.LegacyRules && !evidence.LegacyIPSets && !evidence.TCPRedirect && !evidence.UDPTProxy
	}
	if evidence.Owner == setupInterceptionOwner {
		return evidence.Generation == setupInterceptionGeneration && evidence.Complete && evidence.TCPRedirect && evidence.UDPTProxy && !evidence.LegacyHook && !evidence.LegacySchedule && !evidence.LegacyRules && !evidence.LegacyIPSets
	}
	if evidence.Owner == "xkeen-legacy" {
		return evidence.Generation == "" && !evidence.Complete && !evidence.TCPRedirect && !evidence.UDPTProxy && (evidence.LegacyHook || evidence.LegacySchedule || evidence.LegacyRules || evidence.LegacyIPSets)
	}
	return false
}

func setupInterceptionSnapshotBytes(evidence SetupInterceptionEvidence) ([]byte, error) {
	if !validSetupInterceptionEvidence(evidence) {
		return nil, ErrSetupInterceptionConflict
	}
	contents, err := json.Marshal(struct {
		SchemaVersion int                       `json:"schemaVersion"`
		Evidence      SetupInterceptionEvidence `json:"evidence"`
	}{SchemaVersion: setupInterceptionSchemaVersion, Evidence: evidence})
	if err != nil || len(contents) > setupMaxInterceptionBytes {
		return nil, ErrSetupInterceptionConflict
	}
	return append(contents, '\n'), nil
}

func parseSetupInterceptionSnapshot(contents []byte) (SetupInterceptionEvidence, error) {
	if len(contents) == 0 || len(contents) > setupMaxInterceptionBytes {
		return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var value struct {
		SchemaVersion int                       `json:"schemaVersion"`
		Evidence      SetupInterceptionEvidence `json:"evidence"`
	}
	if decoder.Decode(&value) != nil || value.SchemaVersion != setupInterceptionSchemaVersion || !validSetupInterceptionEvidence(value.Evidence) {
		return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
	}
	return value.Evidence, nil
}

// The hook is source-owned and fixed. It only creates/removes its own chain,
// exact jump, and exact policy-routing entries. It does not flush a built-in
// chain or inspect/delete unrelated router firewall state.
const setupSourceOwnedHybridHook = `#!/bin/sh
# xkeen-control-hybrid v1; source-owned; fixed TCP redirect + UDP TProxy
set -eu

apply_family() {
  family="$1"
  on_ip="$2"
  "$family" -t nat -N XKEEN_CONTROL_HYBRID 2>/dev/null || true
  "$family" -t nat -F XKEEN_CONTROL_HYBRID
  "$family" -t nat -A XKEEN_CONTROL_HYBRID -p tcp -m comment --comment xkeen-control-hybrid -j REDIRECT --to-ports 61219
  "$family" -t nat -C PREROUTING -p tcp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID 2>/dev/null || "$family" -t nat -A PREROUTING -p tcp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID

  "$family" -t mangle -N XKEEN_CONTROL_HYBRID 2>/dev/null || true
  "$family" -t mangle -F XKEEN_CONTROL_HYBRID
  "$family" -t mangle -A XKEEN_CONTROL_HYBRID -p udp -m socket -m comment --comment xkeen-control-hybrid -j MARK --set-mark 0x111/0xfff
  "$family" -t mangle -A XKEEN_CONTROL_HYBRID -p udp -m comment --comment xkeen-control-hybrid -j TPROXY --on-ip "$on_ip" --on-port 61219 --tproxy-mark 0x111/0xfff
  "$family" -t mangle -C PREROUTING -p udp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID 2>/dev/null || "$family" -t mangle -A PREROUTING -p udp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID
}

apply_family iptables 0.0.0.0
command -v ip6tables >/dev/null 2>&1 && apply_family ip6tables :: || true
command -v ip >/dev/null 2>&1 && {
  ip -4 rule add fwmark 0x111/0xfff table 111 pref 111 2>/dev/null || true
  ip -4 route add local 0.0.0.0/0 dev lo table 111 2>/dev/null || true
  ip -6 rule add fwmark 0x111/0xfff table 111 pref 111 2>/dev/null || true
  ip -6 route add local ::/0 dev lo table 111 2>/dev/null || true
}
`

const setupSourceOwnedScheduleHook = `#!/bin/sh
# xkeen-control-hybrid v1; source-owned schedule reconciliation
set -eu
[ "$#" -eq 1 ] && { [ "$1" = start ] || [ "$1" = stop ]; } || exit 2
[ -x /opt/etc/ndm/netfilter.d/proxy.sh ] && /opt/etc/ndm/netfilter.d/proxy.sh
`

func setupSourceOwnedHybridHookBytes() []byte   { return []byte(setupSourceOwnedHybridHook) }
func setupSourceOwnedScheduleHookBytes() []byte { return []byte(setupSourceOwnedScheduleHook) }

func validSetupHybridConfig(files map[string][]byte) bool {
	contents, ok := files["xray/03_inbounds.json"]
	if !ok {
		return false
	}
	var value struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
			Settings struct {
				Network        string `json:"network"`
				FollowRedirect bool   `json:"followRedirect"`
			} `json:"settings"`
			StreamSettings struct {
				Sockopt struct {
					TProxy string `json:"tproxy"`
				} `json:"sockopt"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(contents, &value) != nil {
		return false
	}
	redirect, tproxy := false, false
	for _, inbound := range value.Inbounds {
		if inbound.Tag == "redirect" && inbound.Port == setupInterceptionPort && inbound.Protocol == "dokodemo-door" && inbound.Settings.Network == "tcp" && inbound.Settings.FollowRedirect {
			redirect = true
		}
		if inbound.Tag == "tproxy" && inbound.Port == setupInterceptionPort && inbound.Protocol == "dokodemo-door" && inbound.Settings.Network == "udp" && inbound.Settings.FollowRedirect && inbound.StreamSettings.Sockopt.TProxy == "tproxy" {
			tproxy = true
		}
	}
	return redirect && tproxy
}

func setupReviewedLegacyNetfilterHook(contents []byte) bool {
	text := strings.ReplaceAll(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\r", "\n")
	return strings.HasPrefix(text, "#!/bin/sh\n# "+setupReviewedLegacyHookMarker) &&
		strings.Contains(text, "file_netfilter_hook=") &&
		strings.Contains(text, "file_schedule_hook=") &&
		strings.Contains(text, "name_chain=") &&
		strings.Contains(text, "iptables") && strings.Contains(text, "ip6tables") &&
		strings.Contains(text, "iptables-restore") && strings.Contains(text, "ipset") &&
		strings.Contains(text, "xkeen_rule") && strings.Contains(text, "configure_firewall()") &&
		strings.Contains(text, "clean_firewall()") && strings.Contains(text, "proxy_start()") &&
		strings.Contains(text, "proxy_stop()") && strings.Contains(text, "XKEEN")
}

func setupReviewedLegacyScheduleHook(contents []byte) bool {
	text := strings.ReplaceAll(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\r", "\n")
	return text == "#!/bin/sh\n# "+setupReviewedLegacyScheduleMark+"\n[ \"$1\" = \"start\" ] || [ \"$1\" = \"stop\" ] || exit 0\n[ -x /opt/etc/ndm/netfilter.d/proxy.sh ] && /opt/etc/ndm/netfilter.d/proxy.sh\n"
}

func setupInterceptionFileKind(path string, expected, legacy func([]byte) bool) (string, []byte, error) {
	state, err := setupPathState(path)
	if err != nil {
		return "", nil, err
	}
	if state == setupPathAbsent {
		return "absent", nil, nil
	}
	if state != setupPathRegular {
		return "unknown", nil, ErrSetupInterceptionConflict
	}
	contents, err := readBoundedSetupFile(path, setupMaxInterceptionBytes)
	if err != nil {
		return "unknown", nil, ErrSetupInterceptionConflict
	}
	if expected(contents) {
		return "source", contents, nil
	}
	if legacy(contents) {
		return "legacy", contents, nil
	}
	return "unknown", nil, ErrSetupInterceptionConflict
}

type setupInterceptionStateFile struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Owner           string `json:"owner"`
	Generation      string `json:"generation"`
	TCPRedirectPort int    `json:"tcpRedirectPort"`
	UDPTProxyPort   int    `json:"udpTproxyPort"`
}

func setupInterceptionStateBytes(generation SetupInterceptionGeneration) ([]byte, error) {
	if !validSetupInterceptionGeneration(generation) {
		return nil, ErrSetupInterceptionConflict
	}
	contents, err := json.Marshal(setupInterceptionStateFile{SchemaVersion: setupInterceptionSchemaVersion, Owner: generation.Owner, Generation: generation.Generation, TCPRedirectPort: generation.TCPRedirectPort, UDPTProxyPort: generation.UDP.Port})
	if err != nil {
		return nil, ErrSetupInterceptionConflict
	}
	return append(contents, '\n'), nil
}

func parseSetupInterceptionState(contents []byte) (SetupInterceptionGeneration, error) {
	if len(contents) == 0 || len(contents) > setupMaxInterceptionBytes {
		return SetupInterceptionGeneration{}, ErrSetupInterceptionConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var state setupInterceptionStateFile
	if decoder.Decode(&state) != nil || state.SchemaVersion != setupInterceptionSchemaVersion {
		return SetupInterceptionGeneration{}, ErrSetupInterceptionConflict
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || state.Owner != setupInterceptionOwner || state.Generation != setupInterceptionGeneration || state.TCPRedirectPort != setupInterceptionPort || state.UDPTProxyPort != setupInterceptionPort {
		return SetupInterceptionGeneration{}, ErrSetupInterceptionConflict
	}
	return setupHybridInterceptionGeneration(), nil
}

type fileHybridInterceptionOwner struct {
	hookPath     string
	schedulePath string
	statePath    string
	syncDir      func(string) error
}

// NewFileHybridInterceptionOwner is a deterministic, typed owner used by
// synthetic fixtures and by non-router development contours. It models the
// exact persistent ownership contract without exposing a command surface.
func NewFileHybridInterceptionOwner(paths SetupPaths, syncDir func(string) error) SetupInterceptionOwner {
	if syncDir == nil {
		syncDir = syncDirectory
	}
	return &fileHybridInterceptionOwner{hookPath: paths.InterceptionHook, schedulePath: paths.InterceptionScheduleHook, statePath: paths.InterceptionState, syncDir: syncDir}
}

func (o *fileHybridInterceptionOwner) Inspect(context.Context) (SetupInterceptionEvidence, error) {
	if o == nil || o.hookPath == "" || o.schedulePath == "" || o.statePath == "" {
		return SetupInterceptionEvidence{}, ErrSetupInterceptionUnavailable
	}
	hookKind, _, err := setupInterceptionFileKind(o.hookPath, func(contents []byte) bool { return bytes.Equal(contents, setupSourceOwnedHybridHookBytes()) }, setupReviewedLegacyNetfilterHook)
	if err != nil {
		return SetupInterceptionEvidence{}, err
	}
	scheduleKind, _, err := setupInterceptionFileKind(o.schedulePath, func(contents []byte) bool { return bytes.Equal(contents, setupSourceOwnedScheduleHookBytes()) }, setupReviewedLegacyScheduleHook)
	if err != nil {
		return SetupInterceptionEvidence{}, err
	}
	state, err := setupPathState(o.statePath)
	if err != nil {
		return SetupInterceptionEvidence{}, err
	}
	stateSource := false
	if state != setupPathAbsent {
		if state != setupPathRegular {
			return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
		}
		contents, readErr := readBoundedSetupFile(o.statePath, setupMaxInterceptionBytes)
		if readErr != nil {
			return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
		}
		if _, parseErr := parseSetupInterceptionState(contents); parseErr != nil {
			return SetupInterceptionEvidence{}, parseErr
		}
		stateSource = true
	}
	evidence := SetupInterceptionEvidence{Owner: "", Generation: "", TCPRedirect: false, UDPTProxy: false, LegacyHook: hookKind == "legacy", LegacySchedule: scheduleKind == "legacy"}
	sourceFiles := hookKind == "source" || scheduleKind == "source"
	legacyFiles := evidence.LegacyHook || evidence.LegacySchedule
	switch {
	case legacyFiles && (sourceFiles || stateSource):
		return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
	case legacyFiles:
		evidence.Owner = "xkeen-legacy"
	case sourceFiles || stateSource:
		evidence.Owner = setupInterceptionOwner
		evidence.Generation = setupInterceptionGeneration
		evidence.TCPRedirect = true
		evidence.UDPTProxy = true
		evidence.Complete = hookKind == "source" && scheduleKind == "source" && stateSource
	}
	return finalizeSetupInterceptionEvidence(evidence), nil
}

func (o *fileHybridInterceptionOwner) Snapshot(ctx context.Context) ([]byte, error) {
	evidence, err := o.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	return setupInterceptionSnapshotBytes(evidence)
}

func (o *fileHybridInterceptionOwner) RetireLegacy(_ context.Context, evidence SetupInterceptionEvidence) error {
	if o == nil || evidence.Owner != "xkeen-legacy" || !validSetupInterceptionEvidence(evidence) {
		if evidence.Owner == "" {
			return nil
		}
		return ErrSetupInterceptionConflict
	}
	for _, path := range []string{o.hookPath, o.schedulePath, o.statePath} {
		if err := removeSetupInterceptionFile(path); err != nil {
			return err
		}
	}
	return nil
}

func (o *fileHybridInterceptionOwner) Apply(_ context.Context, generation SetupInterceptionGeneration) error {
	if o == nil || !validSetupInterceptionGeneration(generation) {
		return ErrSetupInterceptionConflict
	}
	hook := setupSourceOwnedHybridHookBytes()
	schedule := setupSourceOwnedScheduleHookBytes()
	state, err := setupInterceptionStateBytes(generation)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{{o.hookPath, hook, 0o700}, {o.schedulePath, schedule, 0o755}, {o.statePath, state, 0o600}} {
		if err := ensureSetupDirectory(filepath.Dir(item.path)); err != nil {
			return err
		}
		if err := writeAtomicComponentFile(item.path, item.data, item.mode, o.syncDir); err != nil {
			return err
		}
	}
	return nil
}

func (o *fileHybridInterceptionOwner) Verify(ctx context.Context, generation SetupInterceptionGeneration) error {
	if !validSetupInterceptionGeneration(generation) {
		return ErrSetupInterceptionConflict
	}
	evidence, err := o.Inspect(ctx)
	if err != nil || evidence.Owner != setupInterceptionOwner || evidence.Generation != generation.Generation || !evidence.Complete || !evidence.TCPRedirect || !evidence.UDPTProxy {
		return ErrSetupInterceptionConflict
	}
	return nil
}

func (o *fileHybridInterceptionOwner) Restore(_ context.Context, snapshot []byte) error {
	if o == nil {
		return ErrSetupInterceptionUnavailable
	}
	if len(snapshot) == 0 {
		// Fresh rollback can interrupt Apply after only one source-owned file
		// has been replaced. Inspecting only a complete generation would leave
		// that partial generation behind and make recovery unprovable. Remove
		// only a wholly source-owned-or-absent partial set; legacy/unknown state
		// remains a hard conflict.
		return o.removePartialSourceOwned()
	}
	if _, err := parseSetupInterceptionSnapshot(snapshot); err != nil {
		return err
	}
	// The Setup file snapshot restores exact hook/state bytes. The typed owner
	// verifies those bytes after the rollback rather than re-creating a foreign
	// lifecycle or editing a caller-provided path.
	return nil
}

func (o *fileHybridInterceptionOwner) removePartialSourceOwned() error {
	if o == nil {
		return ErrSetupInterceptionUnavailable
	}
	hookKind, _, err := setupInterceptionFileKind(o.hookPath, func(contents []byte) bool { return bytes.Equal(contents, setupSourceOwnedHybridHookBytes()) }, setupReviewedLegacyNetfilterHook)
	if err != nil {
		return err
	}
	scheduleKind, _, err := setupInterceptionFileKind(o.schedulePath, func(contents []byte) bool { return bytes.Equal(contents, setupSourceOwnedScheduleHookBytes()) }, setupReviewedLegacyScheduleHook)
	if err != nil {
		return err
	}
	state, err := setupPathState(o.statePath)
	if err != nil {
		return err
	}
	stateSource := false
	if state != setupPathAbsent {
		if state != setupPathRegular {
			return ErrSetupInterceptionConflict
		}
		contents, readErr := readBoundedSetupFile(o.statePath, setupMaxInterceptionBytes)
		if readErr != nil {
			return ErrSetupInterceptionConflict
		}
		if _, parseErr := parseSetupInterceptionState(contents); parseErr != nil {
			return parseErr
		}
		stateSource = true
	}
	if hookKind == "legacy" || scheduleKind == "legacy" || hookKind == "unknown" || scheduleKind == "unknown" {
		return ErrSetupInterceptionConflict
	}
	if hookKind != "source" && scheduleKind != "source" && !stateSource {
		return nil
	}
	for _, path := range []string{o.hookPath, o.schedulePath, o.statePath} {
		if err := removeSetupInterceptionFile(path); err != nil {
			return err
		}
	}
	for _, directory := range []string{filepath.Dir(o.hookPath), filepath.Dir(o.schedulePath), filepath.Dir(o.statePath)} {
		if err := o.syncDir(directory); err != nil {
			return err
		}
	}
	return nil
}

func (o *fileHybridInterceptionOwner) VerifyRestored(ctx context.Context, snapshot []byte) error {
	if len(snapshot) == 0 {
		current, err := o.Inspect(ctx)
		if err != nil || current.Owner != "" {
			return ErrSetupInterceptionConflict
		}
		return nil
	}
	previous, err := parseSetupInterceptionSnapshot(snapshot)
	if err != nil {
		return err
	}
	current, err := o.Inspect(ctx)
	if err != nil || current.Digest != previous.Digest {
		return ErrSetupInterceptionConflict
	}
	return nil
}

func removeSetupInterceptionFile(path string) error {
	state, err := setupPathState(path)
	if err != nil {
		return err
	}
	if state == setupPathAbsent {
		return nil
	}
	if state != setupPathRegular {
		return ErrSetupInterceptionConflict
	}
	return os.Remove(path)
}

type nativeHybridInterceptionOwner struct {
	file *fileHybridInterceptionOwner
}

// NewKeeneticHybridInterceptionOwner is the production adapter. All process
// invocations below are fixed, typed operations for the one Hybrid generation;
// no caller can supply a binary, argv, table, rule, or script.
func NewKeeneticHybridInterceptionOwner(paths SetupPaths) SetupInterceptionOwner {
	return &nativeHybridInterceptionOwner{file: NewFileHybridInterceptionOwner(paths, syncDirectory).(*fileHybridInterceptionOwner)}
}

type boundedCommandOutput struct {
	contents bytes.Buffer
	tooLarge bool
}

func (output *boundedCommandOutput) Write(contents []byte) (int, error) {
	remaining := setupMaxInterceptionBytes - output.contents.Len()
	if remaining <= 0 {
		output.tooLarge = true
		return 0, io.ErrShortBuffer
	}
	if len(contents) > remaining {
		_, _ = output.contents.Write(contents[:remaining])
		output.tooLarge = true
		return remaining, io.ErrShortBuffer
	}
	return output.contents.Write(contents)
}

// runFixedOutput is kept separate from the typed methods so the production
// adapter cannot accidentally grow a generic command API. It bounds output
// while the process is running, before any parser sees it.
func runFixedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		return nil, ErrSetupInterceptionUnavailable
	}
	command := exec.CommandContext(ctx, name, args...)
	var output boundedCommandOutput
	command.Stdout = &output
	if err := command.Run(); err != nil || output.tooLarge {
		return nil, ErrSetupInterceptionUnavailable
	}
	return output.contents.Bytes(), nil
}

func runFixed(ctx context.Context, name string, args ...string) error {
	if runtime.GOOS == "windows" {
		return ErrSetupInterceptionUnavailable
	}
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return ErrSetupInterceptionUnavailable
	}
	return nil
}

type nativeHybridSnapshot struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Evidence      SetupInterceptionEvidence `json:"evidence"`
	IPTables      []byte                    `json:"iptables,omitempty"`
	IP6Tables     []byte                    `json:"ip6tables,omitempty"`
	IPSets        []byte                    `json:"ipsets,omitempty"`
}

func inspectNativeRules(ctx context.Context) (SetupInterceptionEvidence, []byte, []byte, []byte, error) {
	ipv4, err := runFixedOutput(ctx, "iptables-save")
	if err != nil {
		return SetupInterceptionEvidence{}, nil, nil, nil, err
	}
	ipv6, err := runFixedOutput(ctx, "ip6tables-save")
	if err != nil {
		// IPv6 may be disabled on a supported router; an empty typed output is
		// not a second ownership source.
		ipv6 = nil
	}
	ipsets, err := runFixedOutput(ctx, "ipset", "save")
	if err != nil {
		ipsets = nil
	}
	all := append(append(append([]byte(nil), ipv4...), ipv6...), ipsets...)
	if len(all) > setupMaxInterceptionBytes {
		return SetupInterceptionEvidence{}, nil, nil, nil, ErrSetupInterceptionConflict
	}
	evidence := SetupInterceptionEvidence{}
	for _, line := range strings.Split(strings.ReplaceAll(string(all), "\r\n", "\n"), "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		if lower == "" {
			continue
		}
		if strings.Contains(lower, setupInterceptionHookMarker) {
			evidence.TCPRedirect = evidence.TCPRedirect || strings.Contains(lower, "to-ports 61219") || strings.Contains(lower, "redirect")
			evidence.UDPTProxy = evidence.UDPTProxy || strings.Contains(lower, "tproxy") || strings.Contains(lower, "0x111")
			continue
		}
		if strings.Contains(lower, "xkeen_rule") || strings.Contains(lower, "-n xkeen") || strings.Contains(lower, "-n xkeen_force") || strings.Contains(lower, "xkeen_deny_mac") || strings.Contains(lower, "geo_exclude") || strings.Contains(lower, "user_exclude") || strings.Contains(lower, "geo_override") {
			evidence.LegacyRules = evidence.LegacyRules || strings.HasPrefix(lower, "-a ") || strings.Contains(lower, "xkeen_rule") || strings.Contains(lower, "-n xkeen")
			evidence.LegacyIPSets = evidence.LegacyIPSets || strings.Contains(lower, "xkeen_deny_mac") || strings.Contains(lower, "geo_exclude") || strings.Contains(lower, "user_exclude") || strings.Contains(lower, "geo_override")
			continue
		}
		if strings.Contains(lower, "xkeen") {
			return SetupInterceptionEvidence{}, nil, nil, nil, ErrSetupInterceptionConflict
		}
	}
	if evidence.TCPRedirect || evidence.UDPTProxy {
		evidence.Owner = setupInterceptionOwner
		evidence.Generation = setupInterceptionGeneration
		evidence.Complete = evidence.TCPRedirect && evidence.UDPTProxy
	}
	if evidence.LegacyRules || evidence.LegacyIPSets {
		if evidence.Owner != "" {
			return SetupInterceptionEvidence{}, nil, nil, nil, ErrSetupInterceptionConflict
		}
		evidence.Owner = "xkeen-legacy"
	}
	return finalizeSetupInterceptionEvidence(evidence), ipv4, ipv6, ipsets, nil
}

func mergeNativeInterceptionEvidence(fileEvidence, ruleEvidence SetupInterceptionEvidence) (SetupInterceptionEvidence, error) {
	if fileEvidence.Owner != "" && ruleEvidence.Owner != "" && fileEvidence.Owner != ruleEvidence.Owner {
		return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
	}
	// Kernel ownership is admitted only when the exact reviewed persistent
	// owner is present too. This prevents a stale/foreign rule set from being
	// treated as a source-owned generation merely because it contains a tag.
	if ruleEvidence.Owner != "" && fileEvidence.Owner != ruleEvidence.Owner {
		return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
	}
	if fileEvidence.Owner == setupInterceptionOwner &&
		(!fileEvidence.Complete || ruleEvidence.Owner != setupInterceptionOwner || !ruleEvidence.Complete) {
		// A source-owned hook without the source-owned kernel generation is not
		// a configured Hybrid interception. Setup must not accept a stale file
		// as proof that traffic is actually intercepted.
		return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
	}
	evidence := fileEvidence
	if evidence.Owner == "" {
		evidence = ruleEvidence
	} else if ruleEvidence.Owner != "" {
		evidence.TCPRedirect = evidence.TCPRedirect && ruleEvidence.TCPRedirect
		evidence.UDPTProxy = evidence.UDPTProxy && ruleEvidence.UDPTProxy
		evidence.LegacyRules = evidence.LegacyRules || ruleEvidence.LegacyRules
		evidence.LegacyIPSets = evidence.LegacyIPSets || ruleEvidence.LegacyIPSets
		evidence.Complete = evidence.Complete && ruleEvidence.Complete
	}
	return finalizeSetupInterceptionEvidence(evidence), nil
}

func (o *nativeHybridInterceptionOwner) Inspect(ctx context.Context) (SetupInterceptionEvidence, error) {
	fileEvidence, err := o.file.Inspect(ctx)
	if err != nil {
		return SetupInterceptionEvidence{}, err
	}
	ruleEvidence, _, _, _, err := inspectNativeRules(ctx)
	if err != nil {
		return SetupInterceptionEvidence{}, err
	}
	return mergeNativeInterceptionEvidence(fileEvidence, ruleEvidence)
}

func (o *nativeHybridInterceptionOwner) Snapshot(ctx context.Context) ([]byte, error) {
	evidence, ipv4, ipv6, ipsets, err := inspectNativeRules(ctx)
	if err != nil {
		return nil, err
	}
	fileEvidence, err := o.file.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	evidence, err = mergeNativeInterceptionEvidence(fileEvidence, evidence)
	if err != nil {
		return nil, err
	}
	contents, err := json.Marshal(nativeHybridSnapshot{SchemaVersion: setupInterceptionSchemaVersion, Evidence: evidence, IPTables: ipv4, IP6Tables: ipv6, IPSets: ipsets})
	if err != nil || len(contents) > setupMaxInterceptionBytes {
		return nil, ErrSetupInterceptionConflict
	}
	return append(contents, '\n'), nil
}

func (o *nativeHybridInterceptionOwner) RetireLegacy(ctx context.Context, evidence SetupInterceptionEvidence) error {
	if evidence.Owner == "" {
		return nil
	}
	if evidence.Owner != "xkeen-legacy" || !validSetupInterceptionEvidence(evidence) {
		return ErrSetupInterceptionConflict
	}
	for _, family := range []string{"iptables", "ip6tables"} {
		if err := retireLegacyTaggedRules(ctx, family); err != nil && !errors.Is(err, ErrSetupInterceptionUnavailable) {
			return err
		}
		for _, table := range []string{"nat", "mangle"} {
			for _, chain := range []string{"xkeen_force", "xkeen"} {
				_ = runFixed(ctx, family, "-t", table, "-F", chain)
				_ = runFixed(ctx, family, "-t", table, "-X", chain)
			}
		}
	}
	for _, set := range []string{"xkeen_deny_mac", "ext_exclude", "ext_exclude6", "geo_exclude", "geo_exclude6", "geo_override", "geo_override6", "user_exclude", "user_exclude6"} {
		_ = runFixed(ctx, "ipset", "flush", set)
		_ = runFixed(ctx, "ipset", "destroy", set)
	}
	_ = runFixed(ctx, "ip", "-4", "rule", "del", "fwmark", "0x111/0xfff", "table", "111", "pref", "111")
	_ = runFixed(ctx, "ip", "-4", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "111")
	_ = runFixed(ctx, "ip", "-6", "rule", "del", "fwmark", "0x111/0xfff", "table", "111", "pref", "111")
	_ = runFixed(ctx, "ip", "-6", "route", "del", "local", "::/0", "dev", "lo", "table", "111")
	return o.file.RetireLegacy(ctx, evidence)
}

func retireLegacyTaggedRules(ctx context.Context, family string) error {
	contents, err := runFixedOutput(ctx, family+"-save")
	if err != nil {
		return err
	}
	table := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "*") {
			table = strings.TrimPrefix(line, "*")
			continue
		}
		if table == "" || !strings.HasPrefix(line, "-A ") {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "xkeen_rule") && !strings.Contains(lower, "-j xkeen") && !strings.Contains(lower, "-j xkeen_force") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "-A" || fields[1] == "" {
			return ErrSetupInterceptionConflict
		}
		args := append([]string{"-t", table, "-D", fields[1]}, fields[2:]...)
		if err := runFixed(ctx, family, args...); err != nil {
			return err
		}
	}
	return nil
}

func (o *nativeHybridInterceptionOwner) Apply(ctx context.Context, generation SetupInterceptionGeneration) error {
	if !validSetupInterceptionGeneration(generation) {
		return ErrSetupInterceptionConflict
	}
	if err := o.file.Apply(ctx, generation); err != nil {
		return err
	}
	if err := runFixed(ctx, o.file.hookPath); err != nil {
		return err
	}
	return nil
}

func (o *nativeHybridInterceptionOwner) Verify(ctx context.Context, generation SetupInterceptionGeneration) error {
	if err := o.file.Verify(ctx, generation); err != nil {
		return err
	}
	evidence, err := o.Inspect(ctx)
	if err != nil || evidence.Owner != setupInterceptionOwner || !evidence.Complete || !evidence.TCPRedirect || !evidence.UDPTProxy {
		return ErrSetupInterceptionConflict
	}
	return nil
}

func (o *nativeHybridInterceptionOwner) Restore(ctx context.Context, snapshot []byte) error {
	if len(snapshot) == 0 {
		if err := o.removeSourceOwned(ctx); err != nil {
			return err
		}
		return o.file.Restore(ctx, nil)
	}
	var previous nativeHybridSnapshot
	decoder := json.NewDecoder(bytes.NewReader(snapshot))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&previous) != nil || previous.SchemaVersion != setupInterceptionSchemaVersion || !validSetupInterceptionEvidence(previous.Evidence) {
		return ErrSetupInterceptionConflict
	}
	if len(previous.IPTables) > setupMaxInterceptionBytes || len(previous.IP6Tables) > setupMaxInterceptionBytes || len(previous.IPSets) > setupMaxInterceptionBytes {
		return ErrSetupInterceptionConflict
	}
	if len(previous.IPTables) != 0 {
		if err := restoreFixed(ctx, "iptables-restore", previous.IPTables); err != nil {
			return err
		}
	}
	if len(previous.IP6Tables) != 0 {
		if err := restoreFixed(ctx, "ip6tables-restore", previous.IP6Tables); err != nil {
			return err
		}
	}
	if len(previous.IPSets) != 0 {
		if err := restoreFixed(ctx, "ipset", previous.IPSets, "restore", "-exist"); err != nil {
			return err
		}
	}
	return o.file.Restore(ctx, snapshotEvidenceOnly(snapshot, previous.Evidence))
}

func (o *nativeHybridInterceptionOwner) removeSourceOwned(ctx context.Context) error {
	for _, family := range []string{"iptables", "ip6tables"} {
		for _, table := range []string{"nat", "mangle"} {
			_ = runFixed(ctx, family, "-t", table, "-F", "XKEEN_CONTROL_HYBRID")
			_ = runFixed(ctx, family, "-t", table, "-X", "XKEEN_CONTROL_HYBRID")
		}
	}
	_ = runFixed(ctx, "ip", "-4", "rule", "del", "fwmark", "0x111/0xfff", "table", "111", "pref", "111")
	_ = runFixed(ctx, "ip", "-4", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "111")
	_ = runFixed(ctx, "ip", "-6", "rule", "del", "fwmark", "0x111/0xfff", "table", "111", "pref", "111")
	_ = runFixed(ctx, "ip", "-6", "route", "del", "local", "::/0", "dev", "lo", "table", "111")
	return nil
}

func snapshotEvidenceOnly(snapshot []byte, evidence SetupInterceptionEvidence) []byte {
	contents, _ := setupInterceptionSnapshotBytes(evidence)
	return contents
}

func restoreFixed(ctx context.Context, name string, contents []byte, args ...string) error {
	if runtime.GOOS == "windows" {
		return ErrSetupInterceptionUnavailable
	}
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = bytes.NewReader(contents)
	if err := command.Run(); err != nil {
		return ErrSetupInterceptionUnavailable
	}
	return nil
}

func (o *nativeHybridInterceptionOwner) VerifyRestored(ctx context.Context, snapshot []byte) error {
	if len(snapshot) == 0 {
		current, err := o.Inspect(ctx)
		if err != nil || current.Owner != "" {
			return ErrSetupInterceptionConflict
		}
		return nil
	}
	var previous nativeHybridSnapshot
	decoder := json.NewDecoder(bytes.NewReader(snapshot))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&previous) != nil || !validSetupInterceptionEvidence(previous.Evidence) {
		return ErrSetupInterceptionConflict
	}
	current, err := o.Inspect(ctx)
	if err != nil || current.Digest != previous.Evidence.Digest {
		return ErrSetupInterceptionConflict
	}
	return nil
}
