package keenetic

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

var treeHeader = regexp.MustCompile(`^(\s*)([A-Za-z][a-z0-9-]*), name = ([A-Za-z0-9_./-]+)(?:, description = (.*))?:\s*$`)
var interfaceHeader = regexp.MustCompile(`^Interface, name = "([A-Za-z0-9_./-]+)"\s*$`)
var treeLeaf = regexp.MustCompile(`^(\s*)([a-z][a-z0-9-]*):\s*(.*)$`)

type record struct {
	name, description string
	lines             []string
}

func records(b []byte, kind string) ([]record, error) {
	s, e := cleanText(b)
	if e != nil {
		return nil, e
	}
	var out []record
	rootLevel := -1
	for _, line := range strings.Split(s, "\n") {
		if m := interfaceHeader.FindStringSubmatch(line); m != nil {
			if kind != "interface" {
				return nil, ErrCapability
			}
			rootLevel = len("Interface")
			out = append(out, record{name: m[1]})
			continue
		}
		if m := treeHeader.FindStringSubmatch(line); m != nil {
			level := len(m[1]) + len(m[2])
			if rootLevel < 0 {
				rootLevel = level
			}
			if level > rootLevel && len(out) > 0 { // nested port attributes belong to their interface
				if kind != "interface" || m[2] != "port" {
					return nil, ErrCapability
				}
				continue
			}
			if level != rootLevel || !strings.EqualFold(m[2], kind) {
				return nil, ErrCapability
			}
			out = append(out, record{name: m[3], description: m[4]})
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(out) == 0 {
			return nil, ErrCapability
		}
		if treeLeaf.FindStringSubmatch(line) == nil {
			return nil, ErrCapability
		}
		out[len(out)-1].lines = append(out[len(out)-1].lines, line)
	}
	return out, nil
}
func (r record) rootValue(key string) (string, error) {
	level := int(^uint(0) >> 1)
	for _, line := range r.lines {
		m := treeLeaf.FindStringSubmatch(line)
		if m != nil {
			col := len(m[1]) + len(m[2])
			if col < level {
				level = col
			}
		}
	}
	value := ""
	found := false
	for _, line := range r.lines {
		m := treeLeaf.FindStringSubmatch(line)
		if m != nil && m[2] == key && len(m[1])+len(key) == level {
			if found {
				return "", ErrCapability
			}
			value = m[3]
			found = true
		}
	}
	if !found {
		return "", ErrCapability
	}
	return value, nil
}

func (a *Adapter) checkInterfaces(ctx context.Context, s Snapshot) error {
	b, e := a.run(ctx, interfaces, "")
	if e != nil {
		return ErrCapability
	}
	rs, e := records(b, "interface")
	if e != nil {
		return e
	}
	home, wan := false, false
	for _, r := range rs {
		id, e := r.rootValue("id")
		if e != nil || !interfaceID.MatchString(id) {
			return ErrCapability
		}
		alias, e := r.rootValue("interface-name")
		if e != nil || alias != r.name {
			return ErrCapability
		}
		if id != s.Home && id != s.WAN {
			continue
		}
		state, e := r.rootValue("state")
		if e != nil || state != "up" {
			return ErrCapability
		}
		connected, e := r.rootValue("connected")
		if e != nil || connected != "yes" {
			return ErrCapability
		}
		if id == s.Home {
			if home {
				return ErrCapability
			}
			home = true
			found := false
			for _, line := range r.lines {
				m := treeLeaf.FindStringSubmatch(line)
				if m != nil && m[2] == "address" && m[3] == s.Address.String() {
					found = true
				}
			}
			if !found {
				return ErrCapability
			}
		} else {
			if wan {
				return ErrCapability
			}
			wan = true
		}
	}
	if !home || !wan {
		return ErrCapability
	}
	return nil
}

func (a *Adapter) VerifyPolicy(ctx context.Context, p Plan, assigned bool) error {
	if !validPlan(p) {
		return ErrCapability
	}
	s, e := a.current(ctx)
	if e != nil {
		return e
	}
	want := s.Policies[p.PolicyID]
	if want.Description != "xkeen" || want.WAN != p.WAN {
		return ErrUnknown
	}
	if !assigned && s.HomePolicy == p.PolicyID {
		return ErrUnknown
	}
	// All policy assignments are inspected, including offline host rules which
	// project() rejects. No unassigned-policy claim from the live host list.
	for _, l := range s.config {
		w := l.words
		if len(w) == 5 && w[0] == "ip" && w[1] == "hotspot" && w[2] == "policy" && w[4] == p.PolicyID {
			if !assigned || w[3] != p.Home {
				return ErrUnknown
			}
		}
	}
	b, e := a.run(ctx, policies, "")
	if e != nil {
		return ErrUnknown
	}
	rs, e := records(b, "policy")
	if e != nil {
		return ErrUnknown
	}
	count := 0
	for _, r := range rs {
		if !strings.EqualFold(strings.Trim(r.description, `"`), "xkeen") {
			continue
		}
		count++
		if r.name != p.PolicyID {
			return ErrUnknown
		}
		mark, e := r.rootValue("mark")
		if e != nil {
			return ErrUnknown
		}
		n, e := strconv.ParseUint(strings.TrimPrefix(mark, "0x"), 16, 32)
		if e != nil || n == 0 {
			return ErrUnknown
		}
		table, e := r.rootValue("table4")
		if e != nil {
			return ErrUnknown
		}
		t, e := strconv.ParseUint(table, 10, 64)
		if e != nil || t == 0 {
			return ErrUnknown
		}
		defaultRoute := false
		inRoute := false
		destination, iface, rejecting := "", "", ""
		check := func() {
			if destination == "0.0.0.0/0" && iface == p.WAN && rejecting == "no" {
				defaultRoute = true
			}
		}
		for _, line := range r.lines {
			m := treeLeaf.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if m[2] == "route4" && m[3] == "" {
				if inRoute {
					check()
				}
				inRoute = false
				continue
			}
			if m[2] == "route6" {
				if inRoute {
					check()
				}
				inRoute = false
				break
			}
			if m[2] == "route" && m[3] == "" {
				if inRoute {
					check()
				}
				inRoute = true
				destination, iface, rejecting = "", "", ""
				continue
			}
			if inRoute {
				switch m[2] {
				case "destination":
					destination = m[3]
				case "interface":
					iface = m[3]
				case "rejecting":
					rejecting = m[3]
				}
			}
		}
		if inRoute {
			check()
		}
		if !defaultRoute {
			return ErrUnknown
		}
	}
	if count != 1 {
		return ErrUnknown
	}
	return nil
}

func (a *Adapter) PolicyMark(ctx context.Context, p Plan) (uint32, error) {
	return a.Mark(ctx, p, false)
}
func (a *Adapter) Mark(ctx context.Context, p Plan, assigned bool) (uint32, error) {
	if e := a.VerifyPolicy(ctx, p, assigned); e != nil {
		return 0, e
	}
	b, e := a.run(ctx, policies, "")
	if e != nil {
		return 0, ErrUnknown
	}
	rs, e := records(b, "policy")
	if e != nil {
		return 0, ErrUnknown
	}
	for _, r := range rs {
		if r.name == p.PolicyID {
			v, e := r.rootValue("mark")
			if e != nil {
				return 0, ErrUnknown
			}
			n, e := strconv.ParseUint(strings.TrimPrefix(v, "0x"), 16, 32)
			if e != nil || n == 0 {
				return 0, ErrUnknown
			}
			return uint32(n), nil
		}
	}
	return 0, ErrUnknown
}
