//go:build linux

package setup

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/buildinfo"
	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/keenetic"
	"github.com/popiposter/xkeen-control/internal/nodes"
	"github.com/popiposter/xkeen-control/internal/panellistener"
	"github.com/popiposter/xkeen-control/internal/splitdns"
	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/sys/unix"
)

func readOwned(path string, limit int64, private bool) ([]byte, error) {
	d, e := ownedDirectory(filepath.Dir(path), false, private)
	if e != nil {
		return nil, e
	}
	defer d.Close()
	fd, e := unix.Openat(int(d.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0022 != 0 || st.Nlink != 1 || st.Size > limit {
		return nil, ErrState
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, ErrState
	}
	return b, nil
}

func replacePrivate(path string, b []byte) error {
	d, e := privateDirectory(filepath.Dir(path), true)
	if e != nil {
		return e
	}
	defer d.Close()
	if len(b) == 0 || len(b) > 64<<10 {
		return ErrState
	}
	if f, e := protectedFile(d, filepath.Base(path), unix.O_RDONLY); e == nil {
		f.Close()
	} else if !os.IsNotExist(e) {
		return ErrState
	}
	name := filepath.Base(path) + ".new"
	f, e := protectedFile(d, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if e != nil {
		return ErrState
	}
	_, e = f.Write(append(b, '\n'))
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return ErrState
	}
	if unix.Renameat(int(d.Fd()), name, int(d.Fd()), filepath.Base(path)) != nil || d.Sync() != nil {
		return ErrState
	}
	return nil
}

func saveFirmwareBaseline(s keenetic.Snapshot, p keenetic.Plan) error {
	b, e := json.Marshal(struct {
		Before keenetic.Snapshot
		Plan   keenetic.Plan
	}{s, p})
	if e != nil {
		return ErrState
	}
	return exclusiveFile("/opt/etc/xkeen-control/state/initial-firmware-baseline.json", b, 0600)
}

func startupFiles() error { return startupFilesFor(true) }

func startupFilesFor(legacyDNS bool) error { return startupFilesAt("/opt", legacyDNS) }

func startupFilesAt(root string, legacyDNS bool) error {
	if !legacyDNS {
		for _, path := range []string{"/opt/sbin/mosdns", "/opt/etc/mosdns", "/opt/etc/init.d/S06mosdns"} {
			if _, err := os.Lstat(filepath.Join(root, strings.TrimPrefix(path, "/opt/"))); !os.IsNotExist(err) {
				return ErrState
			}
		}
	}
	paths := []string{"/opt/etc/init.d/S99xkeen-control"}
	if legacyDNS {
		paths = append(paths, "/opt/etc/init.d/S06mosdns")
	}
	for _, p := range paths {
		p = filepath.Join(root, strings.TrimPrefix(p, "/opt/"))
		b, e := readOwned(p, 64<<10, false)
		if e != nil || !strings.HasPrefix(string(b), "#!/bin/sh\n") {
			return ErrState
		}
		st, e := os.Lstat(p)
		if e != nil || st.Mode()&0100 == 0 {
			return ErrState
		}
		if strings.HasSuffix(p, "S06mosdns") && string(b) != dnsInit {
			return ErrState
		}
		if strings.HasSuffix(p, "S99xkeen-control") && !strings.Contains(string(b), `"$BIN" setup guard`) {
			return ErrState
		}
	}
	return nil
}

func verifyScope(ctx context.Context, mark uint32) error {
	if mark == 0 {
		return ErrState
	}
	seen := 0
	for _, table := range []string{"nat", "mangle"} {
		b, e := fixedOutput(ctx, 5*time.Second, 1<<20, "/opt/sbin/iptables-save", "-t", table)
		if e != nil {
			return ErrState
		}
		count, e := scopedTable(b, mark)
		if e != nil {
			return ErrState
		}
		seen |= count
	}
	if seen != 3 { // both TCP and UDP must have positively scoped interception
		return ErrState
	}
	return nil
}

func verifyTable(b []byte, mark uint32) error {
	_, err := scopedTable(b, mark)
	return err
}

// Only this stock rule grammar can prove coverage. Extra predicates (source,
// input interface, negated mark/ports, etc.) cannot prove all-HOME interception
// or exemption. Stock's ! --ctstate INVALID is the one supported negation.
func stockScopeRule(w []string, mark uint32) (proto, ports string, err error) {
	seen := map[string]bool{}
	modules := map[string]bool{}
	markText, state := "", ""
	for i := 2; i < len(w); {
		key := w[i]
		if key == "!" {
			if i+2 >= len(w) || w[i+1] != "--ctstate" || w[i+2] != "INVALID" || !modules["conntrack"] || seen["--ctstate"] {
				return "", "", ErrState
			}
			seen["--ctstate"] = true
			state = "!INVALID"
			i += 3
			continue
		}
		if i+1 >= len(w) || seen[key] && key != "-m" {
			return "", "", ErrState
		}
		value := w[i+1]
		seen[key] = true
		switch key {
		case "-m":
			if modules[value] || value != "connmark" && value != "conntrack" && value != "multiport" && value != "comment" && value != "tcp" && value != "udp" {
				return "", "", ErrState
			}
			modules[value] = true
		case "-p":
			proto = value
		case "--mark":
			markText = value
		case "--dports":
			ports = value
		case "--comment":
			if !modules["comment"] || strings.Trim(value, `"`) != "xkeen_rule" {
				return "", "", ErrState
			}
		case "-j":
			if value != "RETURN" && value != "xkeen" || i+2 != len(w) {
				return "", "", ErrState
			}
		default:
			return "", "", ErrState
		}
		i += 2
	}
	parts := strings.Split(markText, "/")
	n, e := strconv.ParseUint(parts[0], 0, 32)
	if e != nil || mark == 0 || n != uint64(mark) || len(parts) > 2 || !modules["connmark"] || !seen["-j"] || proto != "tcp" && proto != "udp" {
		return "", "", ErrState
	}
	if len(parts) == 2 {
		mask, e := strconv.ParseUint(parts[1], 0, 32)
		if e != nil || mask != 0xffffffff {
			return "", "", ErrState
		}
	}
	if modules["conntrack"] && state != "!INVALID" || modules["comment"] && !seen["--comment"] || modules["multiport"] != seen["--dports"] || modules["tcp"] && proto != "tcp" || modules["udp"] && proto != "udp" {
		return "", "", ErrState
	}
	return proto, ports, nil
}

func scopedTable(b []byte, mark uint32) (int, error) {
	returns := map[string]bool{}
	count := 0
	for _, line := range strings.Split(string(b), "\n") {
		w := strings.Fields(line)
		if len(w) < 4 || w[0] != "-A" || w[1] != "PREROUTING" {
			continue
		}
		jump := ""
		for i := 2; i+1 < len(w); i++ {
			if w[i] == "-j" || w[i] == "-g" {
				jump = w[i+1]
			}
		}
		if jump != "RETURN" && !strings.HasPrefix(jump, "xkeen") {
			continue
		}
		proto, ports, e := stockScopeRule(w, mark)
		if jump == "RETURN" {
			if e == nil {
				found, e := excludedPort([]byte(strings.ReplaceAll(ports, ",", "\n")))
				if e != nil {
					return 0, ErrState
				}
				returns[proto] = returns[proto] || found
			}
			continue
		}
		if e != nil || jump != "xkeen" || ports != "" || !returns[proto] {
			return 0, ErrState
		}
		if proto == "tcp" {
			count |= 1
		} else {
			count |= 2
		}
	}
	return count, nil
}
func dnsAnswer(ctx context.Context, address string) error {
	name, _ := dnsmessage.NewName("example.com.")
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: 0x145, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	b, e := query.Pack()
	if e != nil {
		return ErrState
	}
	c, e := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", address)
	if e != nil {
		return ErrState
	}
	defer c.Close()
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if c.SetDeadline(deadline) != nil {
		return ErrState
	}
	request := make([]byte, len(b)+2)
	binary.BigEndian.PutUint16(request, uint16(len(b)))
	copy(request[2:], b)
	if _, e = c.Write(request); e != nil {
		return ErrState
	}
	length := make([]byte, 2)
	if _, e = io.ReadFull(c, length); e != nil {
		return ErrState
	}
	n := binary.BigEndian.Uint16(length)
	if n < 12 || n > 8192 {
		return ErrState
	}
	response := make([]byte, n)
	if _, e = io.ReadFull(c, response); e != nil {
		return ErrState
	}
	var answer dnsmessage.Message
	if answer.Unpack(response) != nil || !answer.Response || answer.ID != query.ID || answer.RCode != dnsmessage.RCodeSuccess || len(answer.Answers) == 0 || len(answer.Questions) != 1 || answer.Questions[0] != query.Questions[0] {
		return ErrState
	}
	return nil
}

func verifyReady(ctx context.Context, e *xkeen.ConfigEditor, dns *splitdns.Service, lease *authority.Lease, digest string, r nodes.Registry, mark uint32) error {
	if e.VerifySetupRuntime(ctx, digest) != nil || verifyScope(ctx, mark) != nil {
		return ErrState
	}
	if dns != nil {
		state := dns.Status(ctx)
		if !state.Running || state.State != "synced" {
			return ErrState
		}
	}
	api := xrayapi.NewClient("", "", 3*time.Second)
	snapshot := api.Snapshot(ctx)
	// Observatory may need its first bounded observation; wait for readback,
	// never re-execute a native start, activation or component installation.
	readyCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	for snapshot.Balancer.NativeSelected == "" {
		select {
		case <-readyCtx.Done():
			return ErrState
		case <-time.After(200 * time.Millisecond):
			snapshot = api.Snapshot(readyCtx)
		}
	}
	if !snapshot.APIReachable || !snapshot.RoutingReachable || !snapshot.ObservatoryReachable || !api.ProbeReachable(ctx) {
		return ErrState
	}
	member := snapshot.Balancer.NativeSelected
	found := false
	for _, n := range r.Nodes {
		found = found || n.Enabled && !n.Stale && !n.Missing && n.OutboundTag == member
	}
	if !found {
		return ErrState
	}
	addresses := []string{"127.0.0.1:53"}
	if dns != nil {
		addresses = []string{"127.0.0.1:15355", "127.0.0.1:15356", "127.0.0.1:15354"}
	}
	for _, address := range addresses {
		if dnsAnswer(ctx, address) != nil {
			return ErrState
		}
	}
	release, err := lease.TryAcquire()
	if err != nil {
		return ErrState
	}
	defer release()
	probe := c1.NewProbeRouter(api)
	proxy, _ := url.Parse("http://127.0.0.1:10808")
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrState }}
	for _, target := range []string{"direct", member} {
		if probe.WithTarget(ctx, "liveness", target, func(ctx context.Context) error {
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.google.com/generate_204", nil)
			response, e := client.Do(request)
			if e != nil {
				return ErrState
			}
			defer response.Body.Close()
			_, e = io.Copy(io.Discard, io.LimitReader(response.Body, 512))
			if e != nil || response.StatusCode != http.StatusNoContent {
				return ErrState
			}
			return nil
		}) != nil {
			return ErrState
		}
	}
	return nil
}

