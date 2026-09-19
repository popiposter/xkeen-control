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
	setupInterceptionLANInterface  = "br0"
	setupInterceptionMark          = "0x111/0xfff"
	setupInterceptionTable         = "111"
	setupInterceptionPriority      = "111"
	setupInterceptionIPv6Policy    = "disabled"
	setupInterceptionDestination   = "exclude-local"

	setupInterceptionHookMarker              = "xkeen-control-hybrid"
	setupReviewedLegacyHookMarker            = "XKeen: Auto-generated file. DO NOT EDIT!"
	setupReviewedLegacyScheduleMark          = "XKeen: re-sync deny MAC ipset on schedule start/stop. Auto-generated. DO NOT EDIT!"
	reviewedUpstreamProxyHookCanonicalSHA256 = "a4a68d2dcf943d313b9e104d0a8dd1b58443da1a24fef915ccd8f6ade16a70cc"
)

// These are the exact dynamic assignments emitted by the reviewed upstream
// S05 generator before its substantive proxy hook body. Values are runtime
// state and are intentionally canonicalized, never trusted as identity.
var reviewedLegacyProxyInjectedVariables = []string{
	"name_client", "name_profile", "mode_proxy", "network_redirect", "network_tproxy", "networks",
	"name_chain", "port_redirect", "port_tproxy", "port_dscp_force_proxy", "port_dscp_force_proxy_redirect",
	"port_dscp_force_proxy_tproxy", "port_donor", "port_exclude", "policy_mark", "policy_mark_full",
	"comment_tag", "comment", "custom_mark", "nfqws_mark", "dscp_exclude", "dscp_proxy", "dscp_force_proxy",
	"dscp_force_proxy_tag", "mode_dscp_force_proxy", "network_dscp_force_proxy", "network_dscp_force_proxy_redirect",
	"network_dscp_force_proxy_tproxy", "user_policies", "table_redirect", "table_tproxy", "table_mark", "table_id",
	"file_dns", "arm_cpu", "file_ca", "proxy_dns", "proxy_router", "directory_configs_app", "directory_xray_config",
	"directory_xray_asset", "iptables_supported", "ip6tables_supported", "arm64_fd", "other_fd", "aghfix", "ipv6_proxy",
	"ipv4_proxy", "val_exclude_ip6", "val_exclude_ip4", "name_ipset_deny_mac", "url_server", "url_hotspot", "rci_token",
	"ru_exclude_ipv4", "ru_exclude_ipv6", "gomemlimit_value", "killswitch",
}

var reviewedLegacyProxyFixedAssignments = map[string]string{
	"name_client":           "xray",
	"name_profile":          "xkeen",
	"mode_proxy":            "Hybrid",
	"network_redirect":      "tcp",
	"network_tproxy":        "udp",
	"networks":              "tcp udp",
	"name_chain":            "xkeen",
	"port_redirect":         "61219",
	"port_tproxy":           "61219",
	"comment_tag":           "xkeen_rule",
	"table_redirect":        "nat",
	"table_tproxy":          "mangle",
	"table_mark":            "0x111",
	"table_id":              "111",
	"name_ipset_deny_mac":   "xkeen_deny_mac",
	"url_server":            "127.0.0.1:79",
	"url_hotspot":           "rci/show/ip/hotspot",
	"ipv4_proxy":            "127.0.0.1",
	"ipv6_proxy":            "::1",
	"directory_configs_app": "/opt/etc/xray",
	"directory_xray_config": "/opt/etc/xray/configs",
	"directory_xray_asset":  "/opt/etc/xray/dat",
}

var (
	ErrSetupInterceptionUnavailable = errors.New("setup interception owner is unavailable")
	ErrSetupInterceptionConflict    = errors.New("setup interception state is unknown or conflicting")
)

// SetupInterceptionGeneration is the only interception shape that Setup can
// create. It is deliberately a closed value: there is no caller-selected
// command, table, chain, port, mark, or policy field.
type SetupInterceptionGeneration struct {
	SchemaVersion   int                    `json:"schemaVersion"`
	Owner           string                 `json:"owner"`
	Generation      string                 `json:"generation"`
	Scope           SetupInterceptionScope `json:"scope"`
	TCPRedirectPort int                    `json:"tcpRedirectPort"`
	UDP             TProxyTarget           `json:"udpTproxy"`
}

// SetupInterceptionScope is the one server-owned Keenetic client scope. The
// values are fixed product policy, not request data: br0 is the typed LAN
// bridge, LOCAL destinations are excluded, and IPv6 interception stays off.
type SetupInterceptionScope struct {
	Interface         string `json:"interface"`
	DestinationPolicy string `json:"destinationPolicy"`
	IPv6Policy        string `json:"ipv6Policy"`
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
	Owner           string                 `json:"owner"`
	Generation      string                 `json:"generation"`
	Scope           SetupInterceptionScope `json:"scope"`
	TCPRedirectPort int                    `json:"tcpRedirectPort"`
	UDPTProxyPort   int                    `json:"udpTproxyPort"`
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
	LANScoped      bool   `json:"lanScoped"`
	PolicyRouting  bool   `json:"policyRouting"`
	IPv6Disabled   bool   `json:"ipv6Disabled"`
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
		Scope:           SetupInterceptionScope{Interface: setupInterceptionLANInterface, DestinationPolicy: setupInterceptionDestination, IPv6Policy: setupInterceptionIPv6Policy},
		TCPRedirectPort: setupInterceptionPort,
		UDP:             TProxyTarget{Protocol: "udp", Action: "tproxy", Port: setupInterceptionPort},
	}
}

