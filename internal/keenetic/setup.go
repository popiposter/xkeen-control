// Package keenetic contains the fixed firmware adapter for explicit fresh setup.
// It has no shell, arbitrary command, RCI POST or public HTTP surface.
package keenetic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrCapability = errors.New("unsupported or incomplete Keenetic setup capability")
	ErrUnknown    = errors.New("firmware result unknown; inspect without replay")
	identifier    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
	interfaceID   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,48}(?:/[A-Za-z0-9_.-]{1,14})?$`)
	// Aliases are data-only readback names and may start with a digit. Commands
	// always use the separately validated canonical interface ID.
	interfaceAlias = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,64}$`)
	ansi           = regexp.MustCompile(`\x1b\[[0-9;]*[Km]`)
)

type command uint8

const (
	version command = iota
	interfaces
	policies
	running
	startup
	dnsRuntime
	mutation
)

type Adapter struct {
	run func(context.Context, command, string) ([]byte, error)
}

func New() *Adapter { return &Adapter{run: execute} }

type Policy struct {
	ID, Description, WAN string
	Mark                 uint32
	Table                uint64
}
type Snapshot struct {
	Home, WAN               string
	Address                 netip.Addr
	Policies                map[string]Policy
	Profiles                map[int]bool
	HomePolicy, HomeProfile string
	Engine                  string
	// Private complete firmware data never enters a public status/log/export.
	config []configLine
	Hash   string
}
type Plan struct {
	Home, WAN, PolicyID string
	Address             netip.Addr
	ProfileID           int
}
type Intent struct {
	Kind, BeforeHash, Home, PolicyID string
	AfterHash                        string
	ProfileID                        int
}
type Persist func(Intent) error

type configLine struct{ words []string }

func cleanText(b []byte) (string, error) {
	if len(b) == 0 || len(b) > 1<<20 {
		return "", ErrCapability
	}
	s := ansi.ReplaceAllString(string(b), "")
	if strings.ContainsAny(s, "\x00\x1b") || strings.Contains(s, "--More--") || strings.Contains(s, "Command::") {
		return "", ErrCapability
	}
	return strings.ReplaceAll(s, "\r\n", "\n"), nil
}

