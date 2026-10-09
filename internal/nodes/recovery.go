package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const recoveryReceiptName = "node-recovery.json"

// RecoveryRuntime supplies independent process identity and refuses live native
// writers. Offline callers must hold setup.Maintenance for the entire operation.
// It is not an API or an alternative node mutation owner.
type RecoveryRuntime interface {
	Snapshot(context.Context) (string, error)
}

type RecoveryInspection struct {
	Classification string `json:"classification"`
	Previous       string `json:"previous"`
	Digest         string `json:"digest,omitempty"`
	Phase          string `json:"phase,omitempty"`
	CanActivate    bool   `json:"canActivate"`
	Nodes          int    `json:"nodes"`
}

type recoveryReceipt struct {
	Schema        int    `json:"schemaVersion"`
	Phase         string `json:"phase"`
	Digest        string `json:"digest"`
	Marker        string `json:"marker"`
	RuntimeBefore string `json:"runtimeBefore"`
	RuntimeAfter  string `json:"runtimeAfter,omitempty"`
}

type recoveryState struct {
	view             RecoveryInspection
	registry         Registry
	marker           os.FileInfo
	markerDigest     string
	runtime          string
	configs          map[string][]byte
	receipt          *recoveryReceipt
	generationDigest string
}

func (m *Manager) recoveryDir() string {
	if m.tx.PreviousDir != "" {
		return m.tx.PreviousDir
	}
	return filepath.Join(filepath.Dir(m.store.Path), "previous")
}

func readRecoveryReceipt(dir string) (*recoveryReceipt, error) {
	data, _, err := recoveryRead(filepath.Join(dir, recoveryReceiptName), 8192, true)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrNodeRecoveryRequired
	}
	var r recoveryReceipt
	if decodeStrictJSON(data, &r) != nil || r.Schema != 1 || (r.Phase != "activation-intent" && r.Phase != "verified" && r.Phase != "completed") || len(r.Digest) != 64 || len(r.Marker) != 64 {
		return nil, ErrNodeRecoveryRequired
	}
	return &r, nil
}

func recoveryUnsettled(dir string) bool {
	r, err := readRecoveryReceipt(dir)
	return err != nil || r != nil && r.Phase != "completed"
}

// RecoveryNeedsInspection fences daemon mutations after a crashed settlement,
// including the interval after the old marker was unlinked but before completion.
func RecoveryNeedsInspection(previousDir string) bool { return recoveryUnsettled(previousDir) }

func coherentRecoveryPair(registryBytes, active []byte) (Registry, bool) {
	var r Registry
	if json.Unmarshal(registryBytes, &r) != nil || r.Validate() != nil {
		return r, false
	}
	rendered, err := RenderNative(active, r, r)
	return r, err == nil && nativeDocumentsEqual(active, rendered)
}

func (m *Manager) recoverySnapshot(ctx context.Context, runtime RecoveryRuntime) (recoveryState, error) {
	s := recoveryState{view: RecoveryInspection{Classification: "mixed-or-unknown", Previous: "unavailable"}, configs: map[string][]byte{}}
	if m.beforeCommit != nil {
		if err := m.beforeCommit(ctx); err != nil {
			return s, ErrNodeRecoveryRequired
		}
	}
	if ctx.Err() != nil || runtime == nil || m.tx.ConfigDir == "" || m.tx.ActiveOutboundsPath != filepath.Join(m.tx.ConfigDir, "04_outbounds.json") {
		return s, ErrNodeRecoveryRequired
	}
	dir := m.recoveryDir()
	receipt, err := readRecoveryReceipt(dir)
	if err != nil {
		return s, err
	}
	s.receipt = receipt
	if receipt != nil {
		s.view.Phase = receipt.Phase
	}
	marker, info, err := recoveryRead(filepath.Join(dir, ".pending"), 128, true)
	if errors.Is(err, os.ErrNotExist) && receipt != nil {
		s.view.Classification = "recovery-unsettled"
		if receipt.Phase == "completed" {
			s.view.Classification = "completed-receipt"
		}
		return s, nil
	}
	if err != nil || string(marker) != "node-operation-pending\n" {
		return s, ErrNodeRecoveryRequired
	}
	s.marker = info
	s.markerDigest = recoveryIdentity(info, marker)
	runtimeID, err := runtime.Snapshot(ctx)
	if err != nil || runtimeID == "" {
		return s, ErrNodeRecoveryRequired
	}
	s.runtime = runtimeID
	h := sha256.New()
	bind := func(name string, b []byte, info os.FileInfo) {
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(b))
		h.Write(b)
		if info != nil {
			fmt.Fprint(h, recoveryIdentity(info, b))
		}
	}
	bind("marker", marker, info)
	raw, info, err := recoveryRead(m.store.Path, MaxRegistryDocument, true)
	if err != nil {
		return s, ErrNodeRecoveryRequired
	}
	bind("registry", raw, info)
	entries, err := recoveryConfigNames(m.tx.ConfigDir)
	if err != nil {
		return s, ErrNodeRecoveryRequired
	}
	total := 0
	for _, name := range entries {
		b, i, e := recoveryRead(filepath.Join(m.tx.ConfigDir, name), MaxLegacyDocument, false)
		if e != nil {
			return s, ErrNodeRecoveryRequired
		}
		total += len(b)
		if total > 8<<20 {
			return s, ErrNodeRecoveryRequired
		}
		bind(name, b, i)
		s.configs[name] = b
	}
	registry, coherent := coherentRecoveryPair(raw, s.configs["04_outbounds.json"])
	s.registry = registry
	s.view.Nodes = len(registry.Nodes)
	if coherent {
		s.view.Classification = "current-coherent"
	}
	previous := map[string][]byte{}
	for _, name := range []string{"nodes.json", "04_outbounds.json", ".registry-absent", ".outbounds-absent"} {
		b, i, e := recoveryRead(filepath.Join(dir, name), MaxRegistryDocument, true)
		if errors.Is(e, os.ErrNotExist) {
			bind("previous-absent:"+name, nil, nil)
			continue
		}
		if e != nil {
			return s, ErrNodeRecoveryRequired
		}
		previous[name] = b
		bind("previous:"+name, b, i)
	}
	if _, ok := coherentRecoveryPair(previous["nodes.json"], previous["04_outbounds.json"]); ok && previous[".registry-absent"] == nil && previous[".outbounds-absent"] == nil {
		s.view.Previous = "coherent"
	}
	s.generationDigest = hex.EncodeToString(h.Sum(nil))
	bind("runtime", []byte(runtimeID), nil)
	if receipt != nil {
		b, _ := json.Marshal(receipt)
		bind("receipt", b, nil)
	}
	s.view.Digest = hex.EncodeToString(h.Sum(nil))
	s.view.CanActivate = coherent && (receipt == nil || receipt.Phase == "completed" && receipt.Marker != s.markerDigest)
	return s, nil
}