func validSetupInterceptionGeneration(generation SetupInterceptionGeneration) bool {
	return generation.SchemaVersion == setupInterceptionSchemaVersion &&
		generation.Owner == setupInterceptionOwner &&
		generation.Generation == setupInterceptionGeneration &&
		generation.Scope.Interface == setupInterceptionLANInterface && generation.Scope.DestinationPolicy == setupInterceptionDestination && generation.Scope.IPv6Policy == setupInterceptionIPv6Policy &&
		generation.TCPRedirectPort == setupInterceptionPort &&
		generation.UDP.Protocol == "udp" && generation.UDP.Action == "tproxy" && generation.UDP.Port == setupInterceptionPort
}

func setupInterceptionPlan(generation SetupInterceptionGeneration) SetupInterceptionPlan {
	return SetupInterceptionPlan{Owner: generation.Owner, Generation: generation.Generation, Scope: generation.Scope, TCPRedirectPort: generation.TCPRedirectPort, UDPTProxyPort: generation.UDP.Port}
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
		return evidence.Generation == "" && !evidence.Complete && !evidence.LegacyHook && !evidence.LegacySchedule && !evidence.LegacyRules && !evidence.LegacyIPSets && !evidence.TCPRedirect && !evidence.UDPTProxy && !evidence.LANScoped && !evidence.PolicyRouting && !evidence.IPv6Disabled
	}
	if evidence.Owner == setupInterceptionOwner {
		return evidence.Generation == setupInterceptionGeneration && evidence.Complete && evidence.TCPRedirect && evidence.UDPTProxy && evidence.LANScoped && evidence.PolicyRouting && evidence.IPv6Disabled && !evidence.LegacyHook && !evidence.LegacySchedule && !evidence.LegacyRules && !evidence.LegacyIPSets
	}
	if evidence.Owner == "xkeen-legacy" {
		return evidence.Generation == "" && !evidence.Complete && !evidence.TCPRedirect && !evidence.UDPTProxy && !evidence.LANScoped && !evidence.PolicyRouting && !evidence.IPv6Disabled && (evidence.LegacyHook || evidence.LegacySchedule || evidence.LegacyRules || evidence.LegacyIPSets)
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
# xkeen-control-hybrid v1; source-owned; fixed LAN-only TCP redirect + UDP TProxy
set -eu

ensure_jump() {
  family="$1"
  table="$2"
  shift 2
  if ! "$family" -t "$table" -C "$@" >/dev/null 2>&1; then
    "$family" -t "$table" -A "$@"
  fi
}

iptables -t nat -L XKEEN_CONTROL_HYBRID >/dev/null 2>&1 || iptables -t nat -N XKEEN_CONTROL_HYBRID
iptables -t mangle -L XKEEN_CONTROL_HYBRID >/dev/null 2>&1 || iptables -t mangle -N XKEEN_CONTROL_HYBRID
iptables -t nat -F XKEEN_CONTROL_HYBRID
iptables -t mangle -F XKEEN_CONTROL_HYBRID
iptables -t nat -A XKEEN_CONTROL_HYBRID -i br0 -m addrtype ! --dst-type LOCAL -p tcp -m comment --comment xkeen-control-hybrid -j REDIRECT --to-ports 61219
iptables -t mangle -A XKEEN_CONTROL_HYBRID -i br0 -m addrtype ! --dst-type LOCAL -p udp -m socket --transparent -m comment --comment xkeen-control-hybrid -j MARK --set-mark 0x111/0xfff
iptables -t mangle -A XKEEN_CONTROL_HYBRID -i br0 -m addrtype ! --dst-type LOCAL -p udp -m comment --comment xkeen-control-hybrid -j TPROXY --on-ip 0.0.0.0 --on-port 61219 --tproxy-mark 0x111/0xfff
ensure_jump iptables nat PREROUTING -i br0 -m addrtype ! --dst-type LOCAL -p tcp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID
ensure_jump iptables mangle PREROUTING -i br0 -m addrtype ! --dst-type LOCAL -p udp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID

ip -4 link show dev br0 >/dev/null
if ! ip -4 rule show | grep -F "fwmark 0x111/0xfff lookup 111" >/dev/null 2>&1; then
  ip -4 rule add fwmark 0x111/0xfff table 111 pref 111
fi
if ! ip -4 route show table 111 | grep -F "local 0.0.0.0/0 dev lo" >/dev/null 2>&1; then
  ip -4 route add local 0.0.0.0/0 dev lo table 111
fi
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

// consumeReviewedLegacyAssignment accepts exactly the shell single-quoted
// assignment emitted by inject_var in the reviewed upstream S05 generator.
// The value may span lines (user_policies) and may contain only the reviewed
// quoted-shell escape sequence for an embedded quote. It is replaced by its variable
// name before the immutable hook fingerprint is calculated.
func consumeReviewedLegacyAssignment(lines []string, index int, name string) (int, bool) {
	if index >= len(lines) || !strings.HasPrefix(lines[index], name+"='") {
		return index, false
	}
	fragment := strings.TrimPrefix(lines[index], name+"='")
	for {
		for position := 0; position < len(fragment); position++ {
			if fragment[position] != '\'' {
				continue
			}
			if position+3 < len(fragment) && fragment[position:position+4] == "'\\''" {
				position += 3
				continue
			}
			if position != len(fragment)-1 {
				return index, false
			}
			return index + 1, true
		}
		index++
		if index >= len(lines) {
			return index, false
		}
		fragment = lines[index]
	}
}

func reviewedLegacyProxyHookFingerprint(contents []byte) (string, bool) {
	if len(contents) == 0 || len(contents) > setupMaxInterceptionBytes || bytes.IndexByte(contents, 0) >= 0 {
		return "", false
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\r", "\n")
	if !strings.HasSuffix(text, "\n") {
		return "", false
	}
	rawLines := strings.Split(text, "\n")
	if len(rawLines) < 3 || rawLines[0] != "#!/bin/sh" || rawLines[1] != "# "+setupReviewedLegacyHookMarker {
		return "", false
	}

	// The upstream generator removes comments and blank lines after the fixed
	// two-line header. Mirror that exact generation step before parsing the
	// dynamic assignment block and body.
	lines := make([]string, 0, len(rawLines))
	lines = append(lines, rawLines[0], rawLines[1])
	for _, line := range rawLines[2:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, line)
	}

	canonical := make([]string, 0, len(lines))
	canonical = append(canonical, lines[0], lines[1])
	index := 2
	for index < len(lines) && !strings.HasPrefix(lines[index], reviewedLegacyProxyInjectedVariables[0]+"='") {
		canonical = append(canonical, lines[index])
		index++
	}
	for _, name := range reviewedLegacyProxyInjectedVariables {
		if expected, fixed := reviewedLegacyProxyFixedAssignments[name]; fixed && (index >= len(lines) || lines[index] != name+"='"+expected+"'") {
			return "", false
		}
		var ok bool
		index, ok = consumeReviewedLegacyAssignment(lines, index, name)
		if !ok {
			return "", false
		}
		canonical = append(canonical, "__REVIEWED_ASSIGN__:"+name)
	}
	if index >= len(lines) || lines[index] != "restart_script() {" {
		return "", false
	}

	// user_policies is expanded into this heredoc by the upstream generator;
	// the reviewed hook identity covers the generator-owned block, not the
	// router's typed policy values. Keep the delimiters and canonicalize only
	// that generated data region.
	for index < len(lines) {
		line := lines[index]
		if strings.TrimSpace(line) == "done <<USER_POLICIES_EOF" {
			canonical = append(canonical, line, "__REVIEWED_USER_POLICIES__")
			index++
			for index < len(lines) && strings.TrimSpace(lines[index]) != "USER_POLICIES_EOF" {
				index++
			}
			if index >= len(lines) {
				return "", false
			}
			canonical = append(canonical, lines[index])
			index++
			continue
		}
		canonical = append(canonical, line)
		index++
	}
	canonicalBytes := []byte(strings.Join(canonical, "\n") + "\n")
	digest := sha256.Sum256(canonicalBytes)
	return hex.EncodeToString(digest[:]), true
}

func setupReviewedLegacyNetfilterHook(contents []byte) bool {
	digest, ok := reviewedLegacyProxyHookFingerprint(contents)
	return ok && digest == reviewedUpstreamProxyHookCanonicalSHA256
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
	SchemaVersion   int                    `json:"schemaVersion"`
	Owner           string                 `json:"owner"`
	Generation      string                 `json:"generation"`
	Scope           SetupInterceptionScope `json:"scope"`
	TCPRedirectPort int                    `json:"tcpRedirectPort"`
	UDPTProxyPort   int                    `json:"udpTproxyPort"`
}

func setupInterceptionStateBytes(generation SetupInterceptionGeneration) ([]byte, error) {
	if !validSetupInterceptionGeneration(generation) {
		return nil, ErrSetupInterceptionConflict
	}
	contents, err := json.Marshal(setupInterceptionStateFile{SchemaVersion: setupInterceptionSchemaVersion, Owner: generation.Owner, Generation: generation.Generation, Scope: generation.Scope, TCPRedirectPort: generation.TCPRedirectPort, UDPTProxyPort: generation.UDP.Port})
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
	if decoder.Decode(&extra) != io.EOF || state.Owner != setupInterceptionOwner || state.Generation != setupInterceptionGeneration || state.Scope != setupHybridInterceptionGeneration().Scope || state.TCPRedirectPort != setupInterceptionPort || state.UDPTProxyPort != setupInterceptionPort {
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
		evidence.LANScoped = true
		evidence.PolicyRouting = true
		evidence.IPv6Disabled = true
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

type nativeOwnedChain struct {
	Family string `json:"family"`
	Table  string `json:"table"`
	Name   string `json:"name"`
}

type nativeOwnedRule struct {
	Family string   `json:"family"`
	Table  string   `json:"table"`
	Chain  string   `json:"chain"`
	Args   []string `json:"args"`
}

type nativeOwnedIPSet struct {
	Name       string     `json:"name"`
	CreateArgs []string   `json:"createArgs"`
	Entries    [][]string `json:"entries,omitempty"`
}

type nativeOwnedPolicyEntry struct {
	Family string   `json:"family"`
	Kind   string   `json:"kind"`
	Args   []string `json:"args"`
}

// nativeOwnedInterception is deliberately an owned subset, never a save-file
// image. Restore can therefore add/delete only these typed objects and leave
// unrelated operator/Keenetic firewall state in place.
type nativeOwnedInterception struct {
	Chains []nativeOwnedChain       `json:"chains,omitempty"`
	Rules  []nativeOwnedRule        `json:"rules,omitempty"`
	IPSets []nativeOwnedIPSet       `json:"ipsets,omitempty"`
	Policy []nativeOwnedPolicyEntry `json:"policy,omitempty"`
}

type nativeHybridSnapshot struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Evidence      SetupInterceptionEvidence `json:"evidence"`
	Owned         nativeOwnedInterception   `json:"owned"`
}

type nativeInspection struct {
	Evidence SetupInterceptionEvidence
	Owned    nativeOwnedInterception
}

var reviewedLegacyIPSetNames = map[string]bool{
	"xkeen_deny_mac": true,
	"ext_exclude":    true,
	"ext_exclude6":   true,
	"geo_exclude":    true,
	"geo_exclude6":   true,
	"geo_override":   true,
	"geo_override6":  true,
	"user_exclude":   true,
	"user_exclude6":  true,
}

func nativeTokenValid(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n;&|`$<>\\")
}

func nativeFamilyValid(value string) bool { return value == "iptables" || value == "ip6tables" }

func nativeTableValid(value string) bool {
	return value == "nat" || value == "mangle" || value == "filter" || value == "raw" || value == "security"
}

func validNativeOwned(owned nativeOwnedInterception) bool {
	if len(owned.Chains) > 16 || len(owned.Rules) > 128 || len(owned.IPSets) > len(reviewedLegacyIPSetNames) || len(owned.Policy) > 8 {
		return false
	}
	for _, chain := range owned.Chains {
		if !nativeFamilyValid(chain.Family) || !nativeTableValid(chain.Table) || !nativeTokenValid(chain.Name) {
			return false
		}
	}
	for _, rule := range owned.Rules {
		if !nativeFamilyValid(rule.Family) || !nativeTableValid(rule.Table) || !nativeTokenValid(rule.Chain) || len(rule.Args) == 0 || len(rule.Args) > 64 {
			return false
		}
		for _, arg := range rule.Args {
			if !nativeTokenValid(arg) {
				return false
			}
		}
	}
	for _, set := range owned.IPSets {
		if !reviewedLegacyIPSetNames[set.Name] || len(set.CreateArgs) == 0 || len(set.CreateArgs) > 32 || len(set.Entries) > 4096 {
			return false
		}
		for _, arg := range set.CreateArgs {
			if !nativeTokenValid(arg) {
				return false
			}
		}
		for _, entry := range set.Entries {
			if len(entry) == 0 || len(entry) > 32 {
				return false
			}
			for _, arg := range entry {
				if !nativeTokenValid(arg) {
					return false
				}
			}
		}
	}
	for _, entry := range owned.Policy {
		if (entry.Family != "ipv4" && entry.Family != "ipv6") || (entry.Kind != "rule" && entry.Kind != "route") || len(entry.Args) == 0 || len(entry.Args) > 16 {
			return false
		}
		for _, arg := range entry.Args {
			if !nativeTokenValid(arg) {
				return false
			}
		}
	}
	return true
}

func nativeArgsEqual(actual []string, expected ...string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		got := strings.ToLower(strings.Trim(actual[index], "\"'"))
		want := strings.ToLower(expected[index])
		switch got {
		case "--to-port":
			got = "--to-ports"
		case "--set-mark":
			got = "--set-xmark"
		}
		if strings.HasPrefix(got, "0x111/0xfff/") {
			got = "0x111/0xfff"
		}
		if got != want {
			return false
		}
	}
	return true
}

func nativeSourceRuleKind(table, chain string, args []string) string {
	scope := []string{"-i", setupInterceptionLANInterface, "-m", "addrtype", "!", "--dst-type", "LOCAL"}
	if table == "nat" && chain == "PREROUTING" && nativeArgsEqual(args, append(scope, "-p", "tcp", "-m", "comment", "--comment", setupInterceptionHookMarker, "-j", "XKEEN_CONTROL_HYBRID")...) {
		return "nat-jump"
	}
	if table == "nat" && chain == "XKEEN_CONTROL_HYBRID" && nativeArgsEqual(args, append(scope, "-p", "tcp", "-m", "comment", "--comment", setupInterceptionHookMarker, "-j", "REDIRECT", "--to-ports", "61219")...) {
		return "nat-rule"
	}
	if table == "mangle" && chain == "PREROUTING" && nativeArgsEqual(args, append(scope, "-p", "udp", "-m", "comment", "--comment", setupInterceptionHookMarker, "-j", "XKEEN_CONTROL_HYBRID")...) {
		return "mangle-jump"
	}
	if table == "mangle" && chain == "XKEEN_CONTROL_HYBRID" && nativeArgsEqual(args, append(scope, "-p", "udp", "-m", "socket", "--transparent", "-m", "comment", "--comment", setupInterceptionHookMarker, "-j", "MARK", "--set-xmark", setupInterceptionMark)...) {
		return "mark-rule"
	}
	if table == "mangle" && chain == "XKEEN_CONTROL_HYBRID" && nativeArgsEqual(args, append(scope, "-p", "udp", "-m", "comment", "--comment", setupInterceptionHookMarker, "-j", "TPROXY", "--on-ip", "0.0.0.0", "--on-port", "61219", "--tproxy-mark", setupInterceptionMark)...) {
		return "tproxy-rule"
	}
	return ""
}

func nativeArgsContainMarker(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(strings.Trim(arg, "\"'"), setupInterceptionHookMarker) {
			return true
		}
	}
	return false
}

func nativeLegacyRule(args []string, chain string) bool {
	if chain == "xkeen" || chain == "xkeen_force" {
		return true
	}
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "-j" && (strings.EqualFold(args[index+1], "xkeen") || strings.EqualFold(args[index+1], "xkeen_force")) {
			return true
		}
		if args[index] == "--comment" && strings.EqualFold(strings.Trim(args[index+1], "\"'"), "xkeen_rule") {
			return true
		}
	}
	return false
}

func nativeContainsXkeen(args []string) bool {
	for _, arg := range args {
		if strings.Contains(strings.ToLower(strings.Trim(arg, "\"'")), "xkeen") {
			return true
		}
	}
	return false
}

func appendNativeRule(owned *nativeOwnedInterception, family, table, chain string, args []string) {
	owned.Rules = append(owned.Rules, nativeOwnedRule{Family: family, Table: table, Chain: chain, Args: append([]string(nil), args...)})
}

func appendNativeChain(owned *nativeOwnedInterception, family, table, name string) {
	for _, existing := range owned.Chains {
		if existing.Family == family && existing.Table == table && existing.Name == name {
			return
		}
	}
	owned.Chains = append(owned.Chains, nativeOwnedChain{Family: family, Table: table, Name: name})
}

func parseNativeIPTables(family string, contents []byte, owned *nativeOwnedInterception, source map[string]int, legacy *bool) error {
	table := ""
	for _, raw := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "*") {
			table = strings.TrimPrefix(line, "*")
			if !nativeTableValid(table) {
				return ErrSetupInterceptionConflict
			}
			continue
		}
		if line == "COMMIT" {
			table = ""
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || table == "" {
			return ErrSetupInterceptionConflict
		}
		if strings.HasPrefix(fields[0], ":") {
			name := strings.TrimPrefix(fields[0], ":")
			if name == "XKEEN_CONTROL_HYBRID" {
				if family != "iptables" || (table != "nat" && table != "mangle") || len(fields) != 3 || fields[1] != "-" || fields[2] != "[0:0]" {
					return ErrSetupInterceptionConflict
				}
				appendNativeChain(owned, family, table, name)
				source[family+":"+table+":chain"]++
				continue
			}
			if name == "xkeen" || name == "xkeen_force" {
				if len(fields) != 3 || fields[1] != "-" || fields[2] != "[0:0]" {
					return ErrSetupInterceptionConflict
				}
				appendNativeChain(owned, family, table, name)
				*legacy = true
				continue
			}
			if strings.Contains(strings.ToLower(name), "xkeen") {
				return ErrSetupInterceptionConflict
			}
			continue
		}
		if fields[0] != "-A" || len(fields) < 3 || !nativeTokenValid(fields[1]) {
			return ErrSetupInterceptionConflict
		}
		chain := fields[1]
		args := fields[2:]
		for _, arg := range args {
			if !nativeTokenValid(arg) {
				return ErrSetupInterceptionConflict
			}
		}
		kind := nativeSourceRuleKind(table, chain, args)
		if kind != "" {
			if family != "iptables" {
				return ErrSetupInterceptionConflict
			}
			if source[kind] != 0 {
				return ErrSetupInterceptionConflict
			}
			source[kind]++
			appendNativeRule(owned, family, table, chain, args)
			continue
		}
		if chain == "XKEEN_CONTROL_HYBRID" || nativeArgsContainMarker(args) || strings.Contains(strings.ToLower(line), "xkeen_control_hybrid") {
			return ErrSetupInterceptionConflict
		}
		if nativeLegacyRule(args, chain) {
			appendNativeRule(owned, family, table, chain, args)
			*legacy = true
			continue
		}
		if nativeContainsXkeen(args) {
			return ErrSetupInterceptionConflict
		}
	}
	return nil
}

func parseNativeIPSets(contents []byte, owned *nativeOwnedInterception, legacy *bool) error {
	setIndex := make(map[string]int)
	for _, raw := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		fields := strings.Fields(strings.TrimSpace(raw))
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		for _, field := range fields {
			if !nativeTokenValid(field) {
				return ErrSetupInterceptionConflict
			}
		}
		switch fields[0] {
		case "create":
			if len(fields) < 3 {
				return ErrSetupInterceptionConflict
			}
			name := fields[1]
			if !reviewedLegacyIPSetNames[name] {
				if strings.Contains(strings.ToLower(name), "xkeen") {
					return ErrSetupInterceptionConflict
				}
				continue
			}
			if _, exists := setIndex[name]; exists {
				return ErrSetupInterceptionConflict
			}
			setIndex[name] = len(owned.IPSets)
			owned.IPSets = append(owned.IPSets, nativeOwnedIPSet{Name: name, CreateArgs: append([]string(nil), fields[2:]...)})
			*legacy = true
		case "add":
			if len(fields) < 3 || !reviewedLegacyIPSetNames[fields[1]] {
				if len(fields) > 1 && strings.Contains(strings.ToLower(fields[1]), "xkeen") {
					return ErrSetupInterceptionConflict
				}
				continue
			}
			index, exists := setIndex[fields[1]]
			if !exists {
				return ErrSetupInterceptionConflict
			}
			owned.IPSets[index].Entries = append(owned.IPSets[index].Entries, append([]string(nil), fields[2:]...))
		default:
			if strings.Contains(strings.ToLower(fields[0]), "xkeen") {
				return ErrSetupInterceptionConflict
			}
		}
	}
	return nil
}

func nativePolicyExact(contents []byte, kind string, family string) (nativeOwnedPolicyEntry, bool, error) {
	var entry nativeOwnedPolicyEntry
	found := false
	for _, raw := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		for _, field := range fields {
			if !nativeTokenValid(field) {
				return nativeOwnedPolicyEntry{}, false, ErrSetupInterceptionConflict
			}
		}
		if kind == "rule" {
			if len(fields) > 0 && strings.HasSuffix(fields[0], ":") {
				fields = fields[1:]
			}
			marker := false
			for _, field := range fields {
				if strings.EqualFold(field, setupInterceptionMark) || strings.EqualFold(field, setupInterceptionTable) {
					marker = true
				}
			}
			if marker && !nativeArgsEqual(fields, "from", "all", "fwmark", setupInterceptionMark, "lookup", setupInterceptionTable) {
				return nativeOwnedPolicyEntry{}, false, ErrSetupInterceptionConflict
			}
			if nativeArgsEqual(fields, "from", "all", "fwmark", setupInterceptionMark, "lookup", setupInterceptionTable) {
				if found {
					return nativeOwnedPolicyEntry{}, false, ErrSetupInterceptionConflict
				}
				entry = nativeOwnedPolicyEntry{Family: family, Kind: kind, Args: []string{"fwmark", setupInterceptionMark, "table", setupInterceptionTable, "pref", setupInterceptionPriority}}
				found = true
			}
		} else {
			prefix := "0.0.0.0/0"
			if family == "ipv6" {
				prefix = "::/0"
			}
			marker := false
			for _, field := range fields {
				if strings.EqualFold(field, prefix) || strings.EqualFold(field, "lo") || strings.EqualFold(field, "111") {
					marker = true
				}
			}
			if marker && !nativeArgsEqualPrefix(fields, "local", prefix, "dev", "lo") {
				return nativeOwnedPolicyEntry{}, false, ErrSetupInterceptionConflict
			}
			if nativeArgsEqualPrefix(fields, "local", prefix, "dev", "lo") {
				if found {
					return nativeOwnedPolicyEntry{}, false, ErrSetupInterceptionConflict
				}
				entry = nativeOwnedPolicyEntry{Family: family, Kind: kind, Args: []string{"local", prefix, "dev", "lo", "table", setupInterceptionTable}}
				found = true
			}
		}
	}
	return entry, found, nil
}

func nativeArgsEqualPrefix(actual []string, expected ...string) bool {
	if len(actual) < len(expected) {
		return false
	}
	for index := range expected {
		if strings.ToLower(strings.Trim(actual[index], "\"'")) != strings.ToLower(expected[index]) {
			return false
		}
	}
	return true
}

func nativeLinkProven(contents []byte) bool {
	for _, raw := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if strings.Contains(line, setupInterceptionLANInterface+":") {
			return true
		}
	}
	return false
}

// parseNativeInterceptionState is pure and deterministic. It is used by the
// native adapter and by fixtures so parser/verification regressions do not
// require a router or any real firewall mutation.
func parseNativeInterceptionState(ipv4, ipv6, ipsets, policyRules, policyRoutes, policyRules6, policyRoutes6, link []byte) (SetupInterceptionEvidence, nativeOwnedInterception, error) {
	owned := nativeOwnedInterception{}
	source := make(map[string]int)
	legacy := false
	if err := parseNativeIPTables("iptables", ipv4, &owned, source, &legacy); err != nil {
		return SetupInterceptionEvidence{}, nativeOwnedInterception{}, err
	}
	if err := parseNativeIPTables("ip6tables", ipv6, &owned, source, &legacy); err != nil {
		return SetupInterceptionEvidence{}, nativeOwnedInterception{}, err
	}
	if err := parseNativeIPSets(ipsets, &owned, &legacy); err != nil {
		return SetupInterceptionEvidence{}, nativeOwnedInterception{}, err
	}
	rule, ruleFound, err := nativePolicyExact(policyRules, "rule", "ipv4")
	if err != nil {
		return SetupInterceptionEvidence{}, nativeOwnedInterception{}, err
	}
	if ruleFound {
		owned.Policy = append(owned.Policy, rule)
		if source["nat-jump"] != 0 || source["mangle-jump"] != 0 {
			// The policy entry is part of the source generation; it is not a
			// free-standing generic mark that can be silently adopted.
		} else {
			legacy = true
		}
	}
	route, routeFound, err := nativePolicyExact(policyRoutes, "route", "ipv4")
	if err != nil {
		return SetupInterceptionEvidence{}, nativeOwnedInterception{}, err
	}
	if routeFound {
		owned.Policy = append(owned.Policy, route)
		if source["nat-jump"] == 0 && source["mangle-jump"] == 0 {
			legacy = true
		}
	}
	rule6, rule6Found, err := nativePolicyExact(policyRules6, "rule", "ipv6")
	if err != nil {
		return SetupInterceptionEvidence{}, nativeOwnedInterception{}, err
	}
	if rule6Found {
		owned.Policy = append(owned.Policy, rule6)
		legacy = true
	}
	route6, route6Found, err := nativePolicyExact(policyRoutes6, "route", "ipv6")
	if err != nil {
		return SetupInterceptionEvidence{}, nativeOwnedInterception{}, err
	}
	if route6Found {
		owned.Policy = append(owned.Policy, route6)
		legacy = true
	}
	if source["nat-jump"] != 0 || source["nat-rule"] != 0 || source["mangle-jump"] != 0 || source["mark-rule"] != 0 || source["tproxy-rule"] != 0 || source["iptables:nat:chain"] != 0 || source["iptables:mangle:chain"] != 0 {
		if source["iptables:nat:chain"] != 1 || source["iptables:mangle:chain"] != 1 || source["nat-jump"] != 1 || source["nat-rule"] != 1 || source["mangle-jump"] != 1 || source["mark-rule"] != 1 || source["tproxy-rule"] != 1 || !ruleFound || !routeFound || !nativeLinkProven(link) {
			return SetupInterceptionEvidence{}, nativeOwnedInterception{}, ErrSetupInterceptionConflict
		}
		for _, chain := range owned.Chains {
			if chain.Name == "xkeen" || chain.Name == "xkeen_force" {
				return SetupInterceptionEvidence{}, nativeOwnedInterception{}, ErrSetupInterceptionConflict
			}
		}
		if legacy {
			return SetupInterceptionEvidence{}, nativeOwnedInterception{}, ErrSetupInterceptionConflict
		}
		evidence := finalizeSetupInterceptionEvidence(SetupInterceptionEvidence{Owner: setupInterceptionOwner, Generation: setupInterceptionGeneration, TCPRedirect: true, UDPTProxy: true, LANScoped: true, PolicyRouting: true, IPv6Disabled: true, Complete: true})
		return evidence, owned, nil
	}
	if legacy {
		evidence := finalizeSetupInterceptionEvidence(SetupInterceptionEvidence{Owner: "xkeen-legacy", LegacyRules: len(owned.Rules) != 0 || len(owned.Chains) != 0 || len(owned.Policy) != 0, LegacyIPSets: len(owned.IPSets) != 0})
		return evidence, owned, nil
	}
	return finalizeSetupInterceptionEvidence(SetupInterceptionEvidence{}), owned, nil
}

func optionalNativeOutput(ctx context.Context, name string, args ...string) []byte {
	contents, err := runFixedOutput(ctx, name, args...)
	if err != nil {
		return nil
	}
	return contents
}

func inspectNativeState(ctx context.Context) (nativeInspection, error) {
	ipv4, err := runFixedOutput(ctx, "iptables-save")
	if err != nil {
		return nativeInspection{}, err
	}
	evidence, owned, err := parseNativeInterceptionState(ipv4, optionalNativeOutput(ctx, "ip6tables-save"), optionalNativeOutput(ctx, "ipset", "save"), optionalNativeOutput(ctx, "ip", "-4", "rule", "show"), optionalNativeOutput(ctx, "ip", "-4", "route", "show", "table", setupInterceptionTable), optionalNativeOutput(ctx, "ip", "-6", "rule", "show"), optionalNativeOutput(ctx, "ip", "-6", "route", "show", "table", setupInterceptionTable), optionalNativeOutput(ctx, "ip", "-4", "link", "show", "dev", setupInterceptionLANInterface))
	if err != nil {
		return nativeInspection{}, err
	}
	return nativeInspection{Evidence: evidence, Owned: owned}, nil
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
		return SetupInterceptionEvidence{}, ErrSetupInterceptionConflict
	}
	evidence := fileEvidence
	if evidence.Owner == "" {
		evidence = ruleEvidence
	} else if ruleEvidence.Owner != "" {
		evidence.TCPRedirect = evidence.TCPRedirect && ruleEvidence.TCPRedirect
		evidence.UDPTProxy = evidence.UDPTProxy && ruleEvidence.UDPTProxy
		evidence.LANScoped = evidence.LANScoped && ruleEvidence.LANScoped
		evidence.PolicyRouting = evidence.PolicyRouting && ruleEvidence.PolicyRouting
		evidence.IPv6Disabled = evidence.IPv6Disabled && ruleEvidence.IPv6Disabled
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
	native, err := inspectNativeState(ctx)
	if err != nil {
		return SetupInterceptionEvidence{}, err
	}
	return mergeNativeInterceptionEvidence(fileEvidence, native.Evidence)
}

func nativeOwnedDigest(owned nativeOwnedInterception) string {
	contents, _ := json.Marshal(owned)
	return digestSetupBytes(contents)
}

func parseNativeHybridSnapshot(contents []byte) (nativeHybridSnapshot, error) {
	if len(contents) == 0 || len(contents) > setupMaxInterceptionSnapshotBytes {
		return nativeHybridSnapshot{}, ErrSetupInterceptionConflict
	}
	var snapshot nativeHybridSnapshot
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&snapshot) != nil || decoder.Decode(&extra) != io.EOF || snapshot.SchemaVersion != setupInterceptionSchemaVersion || !validSetupInterceptionEvidence(snapshot.Evidence) || !validNativeOwned(snapshot.Owned) {
		return nativeHybridSnapshot{}, ErrSetupInterceptionConflict
	}
	if snapshot.Evidence.Owner == "" && (len(snapshot.Owned.Chains) != 0 || len(snapshot.Owned.Rules) != 0 || len(snapshot.Owned.IPSets) != 0 || len(snapshot.Owned.Policy) != 0) {
		return nativeHybridSnapshot{}, ErrSetupInterceptionConflict
	}
	return snapshot, nil
}

func setupInterceptionSnapshotEvidence(contents []byte) (SetupInterceptionEvidence, error) {
	if snapshot, err := parseNativeHybridSnapshot(contents); err == nil {
		return snapshot.Evidence, nil
	}
	return parseSetupInterceptionSnapshot(contents)
}

func (o *nativeHybridInterceptionOwner) Snapshot(ctx context.Context) ([]byte, error) {
	native, err := inspectNativeState(ctx)
	if err != nil {
		return nil, err
	}
	fileEvidence, err := o.file.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	evidence, err := mergeNativeInterceptionEvidence(fileEvidence, native.Evidence)
	if err != nil {
		return nil, err
	}
	contents, err := json.Marshal(nativeHybridSnapshot{SchemaVersion: setupInterceptionSchemaVersion, Evidence: evidence, Owned: native.Owned})
	if err != nil || len(contents) > setupMaxInterceptionSnapshotBytes {
		return nil, ErrSetupInterceptionConflict
	}
	return append(contents, '\n'), nil
}

func nativePolicyCommandArgs(entry nativeOwnedPolicyEntry, action string) ([]string, error) {
	if !validNativeOwned(nativeOwnedInterception{Policy: []nativeOwnedPolicyEntry{entry}}) {
		return nil, ErrSetupInterceptionConflict
	}
	family := "-4"
	if entry.Family == "ipv6" {
		family = "-6"
	}
	args := []string{family}
	if entry.Kind == "rule" {
		args = append(args, "rule", action)
	} else {
		args = append(args, "route", action)
	}
	args = append(args, entry.Args...)
	return args, nil
}

func removeNativeOwned(ctx context.Context, owned nativeOwnedInterception) error {
	if !validNativeOwned(owned) {
		return ErrSetupInterceptionConflict
	}
	// Delete entry-point jumps before chain members, then delete members and
	// finally chains. This never flushes a built-in chain.
	for index := len(owned.Rules) - 1; index >= 0; index-- {
		rule := owned.Rules[index]
		if rule.Chain != "PREROUTING" {
			continue
		}
		if err := runFixed(ctx, rule.Family, append([]string{"-t", rule.Table, "-D", rule.Chain}, rule.Args...)...); err != nil {
			return err
		}
	}
	for index := len(owned.Rules) - 1; index >= 0; index-- {
		rule := owned.Rules[index]
		if rule.Chain == "PREROUTING" {
			continue
		}
		if err := runFixed(ctx, rule.Family, append([]string{"-t", rule.Table, "-D", rule.Chain}, rule.Args...)...); err != nil {
			return err
		}
	}
	for index := len(owned.Policy) - 1; index >= 0; index-- {
		args, err := nativePolicyCommandArgs(owned.Policy[index], "del")
		if err != nil {
			return err
		}
		if err := runFixed(ctx, "ip", args...); err != nil {
			return err
		}
	}
	for index := len(owned.IPSets) - 1; index >= 0; index-- {
		set := owned.IPSets[index]
		if err := runFixed(ctx, "ipset", "destroy", set.Name); err != nil {
			return err
		}
	}
	for index := len(owned.Chains) - 1; index >= 0; index-- {
		chain := owned.Chains[index]
		if err := runFixed(ctx, chain.Family, "-t", chain.Table, "-X", chain.Name); err != nil {
			return err
		}
	}
	return nil
}

func restoreNativeOwned(ctx context.Context, owned nativeOwnedInterception) error {
	if !validNativeOwned(owned) {
		return ErrSetupInterceptionConflict
	}
	for _, set := range owned.IPSets {
		if err := runFixed(ctx, "ipset", append([]string{"create", set.Name}, append(set.CreateArgs, "-exist")...)...); err != nil {
			return err
		}
		for _, entry := range set.Entries {
			if err := runFixed(ctx, "ipset", append([]string{"add", set.Name}, entry...)...); err != nil {
				return err
			}
		}
	}
	for _, chain := range owned.Chains {
		if err := runFixed(ctx, chain.Family, "-t", chain.Table, "-N", chain.Name); err != nil {
			return err
		}
	}
	for _, rule := range owned.Rules {
		if err := runFixed(ctx, rule.Family, append([]string{"-t", rule.Table, "-A", rule.Chain}, rule.Args...)...); err != nil {
			return err
		}
	}
	for _, policy := range owned.Policy {
		args, err := nativePolicyCommandArgs(policy, "add")
		if err != nil {
			return err
		}
		if err := runFixed(ctx, "ip", args...); err != nil {
			return err
		}
	}
	return nil
}

func (o *nativeHybridInterceptionOwner) RetireLegacy(ctx context.Context, evidence SetupInterceptionEvidence) error {
	if evidence.Owner == "" {
		return nil
	}
	if evidence.Owner != "xkeen-legacy" || !validSetupInterceptionEvidence(evidence) {
		return ErrSetupInterceptionConflict
	}
	native, err := inspectNativeState(ctx)
	if err != nil {
		return err
	}
	if native.Evidence.Owner == setupInterceptionOwner {
		return ErrSetupInterceptionConflict
	}
	if native.Evidence.Owner == "xkeen-legacy" {
		if err := removeNativeOwned(ctx, native.Owned); err != nil {
			return err
		}
	}
	return o.file.RetireLegacy(ctx, evidence)
}

func (o *nativeHybridInterceptionOwner) Apply(ctx context.Context, generation SetupInterceptionGeneration) error {
	if !validSetupInterceptionGeneration(generation) {
		return ErrSetupInterceptionConflict
	}
	current, err := inspectNativeState(ctx)
	if err != nil {
		return err
	}
	if current.Evidence.Owner == "xkeen-legacy" {
		return ErrSetupInterceptionConflict
	}
	if _, err := runFixedOutput(ctx, "ip", "-4", "link", "show", "dev", setupInterceptionLANInterface); err != nil {
		return err
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
	if err != nil || evidence.Owner != setupInterceptionOwner || !evidence.Complete || !evidence.TCPRedirect || !evidence.UDPTProxy || !evidence.LANScoped || !evidence.PolicyRouting || !evidence.IPv6Disabled {
		return ErrSetupInterceptionConflict
	}
	return nil
}

func (o *nativeHybridInterceptionOwner) Restore(ctx context.Context, snapshot []byte) error {
	current, err := inspectNativeState(ctx)
	if err != nil {
		return err
	}
	if len(snapshot) == 0 {
		if current.Evidence.Owner == "xkeen-legacy" {
			return ErrSetupInterceptionConflict
		}
		if current.Evidence.Owner == setupInterceptionOwner {
			if err := removeNativeOwned(ctx, current.Owned); err != nil {
				return err
			}
		}
		return o.file.Restore(ctx, nil)
	}
	previous, err := parseNativeHybridSnapshot(snapshot)
	if err != nil {
		return err
	}
	if current.Evidence.Owner == "xkeen-legacy" {
		return ErrSetupInterceptionConflict
	}
	if current.Evidence.Owner == setupInterceptionOwner {
		if err := removeNativeOwned(ctx, current.Owned); err != nil {
			return err
		}
	}
	if err := restoreNativeOwned(ctx, previous.Owned); err != nil {
		return err
	}
	return o.file.Restore(ctx, setupInterceptionSnapshotEvidenceBytes(previous.Evidence))
}

func setupInterceptionSnapshotEvidenceBytes(evidence SetupInterceptionEvidence) []byte {
	contents, _ := setupInterceptionSnapshotBytes(evidence)
	return contents
}

func (o *nativeHybridInterceptionOwner) VerifyRestored(ctx context.Context, snapshot []byte) error {
	if len(snapshot) == 0 {
		current, err := o.Inspect(ctx)
		if err != nil || current.Owner != "" {
			return ErrSetupInterceptionConflict
		}
		return o.file.VerifyRestored(ctx, nil)
	}
	previous, err := parseNativeHybridSnapshot(snapshot)
	if err != nil {
		return err
	}
	native, err := inspectNativeState(ctx)
	if err != nil {
		return err
	}
	current, err := o.Inspect(ctx)
	if err != nil || current.Digest != previous.Evidence.Digest || nativeOwnedDigest(native.Owned) != nativeOwnedDigest(previous.Owned) {
		return ErrSetupInterceptionConflict
	}
	return o.file.VerifyRestored(ctx, setupInterceptionSnapshotEvidenceBytes(previous.Evidence))
}