// A config read must have its native file header and final separator. Relevant
// nested commands are flattened from indentation, never executed from text.
func parseConfig(b []byte) ([]configLine, error) {
	s, e := cleanText(b)
	if e != nil {
		return nil, e
	}
	if !strings.HasPrefix(strings.TrimSpace(s), "! $$$") || !strings.HasSuffix(strings.TrimSpace(s), "!") {
		return nil, ErrCapability
	}
	var out []configLine
	var parent []string
	for _, line := range strings.Split(s, "\n") {
		if len(line) > 8192 {
			return nil, ErrCapability
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "!") {
			parent = nil
			continue
		}
		if strings.ContainsRune(line, '\t') {
			return nil, ErrCapability
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		// Only these sections are inspected. Unknown configuration elsewhere is
		// preserved; unknown relevant grammar is rejected in project().
		w := strings.Fields(trim)
		if indent == 0 {
			parent = w
			out = append(out, configLine{w})
			continue
		}
		if indent != 4 || len(parent) == 0 {
			return nil, ErrCapability
		}
		out = append(out, configLine{append(append([]string{}, parent...), w...)})
	}
	if len(out) < 4 || len(out) > 16384 {
		return nil, ErrCapability
	}
	return out, nil
}

func project(lines []configLine) (Snapshot, error) {
	// Normalize only verified interface aliases; firmware serializes assignments
	// by rename while show output may use canonical IDs. Duplicate aliases fail.
	aliases := map[string]string{}
	for _, l := range lines {
		w := l.words
		if len(w) >= 2 && w[0] == "interface" {
			aliases[w[1]] = w[1]
		}
	}
	for _, l := range lines {
		w := l.words
		if len(w) == 4 && w[0] == "interface" && w[2] == "rename" {
			name := strings.Trim(w[3], `"`)
			if !interfaceAlias.MatchString(name) {
				return Snapshot{}, ErrCapability
			}
			if old, ok := aliases[name]; ok && old != w[1] {
				return Snapshot{}, ErrCapability
			}
			aliases[name] = w[1]
		}
	}
	normalized := make([]configLine, len(lines))
	for i, l := range lines {
		w := append([]string(nil), l.words...)
		at := -1
		if len(w) == 6 && w[0] == "ip" && w[1] == "policy" && w[3] == "permit" && w[4] == "global" {
			at = 5
		}
		if len(w) == 5 && w[0] == "ip" && w[1] == "hotspot" && w[2] == "policy" {
			at = 3
		}
		if len(w) == 7 && w[0] == "dns-proxy" && w[1] == "filter" && w[2] == "assign" && w[3] == "interface" {
			at = 5
		}
		if at >= 0 {
			v, ok := aliases[w[at]]
			if !ok {
				return Snapshot{}, ErrCapability
			}
			w[at] = v
		}
		normalized[i] = configLine{w}
	}
	lines = normalized
	s := Snapshot{Policies: map[string]Policy{}, Profiles: map[int]bool{}}
	privateBridges := map[string]bool{}
	addresses := map[string]netip.Addr{}
	wans := map[string]bool{}
	for _, l := range lines {
		w := l.words
		if len(w) == 0 {
			continue
		}
		if w[0] == "interface" && len(w) >= 2 {
			id := w[1]
			if !interfaceID.MatchString(id) {
				return s, ErrCapability
			}
			if len(w) == 4 && w[2] == "security-level" && w[3] == "private" {
				privateBridges[id] = true
			}
			if len(w) >= 4 && w[2] == "ip" && w[3] == "global" {
				wans[id] = true
			}
			if len(w) == 6 && w[2] == "ip" && w[3] == "address" {
				a, e := netip.ParseAddr(w[4])
				mask, e2 := netip.ParseAddr(w[5])
				if e == nil && e2 == nil && a.Is4() && mask.Is4() && a.IsPrivate() {
					addresses[id] = a
				}
			}
		}
		if len(w) >= 2 && w[0] == "ip" && w[1] == "policy" {
			if len(w) < 3 || !identifier.MatchString(w[2]) {
				return s, ErrCapability
			}
			p := s.Policies[w[2]]
			p.ID = w[2]
			if len(w) > 3 {
				switch w[3] {
				case "description":
					p.Description = strings.Trim(strings.Join(w[4:], " "), `"`)
				case "permit":
					if len(w) != 6 || w[4] != "global" || !interfaceID.MatchString(w[5]) || p.WAN != "" {
						return s, ErrCapability
					}
					p.WAN = w[5]
				default:
					return s, ErrCapability
				}
			}
			s.Policies[p.ID] = p
		}
		if len(w) >= 2 && w[0] == "ip" && w[1] == "hotspot" && len(w) > 2 {
			switch w[2] {
			case "policy":
				if len(w) != 5 || !identifier.MatchString(w[3]) || !identifier.MatchString(w[4]) {
					return s, ErrCapability
				}
			case "host":
				// No offline membership guess: any host-specific policy/schedule/
				// conform/access exception makes this initial scope unsupported.
				// Even an explicit permit excludes a host from the segment policy.
				// This initial scope does not atomically rewrite per-host rules.
				return s, ErrCapability
			case "default":
				if len(w) != 4 || w[3] != "permit" {
					return s, ErrCapability
				}
			case "auto-register":
				if len(w) != 3 {
					return s, ErrCapability
				}
			default:
				return s, ErrCapability
			}
		}
		if w[0] == "dns-proxy" && len(w) > 1 && w[1] == "filter" {
			if len(w) < 3 {
				return s, ErrCapability
			}
			switch w[2] {
			case "engine":
				if len(w) != 4 || w[3] != "public" {
					return s, ErrCapability
				}
				s.Engine = "public"
			case "profile":
				if len(w) < 4 {
					return s, ErrCapability
				}
				n, e := strconv.Atoi(w[3])
				if e != nil || n < 1 || n > 8 {
					return s, ErrCapability
				}
				s.Profiles[n] = true
				if len(w) > 4 {
					switch w[4] {
					case "description":
						if len(w) != 6 || w[5] != "xkeen-control" {
							return s, ErrCapability
						}
					case "dns53":
						if !(len(w) == 7 && w[5] == "upstream" && validUpstream(w[6])) {
							return s, ErrCapability
						}
					default:
						return s, ErrCapability
					}
				}
			case "assign":
				if len(w) != 7 || w[3] != "interface" || w[4] != "profile" {
					return s, ErrCapability
				}
			case "host", "preset":
				return s, ErrCapability
			default:
				return s, ErrCapability
			}
		}
	}
	// Only a single private IPv4 bridge is admitted; factory/fresh topology.
	// Multiple private/guest bridges are unsupported rather than guessed HOME.
	for id := range privateBridges {
		if a, ok := addresses[id]; ok && strings.HasPrefix(id, "Bridge") {
			if s.Home != "" {
				return s, ErrCapability
			}
			s.Home = id
			s.Address = a
		}
	}
	for id := range wans {
		if s.WAN != "" {
			return s, ErrCapability
		}
		s.WAN = id
	}
	if s.Home == "" || s.WAN == "" {
		return s, ErrCapability
	}
	for _, l := range lines {
		w := l.words
		if len(w) == 5 && w[0] == "ip" && w[1] == "hotspot" && w[2] == "policy" && w[3] == s.Home {
			if s.HomePolicy != "" {
				return s, ErrCapability
			}
			s.HomePolicy = w[4]
		}
		if len(w) == 7 && w[0] == "dns-proxy" && w[1] == "filter" && w[2] == "assign" && w[5] == s.Home {
			if s.HomeProfile != "" {
				return s, ErrCapability
			}
			s.HomeProfile = w[6]
		}
	}
	s.config = lines
	s.Hash = projectionHash(lines, s.Home, s.WAN)
	return s, nil
}

func projectionHash(lines []configLine, home, wan string) string {
	return scopedHash(lines, home, wan, "")
}
func validUpstream(v string) bool {
	a, e := netip.ParseAddrPort(v)
	return e == nil && a.Addr().Is4() && a.Addr().IsPrivate() && a.Port() == 15354
}
func scopedHash(lines []configLine, home, wan, omit string) string {
	h := sha256.New()
	var values []string
	for _, l := range lines {
		w := l.words
		line := strings.Join(w, " ")
		if omit != "" && (line == omit || strings.HasPrefix(line, omit+" ")) {
			continue
		}
		// Empty native context headers do not represent an effective setting.
		if len(w) == 1 || len(w) == 2 && (w[0] == "interface" || line == "ip hotspot") || len(w) == 3 && w[0] == "ip" && w[1] == "policy" {
			continue
		}
		if len(w) > 1 && (w[0] == "dns-proxy" || w[0] == "ip" && (w[1] == "hotspot" || w[1] == "policy") || w[0] == "interface" && (w[1] == home || w[1] == wan)) {
			values = append(values, line)
		}
	}
	sort.Strings(values)
	for _, v := range values {
		_, _ = h.Write([]byte(v + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (a *Adapter) Discover(ctx context.Context) (Snapshot, error) {
	v, e := a.run(ctx, version, "")
	if e != nil {
		return Snapshot{}, ErrCapability
	}
	text, e := cleanText(v)
	if e != nil {
		return Snapshot{}, e
	}
	// First implementation deliberately declares one observed readback family.
	fields := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		m := treeLeaf.FindStringSubmatch(line)
		if m != nil && (m[2] == "model" || m[2] == "release" || m[2] == "arch") {
			if _, ok := fields[m[2]]; ok {
				return Snapshot{}, ErrCapability
			}
			fields[m[2]] = m[3]
		}
	}
	if fields["model"] != "Ultra (KN-1811)" || fields["release"] != "5.01.C.6.0-1" || fields["arch"] != "aarch64" {
		return Snapshot{}, ErrCapability
	}
	b, e := a.run(ctx, running, "")
	if e != nil {
		return Snapshot{}, ErrCapability
	}
	lines, e := parseConfig(b)
	if e != nil {
		return Snapshot{}, e
	}
	s, e := project(lines)
	if e != nil {
		return s, e
	}
	saved, e := a.run(ctx, startup, "")
	if e != nil {
		return s, ErrCapability
	}
	savedLines, e := parseConfig(saved)
	if e != nil {
		return s, e
	}
	savedState, e := project(savedLines)
	if e != nil || savedState.Hash != s.Hash {
		return s, ErrCapability
	}
	// Require the selected bridge/IP and active WAN in live interface output.
	if e = a.checkInterfaces(ctx, s); e != nil {
		return s, e
	}
	return s, nil
}

func (a *Adapter) Plan(s Snapshot) (Plan, error) {
	if s.Hash == "" || s.Home == "" || s.WAN == "" || !s.Address.IsPrivate() || s.HomePolicy != "" || s.HomeProfile != "" || len(s.Profiles) != 0 {
		return Plan{}, ErrCapability
	}
	for _, l := range s.config {
		w := l.words
		if len(w) > 2 && w[0] == "dns-proxy" && w[1] == "filter" && w[2] == "assign" {
			return Plan{}, ErrCapability
		}
	}
	for _, p := range s.Policies {
		if strings.EqualFold(p.Description, "xkeen") {
			return Plan{}, ErrCapability
		}
	}
	p := Plan{Home: s.Home, WAN: s.WAN, Address: s.Address, ProfileID: 1}
	for n := 1; n <= 64; n++ {
		id := fmt.Sprintf("Policy%d", n)
		if _, ok := s.Policies[id]; !ok {
			p.PolicyID = id
			break
		}
	}
	if p.PolicyID == "" {
		return Plan{}, ErrCapability
	}
	return p, nil
}

func (a *Adapter) current(ctx context.Context) (Snapshot, error) {
	b, e := a.run(ctx, running, "")
	if e != nil {
		return Snapshot{}, ErrUnknown
	}
	l, e := parseConfig(b)
	if e != nil {
		return Snapshot{}, ErrUnknown
	}
	s, e := project(l)
	if e != nil {
		return s, ErrUnknown
	}
	return s, nil
}

func validPlan(p Plan) bool {
	return interfaceID.MatchString(p.Home) && interfaceID.MatchString(p.WAN) && identifier.MatchString(p.PolicyID) && p.Address.Is4() && p.Address.IsPrivate() && p.ProfileID >= 1 && p.ProfileID <= 8
}
func (a *Adapter) change(ctx context.Context, before Snapshot, p Plan, kind, cmd string, persist Persist, verify func(Snapshot) bool) (Snapshot, error) {
	if !validPlan(p) || persist == nil {
		return Snapshot{}, ErrCapability
	}
	s, e := a.current(ctx)
	if e != nil || s.Hash != before.Hash {
		return s, ErrUnknown
	}
	if persist(Intent{Kind: kind, BeforeHash: s.Hash, Home: p.Home, PolicyID: p.PolicyID, ProfileID: p.ProfileID}) != nil {
		return s, ErrUnknown
	}
	if _, e = a.run(ctx, mutation, cmd); e != nil {
		return s, ErrUnknown
	}
	s, e = a.current(ctx)
	omit := mutationScope(p, kind)
	if e != nil || !verify(s) || omit == "" || s.Home != before.Home || s.WAN != before.WAN || s.Address != before.Address || scopedHash(s.config, s.Home, s.WAN, omit) != scopedHash(before.config, before.Home, before.WAN, omit) {
		return s, ErrUnknown
	}
	if persist(Intent{Kind: kind, BeforeHash: before.Hash, AfterHash: s.Hash, Home: p.Home, PolicyID: p.PolicyID, ProfileID: p.ProfileID}) != nil {
		return s, ErrUnknown
	}
	return s, nil
}

func mutationScope(p Plan, kind string) string {
	id := strconv.Itoa(p.ProfileID)
	switch kind {
	case "policy-create", "policy-remove":
		return "ip policy " + p.PolicyID
	case "policy-wan":
		return "ip policy " + p.PolicyID + " permit"
	case "dns-profile", "dns-remove":
		return "dns-proxy filter profile " + id
	case "dns-upstream":
		return "dns-proxy filter profile " + id + " dns53 upstream"
	case "dns-engine", "dns-engine-remove":
		return "dns-proxy filter engine"
	case "home-policy", "home-policy-remove":
		return "ip hotspot policy " + p.Home
	case "home-dns", "home-dns-remove":
		return "dns-proxy filter assign interface profile " + p.Home
	}
	return ""
}

func (a *Adapter) PreparePolicy(ctx context.Context, s Snapshot, p Plan, persist Persist) (Snapshot, error) {
	if _, ok := s.Policies[p.PolicyID]; ok {
		return s, ErrCapability
	}
	var e error
	s, e = a.change(ctx, s, p, "policy-create", "ip policy "+p.PolicyID+" description xkeen", persist, func(v Snapshot) bool { return v.Policies[p.PolicyID].Description == "xkeen" })
	if e != nil {
		return s, e
	}
	s, e = a.change(ctx, s, p, "policy-wan", "ip policy "+p.PolicyID+" permit global "+p.WAN, persist, func(v Snapshot) bool { return v.Policies[p.PolicyID].WAN == p.WAN })
	if e != nil {
		return s, e
	}
	if e = a.VerifyPolicy(ctx, p, false); e != nil {
		return s, e
	}
	return a.SaveAndInspect(ctx, s, p, persist)
}

func (a *Adapter) SaveAndInspect(ctx context.Context, s Snapshot, p Plan, persist Persist) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, e := a.current(ctx)
	if e != nil || current.Hash != s.Hash {
		return s, ErrUnknown
	}
	if persist == nil || persist(Intent{Kind: "save", BeforeHash: s.Hash, Home: p.Home, PolicyID: p.PolicyID, ProfileID: p.ProfileID}) != nil {
		return s, ErrUnknown
	}
	if _, e = a.run(ctx, mutation, "system configuration save"); e != nil {
		return s, ErrUnknown
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		b, e := a.run(ctx, startup, "")
		if e == nil {
			l, e := parseConfig(b)
			if e == nil {
				v, e := project(l)
				if e == nil && v.Hash == s.Hash {
					now, e := a.current(ctx)
					if e == nil && now.Hash == s.Hash {
						if persist(Intent{Kind: "save", BeforeHash: s.Hash, AfterHash: now.Hash, Home: p.Home, PolicyID: p.PolicyID, ProfileID: p.ProfileID}) != nil {
							return s, ErrUnknown
						}
						return now, nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return s, ErrUnknown
		case <-deadline.C:
			return s, ErrUnknown
		case <-tick.C:
		}
	}
}

func (a *Adapter) PrepareDNS(ctx context.Context, s Snapshot, p Plan, persist Persist) (Snapshot, error) {
	if len(s.Profiles) != 0 || s.HomeProfile != "" {
		return s, ErrCapability
	}
	var e error
	id := strconv.Itoa(p.ProfileID)
	s, e = a.change(ctx, s, p, "dns-profile", "dns-proxy filter profile "+id+" description xkeen-control", persist, func(v Snapshot) bool { return v.Profiles[p.ProfileID] })
	if e != nil {
		return s, e
	}
	want := "dns-proxy filter profile " + id + " dns53 upstream " + p.Address.String() + ":15354"
	s, e = a.change(ctx, s, p, "dns-upstream", want, persist, func(v Snapshot) bool { return contains(v, want) })
	if e != nil {
		return s, e
	}
	if s.Engine == "" {
		s, e = a.change(ctx, s, p, "dns-engine", "dns-proxy filter engine public", persist, func(v Snapshot) bool { return v.Engine == "public" })
		if e != nil {
			return s, e
		}
	}
	return a.SaveAndInspect(ctx, s, p, persist)
}

func contains(s Snapshot, line string) bool {
	for _, l := range s.config {
		if strings.Join(l.words, " ") == line {
			return true
		}
	}
	return false
}
func (a *Adapter) AssignHOME(ctx context.Context, s Snapshot, p Plan, persist Persist) (Snapshot, error) {
	if e := a.VerifyPolicy(ctx, p, false); e != nil {
		return s, e
	}
	if s.HomePolicy != "" || s.HomeProfile != "" || !s.Profiles[p.ProfileID] {
		return s, ErrCapability
	}
	var e error
	s, e = a.change(ctx, s, p, "home-policy", "ip hotspot policy "+p.Home+" "+p.PolicyID, persist, func(v Snapshot) bool { return v.HomePolicy == p.PolicyID })
	if e != nil {
		return s, e
	}
	s, e = a.change(ctx, s, p, "home-dns", fmt.Sprintf("dns-proxy filter assign interface profile %s %d", p.Home, p.ProfileID), persist, func(v Snapshot) bool { return v.HomeProfile == strconv.Itoa(p.ProfileID) })
	if e != nil {
		return s, e
	}
	return a.SaveAndInspect(ctx, s, p, persist)
}

// RestoreOwned removes only confirmed, unchanged effects in the dedicated
// initial scope. Caller must first independently stop native interception and
// disable its autostart. No running/startup-config is ever executed or replayed.
func (a *Adapter) RestoreOwned(ctx context.Context, original Snapshot, p Plan, expected string, persist Persist) (Snapshot, error) {
	s, e := a.current(ctx)
	if e != nil || s.Hash != expected || original.Home != p.Home || original.WAN != p.WAN || original.Address != p.Address || original.HomePolicy != "" || original.HomeProfile != "" || len(original.Profiles) != 0 {
		return s, ErrUnknown
	}
	if s.HomePolicy != "" && s.HomePolicy != p.PolicyID || s.HomeProfile != "" && s.HomeProfile != strconv.Itoa(p.ProfileID) {
		return s, ErrUnknown
	}
	if policy, ok := s.Policies[p.PolicyID]; ok && (policy.Description != "xkeen" || policy.WAN != p.WAN) {
		return s, ErrUnknown
	}
	if s.HomeProfile != "" {
		s, e = a.change(ctx, s, p, "home-dns-remove", "no dns-proxy filter assign interface profile "+p.Home, persist, func(v Snapshot) bool { return v.HomeProfile == "" })
		if e != nil {
			return s, e
		}
	}
	if s.HomePolicy != "" {
		s, e = a.change(ctx, s, p, "home-policy-remove", "no ip hotspot policy "+p.Home, persist, func(v Snapshot) bool { return v.HomePolicy == "" })
		if e != nil {
			return s, e
		}
	}
	if s.Profiles[p.ProfileID] {
		s, e = a.change(ctx, s, p, "dns-remove", fmt.Sprintf("no dns-proxy filter profile %d", p.ProfileID), persist, func(v Snapshot) bool { return !v.Profiles[p.ProfileID] })
		if e != nil {
			return s, e
		}
	}
	if original.Engine == "" && s.Engine != "" {
		s, e = a.change(ctx, s, p, "dns-engine-remove", "no dns-proxy filter engine", persist, func(v Snapshot) bool { return v.Engine == "" })
		if e != nil {
			return s, e
		}
	}
	if _, ok := s.Policies[p.PolicyID]; ok {
		s, e = a.change(ctx, s, p, "policy-remove", "no ip policy "+p.PolicyID, persist, func(v Snapshot) bool { _, ok := v.Policies[p.PolicyID]; return !ok })
		if e != nil {
			return s, e
		}
	}
	if s.Hash != original.Hash {
		return s, ErrUnknown
	}
	return a.SaveAndInspect(ctx, s, p, persist)
}
