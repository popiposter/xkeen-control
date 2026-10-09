package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
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
	CanVerify      bool   `json:"canVerify"`
	Reason         string `json:"reason,omitempty"`
	Nodes          int    `json:"nodes"`
}

type recoveryReceipt struct {
	Schema          int    `json:"schemaVersion"`
	Phase           string `json:"phase"`
	Digest          string `json:"digest"`
	Marker          string `json:"marker"`
	RuntimeBefore   string `json:"runtimeBefore"`
	RuntimeAfter    string `json:"runtimeAfter,omitempty"`
	GenerationProof string `json:"generationProof,omitempty"`
	Stage           string `json:"stage,omitempty"`
	Reason          string `json:"reason,omitempty"`
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
	generationProof  string
	originalDigest   string
	receiptIdentity  string
}

func (m *Manager) recoveryDir() string {
	if m.tx.PreviousDir != "" {
		return m.tx.PreviousDir
	}
	return filepath.Join(filepath.Dir(m.store.Path), "previous")
}

func readRecoveryReceipt(dir string) (*recoveryReceipt, error) {
	r, _, err := readRecoveryReceiptIdentity(dir)
	return r, err
}

func recoveryHex(v string) bool {
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == v
}

func readRecoveryReceiptIdentity(dir string) (*recoveryReceipt, string, error) {
	data, info, err := recoveryRead(filepath.Join(dir, recoveryReceiptName), 8192, true)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", ErrNodeRecoveryRequired
	}
	var r recoveryReceipt
	if decodeStrictJSON(data, &r) != nil || r.Schema != 1 || (r.Phase != "validation-failed" && r.Phase != "activation-intent" && r.Phase != "verified" && r.Phase != "completed") || !recoveryHex(r.Digest) || !recoveryHex(r.Marker) || (r.RuntimeBefore != "stopped" && !recoveryHex(r.RuntimeBefore)) || (r.RuntimeAfter != "" && !recoveryHex(r.RuntimeAfter)) || (r.GenerationProof != "" && !recoveryHex(r.GenerationProof)) {
		return nil, "", ErrNodeRecoveryRequired
	}
	if (r.Phase == "verified" || r.Phase == "completed") && !recoveryHex(r.RuntimeAfter) {
		return nil, "", ErrNodeRecoveryRequired
	}
	if !validRecoveryStage(r.Stage) || (r.Reason != "" && r.Reason != "failed" && r.Reason != "unknown") {
		return nil, "", ErrNodeRecoveryRequired
	}
	return &r, recoveryIdentity(info, data), nil
}

func validRecoveryStage(s string) bool {
	switch s {
	case "", "validation", "restart", "readiness", "inventory", "post-runtime", "generation-drift", "settlement":
		return true
	}
	return false
}

// Keep the original schema-one framing exact; legacy pre-intent digests are
// reconstructed from the same ordered bytes, never from a hash-of-a-hash.
func bindRecovery(h hash.Hash, name string, b []byte, info os.FileInfo) {
	fmt.Fprintf(h, "%s\x00%d\x00", name, len(b))
	h.Write(b)
	if info != nil {
		fmt.Fprint(h, recoveryIdentity(info, b))
	}
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
	receipt, receiptIdentity, err := readRecoveryReceiptIdentity(dir)
	if err != nil {
		return s, err
	}
	s.receipt = receipt
	s.receiptIdentity = receiptIdentity
	if receipt != nil {
		s.view.Phase = receipt.Phase
	}
	marker, info, err := recoveryRead(filepath.Join(dir, ".pending"), 128, true)
	if errors.Is(err, os.ErrNotExist) && receipt != nil {
		s.view.Classification = "recovery-unsettled"
		if receipt.Phase == "completed" {
			s.view.Classification = "completed-receipt"
			s.view.Digest = receiptIdentity
			return s, nil
		}
		if receipt.Phase != "verified" || !recoveryHex(receipt.GenerationProof) {
			s.view.Reason = "missing-generation-proof"
			return s, nil
		}
		err = nil
	}
	if err != nil || (info != nil && string(marker) != "node-operation-pending\n") {
		return s, ErrNodeRecoveryRequired
	}
	s.marker = info
	if info != nil {
		s.markerDigest = recoveryIdentity(info, marker)
	}
	runtimeID, err := runtime.Snapshot(ctx)
	if err != nil || (runtimeID != "stopped" && !recoveryHex(runtimeID)) {
		return s, ErrNodeRecoveryRequired
	}
	s.runtime = runtimeID
	h, original, generation := sha256.New(), sha256.New(), sha256.New()
	bind := func(name string, b []byte, info os.FileInfo) {
		bindRecovery(h, name, b, info)
		bindRecovery(original, name, b, info)
		if name != "marker" {
			bindRecovery(generation, name, b, info)
		}
	}
	if info != nil {
		bind("marker", marker, info)
	}
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
	s.generationProof = hex.EncodeToString(generation.Sum(nil))
	bindRecovery(h, "runtime", []byte(runtimeID), nil)
	if receipt != nil {
		bindRecovery(original, "runtime", []byte(receipt.RuntimeBefore), nil)
		s.originalDigest = hex.EncodeToString(original.Sum(nil))
		b, _ := json.Marshal(receipt)
		bindRecovery(h, "receipt", b, nil)
		bindRecovery(h, "receipt-identity", []byte(receiptIdentity), nil)
	}
	s.view.Digest = hex.EncodeToString(h.Sum(nil))
	s.view.CanActivate = s.marker != nil && coherent && (receipt == nil || receipt.Phase == "completed" && receipt.Marker != s.markerDigest)
	if receipt != nil && (receipt.Phase == "activation-intent" || receipt.Phase == "verified") {
		proof := s.marker != nil && receipt.Marker == s.markerDigest && s.originalDigest == receipt.Digest
		if receipt.Phase == "verified" && recoveryHex(receipt.GenerationProof) {
			proof = receipt.GenerationProof == s.generationProof && (s.marker == nil || receipt.Marker == s.markerDigest) && runtimeID == receipt.RuntimeAfter
		}
		s.view.CanVerify = coherent && proof && runtimeID != "stopped" && runtimeID != receipt.RuntimeBefore
		if !s.view.CanVerify {
			s.view.Reason = "generation-or-runtime-unproven"
		}
	}
	return s, nil
}