func (m *Manager) InspectRecovery(ctx context.Context, runtime RecoveryRuntime) (RecoveryInspection, error) {
	ctx, release, err := m.authority.AcquireForRecoveryContext(ctx, m.gateTimeout)
	if err != nil {
		return RecoveryInspection{}, ErrNodeRecoveryRequired
	}
	defer release()
	s, err := m.recoverySnapshot(ctx, runtime)
	return s.view, err
}

// RecoverCurrent never rewrites registry/outbounds/previous and never rolls back
// or retries native activation. A durable intent fences replay across CLI crash.
func (m *Manager) RecoverCurrent(ctx context.Context, digest string, runtime RecoveryRuntime) (resultErr error) {
	if len(digest) != 64 || m.tx.Activator == nil {
		return ErrNodeRecoveryRequired
	}
	ctx, release, err := m.authority.AcquireForRecoveryContext(ctx, m.gateTimeout)
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	defer release()
	m.authority.Block()
	budget := m.tx.Budget.normalized()
	ctx, cancel := context.WithTimeout(ctx, budget.Total)
	defer cancel()
	s, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || !s.view.CanActivate || s.view.Digest != digest {
		return ErrPreviewStale
	}
	candidate, err := os.MkdirTemp("", "xkeen-node-recovery-")
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	defer os.RemoveAll(candidate)
	names := make([]string, 0, len(s.configs))
	for name := range s.configs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if atomicWrite(filepath.Join(candidate, name), s.configs[name], 0600) != nil {
			return ErrNodeRecoveryRequired
		}
	}
	validation, cancelValidation := context.WithTimeout(ctx, budget.CandidateValidation)
	err = m.tx.Activator.ValidateCandidate(validation, candidate)
	cancelValidation()
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	before, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || before.view.Digest != digest {
		return ErrPreviewStale
	}
	receipt := recoveryReceipt{Schema: 1, Phase: "activation-intent", Digest: digest, Marker: s.markerDigest, RuntimeBefore: s.runtime}
	save := func() error {
		b, e := json.Marshal(receipt)
		if e != nil {
			return e
		}
		if e = atomicWrite(filepath.Join(m.recoveryDir(), recoveryReceiptName), b, 0600); e != nil {
			return e
		}
		return syncNodeDirectory(m.recoveryDir())
	}
	if save() != nil {
		return ErrNodeRecoveryRequired
	}
	activation, cancelActivation := context.WithTimeout(ctx, budget.Activation)
	err = m.tx.activate(activation, s.registry)
	cancelActivation()
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	after, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || after.runtime == s.runtime || after.runtime == "stopped" {
		return ErrNodeRecoveryRequired
	}
	if s.generationDigest != after.generationDigest {
		return ErrNodeRecoveryRequired
	}
	receipt.Phase = "verified"
	receipt.RuntimeAfter = after.runtime
	if save() != nil {
		return ErrNodeRecoveryRequired
	}
	if settleNodeIntent(filepath.Join(m.recoveryDir(), ".pending"), s.marker) != nil {
		return ErrNodeRecoveryRequired
	}
	receipt.Phase = "completed"
	if save() != nil {
		return ErrNodeRecoveryRequired
	}
	m.authority.Unblock()
	return nil
}
