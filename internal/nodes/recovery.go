package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"
)

const recoveryReceiptName = "node-recovery.json"
const recoveryCompletionFenceName = "node-recovery-completing"

func predecessorName(slot string) string {
	if slot == "a" {
		return "node-recovery-predecessor-a.json"
	}
	if slot == "b" {
		return "node-recovery-predecessor-b.json"
	}
	return "invalid-predecessor-slot"
}

// RecoveryRuntime supplies independent process identity and refuses live native
// writers. Offline callers must hold setup.Maintenance for the entire operation.
// It is not an API or an alternative node mutation owner.
type RecoveryRuntime interface {
	Snapshot(context.Context) (string, error)
}

type RecoveryInspection struct {
	Classification      string        `json:"classification"`
	Previous            string        `json:"previous"`
	Digest              string        `json:"digest,omitempty"`
	Phase               string        `json:"phase,omitempty"`
	CanActivate         bool          `json:"canActivate"`
	CanVerify           bool          `json:"canVerify"`
	Reason              string        `json:"reason,omitempty"`
	Nodes               int           `json:"nodes"`
	Branch              string        `json:"branch,omitempty"`
	Stage               string        `json:"stage,omitempty"`
	Failure             string        `json:"failure,omitempty"`
	ElapsedMS           int64         `json:"elapsedMs,omitempty"`
	StageElapsedMS      int64         `json:"stageElapsedMs,omitempty"`
	RollbackFailure     string        `json:"rollbackFailure,omitempty"`
	ActivationFailure   string        `json:"activationFailure,omitempty"`
	ActivationElapsedMS int64         `json:"activationElapsedMs,omitempty"`
	ActivationTiming    *StageTimings `json:"activationTiming,omitempty"`
	RollbackTiming      *StageTimings `json:"rollbackTiming,omitempty"`
}