func saveRecoveryReceipt(dir string, receipt *recoveryReceipt) error {
	b, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(dir, recoveryReceiptName), b, 0600); err != nil {
		return err
	}
	return syncNodeDirectory(dir)
}

// VerifyExistingRecovery validates and settles one retained attempt. It never
// calls a lifecycle action, including when verification fails or is repeated.
func (m *Manager) VerifyExistingRecovery(ctx context.Context, digest string, runtime RecoveryRuntime) error {
	return m.verifyExistingRecovery(ctx, digest, runtime, saveRecoveryReceipt, settleNodeIntent)
}

// Dependency seams are private to deterministic crash fixtures; production
// always uses the fixed durable writer and identity-checked settlement above.
func (m *Manager) verifyExistingRecovery(ctx context.Context, digest string, runtime RecoveryRuntime, save func(string, *recoveryReceipt) error, settle func(string, os.FileInfo) error) error {
	if !recoveryHex(digest) || m.tx.Activator == nil {
		return ErrNodeRecoveryRequired
	}
	ctx, release, err := m.authority.AcquireForRecoveryContext(ctx, m.gateTimeout)
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, m.tx.Budget.normalized().Total)
	defer cancel()
	s, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || s.view.Digest != digest {
		return ErrPreviewStale
	}
	if s.view.Classification == "completed-receipt" {
		return nil
	}
	if !s.view.CanVerify {
		return ErrNodeRecoveryRequired
	}
	m.authority.Block()
	// The matched original digest already binds the fully validated generation.
	// Recheck readiness and inventory without rendering or invoking Restart.
	if m.tx.Activator.WaitReady(ctx) != nil || m.tx.Activator.VerifyOutboundTags(ctx, enabledTags(s.registry)) != nil {
		return ErrNodeRecoveryRequired
	}
	after, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || !after.view.CanVerify || after.view.Digest != digest || after.runtime != s.runtime || after.receiptIdentity != s.receiptIdentity {
		return ErrPreviewStale
	}
	receipt := *after.receipt
	receipt.Phase, receipt.Stage, receipt.Reason = "verified", "settlement", ""
	receipt.RuntimeAfter, receipt.GenerationProof = after.runtime, after.generationProof
	if save(m.recoveryDir(), &receipt) != nil {
		return ErrNodeRecoveryRequired
	}
	// Re-read after the verified write as well. This captures concurrent native
	// writers without treating the panel lock as exclusion of external CLI/cron.
	final, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || !final.view.CanVerify || final.runtime != after.runtime || final.generationProof != after.generationProof || final.receipt == nil || *final.receipt != receipt {
		return ErrNodeRecoveryRequired
	}
	if final.marker != nil {
		if settle(filepath.Join(m.recoveryDir(), ".pending"), final.marker) != nil {
			return ErrNodeRecoveryRequired
		}
	}
	receipt.Phase = "completed"
	if save(m.recoveryDir(), &receipt) != nil {
		return ErrNodeRecoveryRequired
	}
	m.authority.Unblock()
	return nil
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
	receipt := recoveryReceipt{Schema: 1, Phase: "validation-failed", Digest: digest, Marker: s.markerDigest, RuntimeBefore: s.runtime, Stage: "validation"}
	fail := func(stage, reason string) error {
		receipt.Stage, receipt.Reason = stage, reason
		_ = saveRecoveryReceipt(m.recoveryDir(), &receipt)
		return ErrNodeRecoveryRequired
	}
	candidate, err := os.MkdirTemp("", "xkeen-node-recovery-")
	if err != nil {
		return fail("validation", "failed")
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
		return fail("validation", "failed")
	}
	before, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || before.view.Digest != digest {
		return fail("generation-drift", "failed")
	}
	receipt.Phase, receipt.Stage = "activation-intent", "restart"
	save := func() error {
		return saveRecoveryReceipt(m.recoveryDir(), &receipt)
	}
	if save() != nil {
		return ErrNodeRecoveryRequired
	}
	activation, cancelActivation := context.WithTimeout(ctx, budget.Activation)
	err = m.tx.Activator.Restart(activation)
	if err == nil {
		receipt.Stage = "readiness"
		if err = save(); err == nil {
			err = m.tx.Activator.WaitReady(activation)
		}
	}
	if err == nil {
		receipt.Stage = "inventory"
		if err = save(); err == nil {
			err = m.tx.Activator.VerifyOutboundTags(activation, enabledTags(s.registry))
		}
	}
	cancelActivation()
	if err != nil {
		return fail(receipt.Stage, "unknown")
	}
	after, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || after.runtime == s.runtime || after.runtime == "stopped" {
		return fail("post-runtime", "failed")
	}
	if s.generationDigest != after.generationDigest {
		return fail("generation-drift", "failed")
	}
	receipt.Phase = "verified"
	receipt.RuntimeAfter = after.runtime
	receipt.GenerationProof, receipt.Stage = after.generationProof, "settlement"
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
