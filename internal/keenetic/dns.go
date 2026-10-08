package keenetic

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// Observed KN1811/5.01 text family: proxy-safe belongs to System, and interface
// keys correlate to kernel ifindex, not NDM index. Never infer internal profile
// ID from the external CLI profile name. This proves configuration/readback;
// independent LAN traffic and upstream-failure behavior require hardware tests.
type safeProfile struct {
	mode    string
	servers []netip.AddrPort
}
type safeDNS struct {
	profiles   map[uint32]*safeProfile
	interfaces map[uint32]uint32
}

func parseSafeDNS(b []byte) (safeDNS, error) {
	out := safeDNS{profiles: map[uint32]*safeProfile{}, interfaces: map[uint32]uint32{}}
	s, e := cleanText(b)
	if e != nil {
		return out, e
	}
	system := false
	safe := false
	safeLevel := 0
	blocks := 0
	defaultSeen := false
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := treeLeaf.FindStringSubmatch(line); m != nil {
			col := len(m[1]) + len(m[2])
			if safe && col <= safeLevel {
				safe = false
			}
			if m[2] == "proxy-status" {
				system = false
				safe = false
			}
			if m[2] == "proxy-name" {
				system = m[3] == "System"
			}
			if m[2] == "proxy-safe" && system {
				if m[3] != "" {
					return out, ErrCapability
				}
				blocks++
				safe = true
				safeLevel = col
			}
			continue
		}
		if !safe {
			continue
		}
		w := strings.Fields(line)
		if len(w) == 0 {
			continue
		}
		number := func(v string) (uint32, error) {
			n, e := strconv.ParseUint(v, 10, 32)
			if e != nil || n == 0 {
				return 0, ErrCapability
			}
			return uint32(n), nil
		}
		switch w[0] {
		case "profile":
			if len(w) != 3 || w[2] != "safe-access" {
				return out, ErrCapability
			}
			id, e := number(w[1])
			if e != nil || out.profiles[id] != nil {
				return out, ErrCapability
			}
			out.profiles[id] = &safeProfile{mode: w[2]}
		case "profile-set-server":
			if len(w) != 3 {
				return out, ErrCapability
			}
			id, e := number(w[1])
			if e != nil || out.profiles[id] == nil {
				return out, ErrCapability
			}
			endpoint, e := netip.ParseAddrPort(w[2])
			if e != nil || len(out.profiles[id].servers) >= 6 {
				return out, ErrCapability
			}
			out.profiles[id].servers = append(out.profiles[id].servers, endpoint)
		case "set-profile":
			if len(w) != 3 || w[1] != "mac_default" || w[2] != "0" || defaultSeen {
				return out, ErrCapability
			}
			defaultSeen = true
		case "set-profile-interface":
			if len(w) != 3 {
				return out, ErrCapability
			}
			index, e := number(w[1])
			id, e2 := number(w[2])
			if e != nil || e2 != nil || out.interfaces[index] != 0 {
				return out, ErrCapability
			}
			out.interfaces[index] = id
		default:
			return out, ErrCapability
		}
	}
	if blocks != 1 || !defaultSeen {
		return out, ErrCapability
	}
	for _, id := range out.interfaces {
		if out.profiles[id] == nil {
			return out, ErrCapability
		}
	}
	return out, nil
}
func homeKernelIndex(address netip.Addr) (uint32, error) {
	interfaces, e := net.Interfaces()
	if e != nil || len(interfaces) > 128 {
		return 0, ErrCapability
	}
	index := 0
	for _, iface := range interfaces {
		addrs, e := iface.Addrs()
		if e != nil || len(addrs) > 128 {
			return 0, ErrCapability
		}
		for _, a := range addrs {
			prefix, e := netip.ParsePrefix(a.String())
			if e != nil {
				return 0, ErrCapability
			}
			if prefix.Addr() == address {
				if index != 0 || iface.Index <= 0 || iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
					return 0, ErrCapability
				}
				index = iface.Index
			}
		}
	}
	if index == 0 {
		return 0, ErrCapability
	}
	return uint32(index), nil
}
func verifyDNSProjection(s Snapshot, p Plan, parsed safeDNS, index uint32, assigned bool) error {
	expected := "dns-proxy filter profile " + strconv.Itoa(p.ProfileID) + " dns53 upstream " + p.Address.String() + ":15354"
	if len(s.Profiles) != 1 || !s.Profiles[p.ProfileID] || s.Engine != "public" || !contains(s, expected) {
		return ErrUnknown
	}
	upstreamCount := 0
	for _, l := range s.config {
		w := l.words
		if len(w) >= 6 && w[0] == "dns-proxy" && w[1] == "filter" && w[2] == "profile" && w[4] == "dns53" {
			upstreamCount++
		}
	}
	if upstreamCount != 1 {
		return ErrUnknown
	}
	id := uint32(0)
	for n, v := range parsed.profiles {
		if len(v.servers) == 1 && v.servers[0] == netip.AddrPortFrom(p.Address, 15354) {
			if id != 0 {
				return ErrUnknown
			}
			id = n
		}
	}
	if id == 0 || len(parsed.profiles) != 1 {
		return ErrUnknown
	}
	if assigned {
		if s.HomeProfile != strconv.Itoa(p.ProfileID) || parsed.interfaces[index] != id {
			return ErrUnknown
		}
	} else if s.HomeProfile != "" || len(parsed.interfaces) != 0 {
		return ErrUnknown
	}
	return nil
}
func (a *Adapter) VerifyDNS(ctx context.Context, p Plan, assigned bool) error {
	if !validPlan(p) {
		return ErrCapability
	}
	s, e := a.current(ctx)
	if e != nil {
		return e
	}
	index, e := homeKernelIndex(p.Address)
	if e != nil {
		return e
	}
	b, e := a.run(ctx, dnsRuntime, "")
	if e != nil {
		return ErrUnknown
	}
	parsed, e := parseSafeDNS(b)
	if e != nil {
		return e
	}
	if verifyDNSProjection(s, p, parsed, index, assigned) != nil {
		return ErrUnknown
	}
	again, e := a.current(ctx)
	indexAgain, e2 := homeKernelIndex(p.Address)
	if e != nil || e2 != nil || again.Hash != s.Hash || indexAgain != index {
		return ErrUnknown
	}
	return nil
}