type recoveryReceipt struct {
	Schema              int           `json:"schemaVersion"`
	Phase               string        `json:"phase"`
	Digest              string        `json:"digest"`
	Marker              string        `json:"marker"`
	RuntimeBefore       string        `json:"runtimeBefore"`
	RuntimeAfter        string        `json:"runtimeAfter,omitempty"`
	GenerationProof     string        `json:"generationProof,omitempty"`
	Stage               string        `json:"stage,omitempty"`
	Reason              string        `json:"reason,omitempty"`
	Owner               string        `json:"owner,omitempty"`
	Branch              string        `json:"branch,omitempty"`
	BeforeProof         string        `json:"beforeProof,omitempty"`
	StableProof         string        `json:"stableProof,omitempty"`
	CandidateContent    string        `json:"candidateContent,omitempty"`
	PreviousContent     string        `json:"previousContent,omitempty"`
	RollbackFailure     string        `json:"rollbackFailure,omitempty"`
	ActivationFailure   string        `json:"activationFailure,omitempty"`
	Predecessor         string        `json:"predecessor,omitempty"`
	PredecessorOriginal string        `json:"predecessorOriginal,omitempty"`
	PredecessorSlot     string        `json:"predecessorSlot,omitempty"`
	ElapsedMS           int64         `json:"elapsedMs,omitempty"`
	StageElapsedMS      int64         `json:"stageElapsedMs,omitempty"`
	ActivationElapsedMS int64         `json:"activationElapsedMs,omitempty"`
	ActivationTiming    *StageTimings `json:"activationTiming,omitempty"`
	RollbackTiming      *StageTimings `json:"rollbackTiming,omitempty"`
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
	if decodeStrictJSON(data, &r) != nil || r.Schema != 1 || !validRecoveryPhase(r.Phase) || !recoveryHex(r.Digest) || !recoveryHex(r.Marker) || (r.RuntimeBefore != "stopped" && !recoveryHex(r.RuntimeBefore)) || (r.RuntimeAfter != "" && !recoveryHex(r.RuntimeAfter) && !(r.Branch == "metadata" && r.RuntimeAfter == "stopped")) || (r.GenerationProof != "" && !recoveryHex(r.GenerationProof)) {
		return nil, "", ErrNodeRecoveryRequired
	}
	if (r.Phase == "verified" || r.Phase == "completed") && !recoveryHex(r.RuntimeAfter) && !(r.Branch == "metadata" && r.RuntimeAfter == "stopped") {
		return nil, "", ErrNodeRecoveryRequired
	}
	if !validRecoveryStage(r.Stage) || !validRecoveryReason(r.Stage, r.Reason) {
		return nil, "", ErrNodeRecoveryRequired
	}
	if r.Owner != "" && r.Owner != "transaction" && r.Owner != "recovery" {
		return nil, "", ErrNodeRecoveryRequired
	}
	if r.Branch != "" && r.Branch != "candidate" && r.Branch != "previous" && r.Branch != "metadata" && r.Branch != "current" {
		return nil, "", ErrNodeRecoveryRequired
	}
	for _, v := range []string{r.BeforeProof, r.StableProof, r.CandidateContent, r.PreviousContent, r.Predecessor, r.PredecessorOriginal} {
		if v != "" && !recoveryHex(v) {
			return nil, "", ErrNodeRecoveryRequired
		}
	}
	if ((r.ActivationTiming != nil || r.RollbackTiming != nil) && r.Owner == "") || !validStageTimings(r.ActivationTiming) || !validStageTimings(r.RollbackTiming) || (r.Branch == "metadata" && (r.ActivationTiming != nil || r.RollbackTiming != nil)) || (r.RollbackTiming != nil && (r.Owner != "transaction" || r.Branch != "previous")) {
		return nil, "", ErrNodeRecoveryRequired
	}
	if r.ElapsedMS < 0 || r.ElapsedMS > int64((24*time.Hour)/time.Millisecond) || r.StageElapsedMS < 0 || r.StageElapsedMS > r.ElapsedMS || r.ActivationElapsedMS < 0 || r.ActivationElapsedMS > r.ElapsedMS {
		return nil, "", ErrNodeRecoveryRequired
	}
	if (r.ActivationFailure != "" && !validFailureCode(r.ActivationFailure)) || (r.RollbackFailure != "" && !validFailureCode(r.RollbackFailure)) {
		return nil, "", ErrNodeRecoveryRequired
	}
	if (r.Predecessor == "") != (r.PredecessorOriginal == "") {
		return nil, "", ErrNodeRecoveryRequired
	}
	if r.Owner == "transaction" && (r.Branch == "" || !recoveryHex(r.BeforeProof) || !recoveryHex(r.StableProof) || !recoveryHex(r.CandidateContent) || !recoveryHex(r.PreviousContent)) {
		return nil, "", ErrNodeRecoveryRequired
	}
	if r.Owner == "recovery" && (r.Branch != "current" || !recoveryHex(r.GenerationProof)) {
		return nil, "", ErrNodeRecoveryRequired
	}
	if r.Owner == "" && (r.Branch != "" || r.Predecessor != "" || r.BeforeProof != "" || r.StableProof != "" || r.CandidateContent != "" || r.PreviousContent != "" || r.Phase == "prepared" || r.Phase == "committing" || r.Phase == "restoring" || r.Phase == "inspection-required") {
		return nil, "", ErrNodeRecoveryRequired
	}
	if (r.Predecessor == "" && r.PredecessorSlot != "") || (r.Predecessor != "" && r.PredecessorSlot != "a" && r.PredecessorSlot != "b") {
		return nil, "", ErrNodeRecoveryRequired
	}
	if r.Owner != "" && (r.Phase == "verified" || r.Phase == "completed") {
		if !recoveryHex(r.GenerationProof) || (r.Branch == "metadata" && r.RuntimeAfter != r.RuntimeBefore) || (r.Branch != "metadata" && (r.RuntimeAfter == r.RuntimeBefore || !recoveryHex(r.RuntimeAfter))) {
			return nil, "", ErrNodeRecoveryRequired
		}
	}
	return &r, recoveryIdentity(info, data), nil
}