func startPanel(ctx context.Context, info buildinfo.Info) error {
	if fixedCommand(ctx, 20*time.Second, "/opt/etc/init.d/S99xkeen-control", "start") != nil {
		return ErrState
	}
	b, e := readOwned("/opt/etc/xkeen-control/listen-address", 256, true)
	if e != nil {
		return ErrState
	}
	address := strings.TrimSpace(string(b))
	host, port, e := net.SplitHostPort(address)
	if e != nil || port != "8787" {
		return ErrState
	}
	if _, e := panellistener.ParseAddress(address); e != nil {
		return ErrState
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrState }}
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/healthz", nil)
	if e != nil {
		return ErrState
	}
	response, e := client.Do(request)
	if e != nil {
		return ErrState
	}
	defer response.Body.Close()
	content, e := io.ReadAll(io.LimitReader(response.Body, 32))
	if e != nil || response.StatusCode != 200 || string(content) != "ok" {
		return ErrState
	}
	metadata, e := fixedOutput(ctx, 5*time.Second, 8192, "/opt/sbin/xkeen-control", "version", "--json")
	if e != nil {
		return ErrState
	}
	var actual buildinfo.Info
	if json.Unmarshal(metadata, &actual) != nil || actual != info {
		return ErrState
	}
	pid, e := readOwned("/opt/var/run/xkeen-control.pid", 64, false)
	if e != nil {
		return ErrState
	}
	n, e := strconv.Atoi(strings.TrimSpace(string(pid)))
	if e != nil || n < 1 {
		return ErrState
	}
	executable, e := os.Readlink(filepath.Join("/proc", strconv.Itoa(n), "exe"))
	if e != nil || executable != "/opt/sbin/xkeen-control" {
		return ErrState
	}
	return nil
}
