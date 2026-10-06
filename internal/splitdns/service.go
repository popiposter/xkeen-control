package splitdns

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/geodatareader"
)

type Status struct {
	State    string    `json:"state"`
	Running  bool      `json:"running"`
	Entries  int       `json:"entries"`
	Skipped  int       `json:"conditionalRules"`
	LastSync time.Time `json:"lastSync,omitempty"`
	Message  string    `json:"message,omitempty"`
}
type manifest struct {
	Digest  string            `json:"digest"`
	Sources map[string]string `json:"sources"`
	Entries int               `json:"entries"`
	Skipped int               `json:"conditionalRules"`
	Synced  time.Time         `json:"synced"`
}
type Service struct {
	Dir, Init, AssetDir string
	Lease               *authority.Lease
	ReadNative          func(context.Context) (map[string][]byte, error)
	Pending             func() bool
	Restart             func(context.Context) error
	Ready               func(context.Context) bool
	Identity            func(context.Context) string
	mu                  sync.Mutex
	status              Status
	metadata            string
	ambiguous           bool
}

var ErrSync = errors.New("LAN DNS synchronization failed; inspect its status without replaying XKeen")

func safeDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0022 == 0
}
func readRegular(path string, limit int64) ([]byte, error) {
	i, err := os.Lstat(path)
	if err != nil || !i.Mode().IsRegular() || i.Size() > limit {
		return nil, ErrSync
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrSync
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(i, opened) {
		return nil, ErrSync
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, ErrSync
	}
	return b, nil
}
func atomicWrite(path string, data []byte) error {
	if i, err := os.Lstat(path); err == nil && !i.Mode().IsRegular() {
		return ErrSync
	} else if err != nil && !os.IsNotExist(err) {
		return ErrSync
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".panel-dns-")
	if err != nil {
		return ErrSync
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrSync
	}
	if os.Rename(name, path) != nil {
		return ErrSync
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return ErrSync
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrSync
	}
	return nil
}
func (s *Service) configured() bool {
	// Absence is optional. Present but unreadable/unsafe data must fail visibly.
	_, err := os.Lstat(filepath.Join(s.Dir, "config.json"))
	return !os.IsNotExist(err)
}
func fileHash(path string, limit int64) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return "", ErrSync
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ErrSync
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", ErrSync
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, limit+1))
	after, statErr := os.Lstat(path)
	if err != nil || n > limit || statErr != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return "", ErrSync
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *Service) isReady(ctx context.Context) bool {
	if s.Ready != nil {
		return s.Ready(ctx)
	}
	d := net.Dialer{Timeout: time.Second}
	c, err := d.DialContext(ctx, "tcp", "127.0.0.1:15354")
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
func (s *Service) identity(ctx context.Context) string {
	if s.Identity != nil {
		return s.Identity(ctx)
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}
	identity := ""
	count := 0
	for _, e := range entries {
		if ctx.Err() != nil {
			return ""
		}
		name := e.Name()
		numeric := name != ""
		for _, c := range name {
			if c < '0' || c > '9' {
				numeric = false
			}
		}
		if !numeric {
			continue
		}
		count++
		if count > 4096 {
			return ""
		}
		path := filepath.Join("/proc", name)
		exe, err := os.Readlink(filepath.Join(path, "exe"))
		if err != nil || exe != "/opt/sbin/mosdns" {
			continue
		}
		cmd, err := readRegular(filepath.Join(path, "cmdline"), 4096)
		if err != nil || !bytes.Contains(cmd, []byte("-c\x00"+filepath.Join(s.Dir, "config.json")+"\x00")) {
			return ""
		}
		stat, err := readRegular(filepath.Join(path, "stat"), 4096)
		if err != nil {
			return ""
		}
		end := strings.LastIndex(string(stat), ")")
		if end < 0 {
			return ""
		}
		fields := strings.Fields(string(stat[end+1:]))
		if len(fields) < 20 || identity != "" {
			return ""
		}
		identity = name + ":" + fields[19]
	}
	return identity
}
func (s *Service) Status(ctx context.Context) Status {
	s.mu.Lock()
	v := s.status
	s.mu.Unlock()
	if !s.configured() {
		return Status{State: "unconfigured"}
	}
	v.Running = s.isReady(ctx) && s.identity(ctx) != ""
	if v.State == "" {
		v.State = "checking"
	}
	if v.State == "synced" && !v.Running {
		v.State = "failed"
		v.Message = "DNS process or listener is not confirmed. Inspect the service before synchronization."
	}
	if s.Pending != nil && s.Pending() {
		v.State = "pending"
		v.Message = "Saved native configuration is waiting for Apply; LAN DNS keeps the active rules."
	}
	return v
}
func (s *Service) fail(message string, ambiguous bool) error {
	s.status.State = "failed"
	s.status.Message = message
	s.ambiguous = ambiguous
	return ErrSync
}
func (s *Service) Validate(ctx context.Context, files map[string][]byte) error {
	if !s.configured() {
		return nil
	}
	if !safeDirectory(s.Dir) {
		return ErrPolicy
	}
	config, err := readRegular(filepath.Join(s.Dir, "config.json"), 8<<20)
	if err != nil {
		return ErrPolicy
	}
	_, err = Compile(ctx, files, config, s.Dir, &geodatareader.Reader{Dir: s.AssetDir})
	return err
}

// ReconcileOwned runs under the caller's ordinary panel lease. It never invokes
// a native XKeen action, writes its files, or installs/updates a component.
func (s *Service) ReconcileOwned(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.configured() {
		s.status = Status{State: "unconfigured"}
		return nil
	}
	if !safeDirectory(s.Dir) {
		return s.fail("DNS configuration directory is unsafe.", false)
	}
	if _, err := os.Lstat(filepath.Join(s.Dir, "panel-operation.json")); err == nil {
		s.ambiguous = true
	} else if !os.IsNotExist(err) {
		return s.fail("DNS operation state is unavailable.", true)
	}
	if s.ambiguous {
		return s.fail("DNS activation needs independent verification. Check and synchronize will inspect it without replay.", true)
	}
	if s.Pending != nil && s.Pending() {
		s.status.State = "pending"
		return nil
	}
	files, err := s.ReadNative(ctx)
	if err != nil {
		return s.fail("Native configuration changed or is unavailable.", false)
	}
	config, err := readRegular(filepath.Join(s.Dir, "config.json"), 8<<20)
	if err != nil {
		return s.fail("DNS configuration is unavailable.", false)
	}
	plan, err := Compile(ctx, files, config, s.Dir, &geodatareader.Reader{Dir: s.AssetDir})
	if err != nil {
		return s.fail("DNS rules could not be derived. Check domain targets, geodata categories and VPN resolvers.", false)
	}
	// Recheck bytes after potentially slow geodata parsing. External CLI/cron are
	// not locked by this panel lease; visible drift is rejected, never overwritten.
	again, err := s.ReadNative(ctx)
	if err != nil {
		return s.fail("Native configuration changed during DNS synchronization.", false)
	}
	for _, name := range []string{"02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json"} {
		if !bytes.Equal(files[name], again[name]) {
			return s.fail("Native configuration changed during DNS synchronization.", false)
		}
	}
	for name, digest := range plan.Sources {
		if strings.HasPrefix(name, "geosite") {
			actual, err := fileHash(filepath.Join(s.AssetDir, name), geodatareader.MaxFile)
			if err != nil || actual != digest {
				return s.fail("Geodata changed during DNS synchronization.", false)
			}
		}
	}
	current, err := readRegular(filepath.Join(s.Dir, "config.json"), 8<<20)
	if err != nil || !bytes.Equal(current, config) {
		return s.fail("DNS configuration changed during synchronization.", false)
	}
	var old manifest
	if b, err := readRegular(filepath.Join(s.Dir, "panel-manifest.json"), 64<<10); err == nil {
		_ = json.Unmarshal(b, &old)
	}
	if old.Digest == plan.Digest && s.isReady(ctx) {
		// Validate actual owned list/config content, not just a manifest assertion.
		expected := renderedConfig(plan, s.Dir)
		intact := bytes.Equal(config, expected)
		for name, data := range plan.Lists {
			b, err := readRegular(filepath.Join(s.Dir, "panel-generation-"+plan.Digest[:16], name), 16<<20)
			if err != nil || !bytes.Equal(b, data) {
				intact = false
			}
		}
		if intact && s.identity(ctx) != "" {
			if !reflect.DeepEqual(old.Sources, plan.Sources) || old.Skipped != plan.Skipped {
				old.Sources, old.Skipped, old.Synced = plan.Sources, plan.Skipped, time.Now().UTC()
				value, _ := json.Marshal(old)
				if atomicWrite(filepath.Join(s.Dir, "panel-manifest.json"), value) != nil {
					return s.fail("DNS source metadata could not be saved.", false)
				}
			}
			s.status = Status{State: "synced", Running: true, Entries: plan.Entries, Skipped: plan.Skipped, LastSync: old.Synced}
			return nil
		}
	}
	generation := "panel-generation-" + plan.Digest[:16]
	path := filepath.Join(s.Dir, generation)
	if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
		return s.fail("Could not stage DNS rules.", false)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return s.fail("DNS generation directory is unsafe.", false)
	}
	for name, data := range plan.Lists {
		if atomicWrite(filepath.Join(path, name), data) != nil {
			return s.fail("Could not save DNS rules.", false)
		}
	}
	newConfig := renderedConfig(plan, s.Dir)
	operation, _ := json.Marshal(map[string]string{"digest": plan.Digest, "before": s.identity(ctx)})
	if atomicWrite(filepath.Join(s.Dir, "panel-previous.json"), config) != nil || atomicWrite(filepath.Join(s.Dir, "panel-operation.json"), operation) != nil || atomicWrite(filepath.Join(s.Dir, "config.json"), newConfig) != nil {
		return s.fail("Could not activate the DNS configuration.", true)
	}
	restartCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	err = s.restart(restartCtx)
	if err != nil {
		// Keep actual files/process inspectable; do not replay native commands or
		// automatically restart a possibly still-running DNS operation.
		return s.fail("DNS service restart was not confirmed. Inspect its process before another synchronization.", true)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for !s.isReady(ctx) {
		select {
		case <-ctx.Done():
			return s.fail("DNS readiness was not confirmed.", true)
		case <-deadline.C:
			return s.fail("DNS service did not become ready.", true)
		case <-tick.C:
		}
	}
	var op map[string]string
	_ = json.Unmarshal(operation, &op)
	if id := s.identity(ctx); id == "" || id == op["before"] {
		return s.fail("A new DNS process was not independently confirmed.", true)
	}
	if b, err := readRegular(filepath.Join(s.Dir, "config.json"), 8<<20); err != nil || !bytes.Equal(b, newConfig) {
		return s.fail("DNS configuration changed during activation.", true)
	}
	now := time.Now().UTC()
	m := manifest{plan.Digest, plan.Sources, plan.Entries, plan.Skipped, now}
	b, _ := json.Marshal(m)
	if atomicWrite(filepath.Join(s.Dir, "panel-manifest.json"), b) != nil {
		return s.fail("DNS is running but synchronization metadata was not saved.", true)
	}
	if os.Remove(filepath.Join(s.Dir, "panel-operation.json")) != nil {
		return s.fail("DNS is running but its operation receipt could not be finalized.", true)
	}
	s.status = Status{State: "synced", Running: true, Entries: plan.Entries, Skipped: plan.Skipped, LastSync: now}
	s.cleanupGenerations(generation, config)
	return nil
}
func (s *Service) restart(ctx context.Context) error {
	if s.Restart != nil {
		return s.Restart(ctx)
	}
	i, err := os.Lstat(s.Init)
	if err != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0022 != 0 {
		return ErrSync
	}
	command := exec.CommandContext(ctx, s.Init, "restart")
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.WaitDelay = time.Second
	return command.Run()
}
func (s *Service) Sync(ctx context.Context) error {
	release, err := s.Lease.TryAcquire()
	if err != nil {
		return err
	}
	defer release()
	if s.configured() && !safeDirectory(s.Dir) {
		return ErrSync
	}
	if s.Pending != nil && s.Pending() {
		s.mu.Lock()
		s.status.State = "pending"
		s.mu.Unlock()
		return nil
	}
	// An interrupted operation is inspect-only; a new process plus exact derived
	// config/list bytes can confirm it without another service/native command.
	if b, err := readRegular(filepath.Join(s.Dir, "panel-operation.json"), 4096); err == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		var op map[string]string
		if json.Unmarshal(b, &op) != nil {
			return s.fail("DNS operation receipt is invalid.", true)
		}
		files, err := s.ReadNative(ctx)
		if err != nil {
			return ErrSync
		}
		config, err := readRegular(filepath.Join(s.Dir, "config.json"), 8<<20)
		if err != nil {
			return ErrSync
		}
		plan, err := Compile(ctx, files, config, s.Dir, &geodatareader.Reader{Dir: s.AssetDir})
		if err != nil || plan.Digest != op["digest"] {
			return s.fail("Interrupted DNS configuration no longer matches native rules.", true)
		}
		generation := "panel-generation-" + plan.Digest[:16]
		expected := renderedConfig(plan, s.Dir)
		id := s.identity(ctx)
		if !bytes.Equal(config, expected) || !s.isReady(ctx) || id == "" || id == op["before"] {
			return s.fail("Interrupted DNS activation remains unconfirmed; no restart was replayed.", true)
		}
		for name, data := range plan.Lists {
			b, err := readRegular(filepath.Join(s.Dir, generation, name), 16<<20)
			if err != nil || !bytes.Equal(b, data) {
				return s.fail("Interrupted DNS rules changed.", true)
			}
		}
		fresh, err := s.ReadNative(ctx)
		if err != nil {
			return s.fail("Native rules could not be verified.", true)
		}
		for name, digest := range plan.Sources {
			if strings.HasPrefix(name, "geosite") {
				actual, err := fileHash(filepath.Join(s.AssetDir, name), geodatareader.MaxFile)
				if err != nil || actual != digest {
					return s.fail("Geodata changed during verification.", true)
				}
			} else if hash(fresh[name]) != digest {
				return s.fail("Native rules changed during verification.", true)
			}
		}
		current, err := readRegular(filepath.Join(s.Dir, "config.json"), 8<<20)
		if err != nil || !bytes.Equal(current, config) || s.identity(ctx) != id {
			return s.fail("DNS changed during verification.", true)
		}
		now := time.Now().UTC()
		m := manifest{plan.Digest, plan.Sources, plan.Entries, plan.Skipped, now}
		value, _ := json.Marshal(m)
		if atomicWrite(filepath.Join(s.Dir, "panel-manifest.json"), value) != nil || os.Remove(filepath.Join(s.Dir, "panel-operation.json")) != nil {
			return ErrSync
		}
		s.ambiguous = false
		s.status = Status{State: "synced", Running: true, Entries: plan.Entries, Skipped: plan.Skipped, LastSync: now}
		return nil
	}
	return s.ReconcileOwned(ctx)
}
func (s *Service) cleanupGenerations(active string, previous []byte) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !regexpGeneration(name) || name == active || bytes.Contains(previous, []byte(name)) {
			continue
		}
		if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(s.Dir, name)
		children, err := os.ReadDir(dir)
		if err != nil || len(children) > 257 {
			continue
		}
		safe := true
		for _, child := range children {
			if child.Type()&os.ModeSymlink != 0 || child.IsDir() || !strings.HasPrefix(child.Name(), "rule-") || !strings.HasSuffix(child.Name(), ".txt") {
				safe = false
			}
		}
		if safe {
			for _, child := range children {
				_ = os.Remove(filepath.Join(dir, child.Name()))
			}
			_ = os.Remove(dir)
		}
	}
}
func regexpGeneration(name string) bool {
	if len(name) != len("panel-generation-")+16 || !strings.HasPrefix(name, "panel-generation-") {
		return false
	}
	for _, c := range strings.TrimPrefix(name, "panel-generation-") {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Run checks metadata, not every byte every minute. Changes from native cron
// while the panel is stopped are reconciled on the next panel startup.
func (s *Service) Run(ctx context.Context) {
	s.check(ctx)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.check(ctx)
		}
	}
}
func (s *Service) check(ctx context.Context) {
	if !s.configured() {
		return
	}
	var names []string
	for _, dir := range []string{s.AssetDir, s.Dir} {
		es, err := os.ReadDir(dir)
		if err != nil || len(es) > 128 {
			return
		}
		for _, e := range es {
			if dir == s.AssetDir && !strings.HasPrefix(e.Name(), "geosite") {
				continue
			}
			if dir == s.Dir && e.Name() != "config.json" {
				continue
			}
			i, err := e.Info()
			if err != nil {
				return
			}
			names = append(names, fmt.Sprintf("%s/%s:%d:%d", dir, e.Name(), i.Size(), i.ModTime().UnixNano()))
		}
	}
	// Reading Snapshot also detects routing/inbound/DNS/endpoint changes without
	// ever exposing native credentials through a status projection.
	files, err := s.ReadNative(ctx)
	if err != nil {
		return
	}
	for _, n := range []string{"02_dns.json", "03_inbounds.json", "04_outbounds.json", "05_routing.json"} {
		names = append(names, n+":"+hash(files[n]))
	}
	sort.Strings(names)
	metadata := strings.Join(names, "\n")
	s.mu.Lock()
	same := s.metadata == metadata && s.status.State == "synced"
	s.mu.Unlock()
	if same {
		return
	}
	release, err := s.Lease.TryAcquire()
	if err != nil {
		return
	}
	defer release()
	bounded, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if s.ReconcileOwned(bounded) == nil {
		s.mu.Lock()
		if s.status.State == "synced" {
			s.metadata = metadata
		}
		s.mu.Unlock()
	}
}