func validRecoveryPhase(s string) bool {
	switch s {
	case "validation-failed", "activation-intent", "verified", "completed", "prepared", "committing", "restoring", "inspection-required":
		return true
	}
	return false
}
func validRecoveryReason(stage, reason string) bool {
	switch reason {
	case "", "failed", "unknown", "deadline", "canceled":
		return true
	case "unavailable", "unsupported", "permission", "protocol":
		return stage == "readiness"
	}
	return false
}
func validFailureCode(s string) bool {
	for _, reason := range []string{"unavailable", "unsupported", "permission", "protocol"} {
		if s == "readiness:"+reason {
			return true
		}
	}
	for _, stage := range []string{"commit", "restart", "readiness", "inventory", "post-runtime", "generation-drift", "restore", "settlement"} {
		for _, reason := range []string{"failed", "unknown", "deadline", "canceled"} {
			if s == stage+":"+reason {
				return true
			}
		}
	}
	return false
}
func validRecoveryStage(s string) bool {
	switch s {
	case "", "prepared", "commit", "restore", "validation", "restart", "readiness", "inventory", "post-runtime", "generation-drift", "settlement":
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
	if _, err := os.Lstat(filepath.Join(dir, recoveryCompletionFenceName)); !errors.Is(err, os.ErrNotExist) {
		return true
	}
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
	fence, fenceInfo, fenceErr := recoveryRead(filepath.Join(dir, recoveryCompletionFenceName), 65, true)
	fenced := fenceErr == nil
	if fenced && (receipt == nil || string(fence) != receipt.Digest+"\n") || (fenceErr != nil && !errors.Is(fenceErr, os.ErrNotExist)) {
		return s, ErrNodeRecoveryRequired
	}
	if receipt != nil {
		s.view.Phase = receipt.Phase
		s.view.ActivationTiming, s.view.RollbackTiming = receipt.ActivationTiming, receipt.RollbackTiming
		s.view.Branch, s.view.Stage, s.view.Failure, s.view.ElapsedMS = receipt.Branch, receipt.Stage, receipt.Reason, receipt.ElapsedMS
		s.view.StageElapsedMS, s.view.ActivationFailure, s.view.ActivationElapsedMS = receipt.StageElapsedMS, receipt.ActivationFailure, receipt.ActivationElapsedMS
		s.view.RollbackFailure = receipt.RollbackFailure
	}
	marker, info, err := recoveryRead(filepath.Join(dir, ".pending"), 128, true)
	if errors.Is(err, os.ErrNotExist) && receipt != nil {
		s.view.Classification = "recovery-unsettled"
		if receipt.Phase == "completed" && !fenced {
			s.view.Classification = "completed-receipt"
			s.view.Digest = receiptIdentity
			return s, nil
		}
		if (receipt.Phase != "verified" && !(receipt.Phase == "completed" && fenced)) || !recoveryHex(receipt.GenerationProof) {
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
	g, err := captureNodeGeneration(m.tx, bind)
	if err != nil {
		return s, err
	}
	s.configs = g.configs
	registry, coherent := coherentRecoveryPair(g.registry, s.configs["04_outbounds.json"])
	s.registry = registry
	s.view.Nodes = len(registry.Nodes)
	if coherent {
		s.view.Classification = "current-coherent"
	}
	if _, ok := coherentRecoveryPair(g.previous["nodes.json"], g.previous["04_outbounds.json"]); ok && g.previous[".registry-absent"] == nil && g.previous[".outbounds-absent"] == nil {
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
	if fenced {
		bindRecovery(h, "completion-fence", fence, fenceInfo)
	}
	s.view.Digest = hex.EncodeToString(h.Sum(nil))
	s.view.CanActivate = !fenced && s.marker != nil && coherent && (receipt == nil || receipt.Phase == "completed" && receipt.Marker != s.markerDigest)
	if receipt != nil && (receipt.Phase == "activation-intent" || receipt.Phase == "inspection-required" || receipt.Phase == "verified" || receipt.Phase == "completed" && fenced) {
		proof := s.marker != nil && receipt.Marker == s.markerDigest && s.originalDigest == receipt.Digest
		if receipt.Owner != "" {
			proof = s.marker != nil && receipt.Marker == s.markerDigest && recoveryHex(receipt.GenerationProof) && receipt.GenerationProof == s.generationProof && (receipt.Stage == "restart" || receipt.Stage == "readiness" || receipt.Stage == "inventory" || receipt.Stage == "post-runtime" || receipt.Stage == "settlement")
		}
		if (receipt.Phase == "verified" || receipt.Phase == "completed") && recoveryHex(receipt.GenerationProof) {
			proof = receipt.GenerationProof == s.generationProof && (s.marker == nil || receipt.Marker == s.markerDigest) && runtimeID == receipt.RuntimeAfter
		}
		runtimeProven := runtimeID != "stopped" && runtimeID != receipt.RuntimeBefore
		if receipt.Branch == "metadata" {
			runtimeProven = runtimeID == receipt.RuntimeBefore && (receipt.Phase == "verified" || receipt.Phase == "completed")
		}
		s.view.CanVerify = coherent && proof && runtimeProven && (receipt.RuntimeAfter == "" || runtimeID == receipt.RuntimeAfter)
		if !s.view.CanVerify {
			s.view.Reason = "generation-or-runtime-unproven"
		}
	}
	if receipt != nil && receipt.Predecessor != "" {
		b, i, e := recoveryRead(filepath.Join(dir, predecessorName(receipt.PredecessorSlot)), 8192, true)
		if e != nil || recoveryIdentity(i, b) != receipt.Predecessor {
			return s, ErrNodeRecoveryRequired
		}
	}
	return s, nil
}

func saveRecoveryReceipt(dir string, receipt *recoveryReceipt) error {
	_, err := writeRecoveryReceipt(dir, receipt)
	return err
}

func writeRecoveryReceipt(dir string, receipt *recoveryReceipt) (string, error) {
	return writeRecoveryReceiptWithSync(dir, receipt, (*os.File).Sync, syncNodeDirectory)
}

func writeRecoveryReceiptWithSync(dir string, receipt *recoveryReceipt, syncFile func(*os.File) error, syncDir func(string) error) (string, error) {
	b, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	// Capture the inode we created, not whichever inode occupies the final path
	// after rename. Equal-byte replacement is still external receipt drift.
	f, err := os.CreateTemp(dir, ".xkeen-node-recovery-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		return "", err
	}
	if err = syncFile(f); err != nil {
		return "", err
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	ownedIdentity := recoveryIdentity(info, b)
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, recoveryReceiptName)); err != nil {
		return "", err
	}
	if err = syncDir(dir); err != nil {
		return "", err
	}
	r, identity, err := readRecoveryReceiptIdentity(dir)
	if err != nil || r == nil || !reflect.DeepEqual(r, receipt) || identity != ownedIdentity {
		return "", ErrNodeRecoveryRequired
	}
	return identity, nil
}

// VerifyExistingRecovery validates and settles one retained attempt. It never
// calls a lifecycle action, including when verification fails or is repeated.
func (m *Manager) VerifyExistingRecovery(ctx context.Context, digest string, runtime RecoveryRuntime) error {
	return m.verifyExistingRecovery(ctx, digest, runtime, writeRecoveryReceipt, settleNodeIntent)
}

// Dependency seams are private to deterministic crash fixtures; production
// always uses the fixed durable writer and identity-checked settlement above.
func (m *Manager) verifyExistingRecovery(ctx context.Context, digest string, runtime RecoveryRuntime, save func(string, *recoveryReceipt) (string, error), settle func(string, os.FileInfo) error) error {
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
	if s.receipt.Branch != "metadata" && (m.tx.Activator.WaitReady(ctx) != nil || m.tx.Activator.VerifyOutboundTags(ctx, enabledTags(s.registry)) != nil) {
		return ErrNodeRecoveryRequired
	}
	after, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || !after.view.CanVerify || after.view.Digest != digest || after.runtime != s.runtime || after.receiptIdentity != s.receiptIdentity {
		return ErrPreviewStale
	}
	receipt := *after.receipt
	receipt.Phase, receipt.Stage, receipt.Reason = "verified", "settlement", ""
	receipt.RuntimeAfter, receipt.GenerationProof = after.runtime, after.generationProof
	ownedIdentity, err := save(m.recoveryDir(), &receipt)
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	// Re-read after the verified write as well. This captures concurrent native
	// writers without treating the panel lock as exclusion of external CLI/cron.
	final, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || !final.view.CanVerify || final.runtime != after.runtime || final.generationProof != after.generationProof || final.receipt == nil || !reflect.DeepEqual(*final.receipt, receipt) || final.receiptIdentity != ownedIdentity {
		return ErrNodeRecoveryRequired
	}
	if final.marker != nil {
		if _, err := beginRecoveryCompletion(m.recoveryDir(), receipt.Digest); err != nil {
			return ErrNodeRecoveryRequired
		}
		if settle(filepath.Join(m.recoveryDir(), ".pending"), final.marker) != nil {
			return ErrNodeRecoveryRequired
		}
	}
	settled, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || settled.marker != nil || !settled.view.CanVerify || settled.runtime != after.runtime || settled.generationProof != after.generationProof || settled.receiptIdentity != ownedIdentity || settled.receipt == nil || !reflect.DeepEqual(*settled.receipt, receipt) {
		return ErrNodeRecoveryRequired
	}
	if m.completeRecovery(ctx, runtime, &receipt, save) != nil {
		return ErrNodeRecoveryRequired
	}
	m.authority.Unblock()
	return nil
}

// A durable fence keeps a completed rename with a failed fsync from admitting
// a fresh daemon. Its identity-checked unlink is the final commit, only after
// the completed receipt and generation/runtime proof have been re-read.
func beginRecoveryCompletion(dir, digest string) (os.FileInfo, error) {
	return beginRecoveryCompletionWithSync(dir, digest, (*os.File).Sync, syncNodeDirectory)
}

func beginRecoveryCompletionWithSync(dir, digest string, syncFile func(*os.File) error, syncDir func(string) error) (os.FileInfo, error) {
	path := filepath.Join(dir, recoveryCompletionFenceName)
	want := digest + "\n"
	b, info, err := recoveryRead(path, 65, true)
	if errors.Is(err, os.ErrNotExist) {
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		_, e = f.WriteString(want)
		if e == nil {
			e = syncFile(f)
		}
		info, err = f.Stat()
		closeErr := f.Close()
		if e != nil || err != nil || closeErr != nil {
			return nil, ErrNodeRecoveryRequired
		}
	} else {
		if err != nil || string(b) != want {
			return nil, ErrNodeRecoveryRequired
		}
		f, e := os.Open(path)
		if e != nil {
			return nil, e
		}
		actual, e := f.Stat()
		if e == nil && os.SameFile(info, actual) {
			e = syncFile(f)
		} else {
			e = ErrNodeRecoveryRequired
		}
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			return nil, ErrNodeRecoveryRequired
		}
	}
	if syncDir(dir) != nil {
		return nil, ErrNodeRecoveryRequired
	}
	b, actual, err := recoveryRead(path, 65, true)
	if err != nil || string(b) != want || !os.SameFile(info, actual) {
		return nil, ErrNodeRecoveryRequired
	}
	return actual, nil
}

func (m *Manager) completeRecovery(ctx context.Context, runtime RecoveryRuntime, receipt *recoveryReceipt, save func(string, *recoveryReceipt) (string, error)) error {
	fence, err := beginRecoveryCompletion(m.recoveryDir(), receipt.Digest)
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	receipt.Phase = "completed"
	identity, err := save(m.recoveryDir(), receipt)
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	s, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || !s.view.CanVerify || s.marker != nil || s.generationProof != receipt.GenerationProof || s.runtime != receipt.RuntimeAfter || s.receiptIdentity != identity || s.receipt == nil || !reflect.DeepEqual(s.receipt, receipt) {
		return ErrNodeRecoveryRequired
	}
	return finishRecoveryCompletion(m.recoveryDir(), receipt.Digest, fence, os.Remove, syncNodeDirectory)
}

func finishRecoveryCompletion(dir, digest string, fence os.FileInfo, remove func(string) error, syncDir func(string) error) error {
	path := filepath.Join(dir, recoveryCompletionFenceName)
	b, current, err := recoveryRead(path, 65, true)
	if err != nil || string(b) != digest+"\n" || !os.SameFile(fence, current) {
		return ErrNodeRecoveryRequired
	}
	if err = remove(path); err != nil {
		return ErrNodeRecoveryRequired
	}
	// The completed receipt is already durable. A failed cleanup sync can only
	// resurrect the conservative fence on crash; it cannot revoke this commit.
	if syncDir(dir) != nil {
		log.Print("node recovery completion fence cleanup sync deferred")
	}
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
	receipt := recoveryReceipt{Schema: 1, Phase: "validation-failed", Digest: digest, Marker: s.markerDigest, RuntimeBefore: s.runtime, Stage: "validation", Owner: "recovery", Branch: "current", GenerationProof: s.generationProof}

	expectedIdentity := s.receiptIdentity
	owned := false
	save := func() error {
		_, current, e := readRecoveryReceiptIdentity(m.recoveryDir())
		if e != nil || current != expectedIdentity {
			return ErrNodeRecoveryRequired
		}
		if !owned {
			receipt.Predecessor, receipt.PredecessorOriginal, receipt.PredecessorSlot, e = preserveCompletedPredecessor(m.recoveryDir())
			if e != nil || receipt.PredecessorOriginal != expectedIdentity {
				return ErrNodeRecoveryRequired
			}
		}
		identity, e := writeRecoveryReceipt(m.recoveryDir(), &receipt)
		if e != nil {
			return ErrNodeRecoveryRequired
		}
		expectedIdentity = identity
		owned = true
		return nil
	}
	fail := func(stage, reason string) error {
		receipt.Stage, receipt.Reason = stage, reason
		if stage != "validation" {
			receipt.ActivationFailure = stage + ":" + reason
		}
		_ = save()
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
	if save() != nil {
		return ErrNodeRecoveryRequired
	}
	activation, cancelActivation := context.WithTimeout(ctx, budget.Activation)
	started := time.Now()
	err = m.tx.Activator.Restart(activation)
	recordStageTiming(&receipt.ActivationTiming, "restart", time.Since(started), 0)
	if err == nil {
		receipt.Stage = "post-runtime"
		id, e := runtime.Snapshot(activation)
		if e != nil || id == "stopped" || id == receipt.RuntimeBefore {
			err = ErrNodeRecoveryRequired
		} else {
			receipt.RuntimeAfter = id
		}
	}
	if err == nil {
		receipt.Stage = "readiness"
		if err = save(); err == nil {
			allowance, ready := activationReadiness(activation, m.tx.Activator)
			started = time.Now()
			err = ready()
			recordStageTiming(&receipt.ActivationTiming, "readiness", time.Since(started), allowance)
		}
	}
	if err == nil {
		receipt.Stage = "inventory"
		if err = save(); err == nil {
			started = time.Now()
			err = m.tx.Activator.VerifyOutboundTags(activation, enabledTags(s.registry))
			recordStageTiming(&receipt.ActivationTiming, "inventory", time.Since(started), 0)
		}
	}
	cancelActivation()
	if err != nil {
		reason := "unknown"
		if receipt.Stage == "readiness" {
			reason = failureReason(receipt.Stage, err)
		}
		return fail(receipt.Stage, reason)
	}
	after, err := m.recoverySnapshot(ctx, runtime)
	if err != nil || after.runtime == s.runtime || after.runtime == "stopped" || after.runtime != receipt.RuntimeAfter {
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
	if _, err := beginRecoveryCompletion(m.recoveryDir(), receipt.Digest); err != nil {
		return ErrNodeRecoveryRequired
	}
	if settleNodeIntent(filepath.Join(m.recoveryDir(), ".pending"), s.marker) != nil {
		return ErrNodeRecoveryRequired
	}
	if m.completeRecovery(ctx, runtime, &receipt, writeRecoveryReceipt) != nil {
		return ErrNodeRecoveryRequired
	}
	m.authority.Unblock()
	return nil
}
