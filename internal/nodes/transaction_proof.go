package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/popiposter/xkeen-control/internal/xkeen"
)

type nodeGeneration struct {
	registry                    []byte
	configs, previous           map[string][]byte
	proof, content, stableProof string
}

// This is the same ordered framing used by legacy recovery. Content identity
// predicts a commit; protected identity additionally detects inode replacement.
func captureNodeGeneration(t Transaction, bind func(string, []byte, os.FileInfo)) (nodeGeneration, error) {
	if t.PreviousDir == "" {
		t.PreviousDir = filepath.Join(filepath.Dir(t.Store.Path), "previous")
	}
	g := nodeGeneration{configs: map[string][]byte{}, previous: map[string][]byte{}}
	h, stable := sha256.New(), sha256.New()
	add := func(name string, b []byte, i os.FileInfo) {
		bindRecovery(h, name, b, i)
		if name != "registry" && name != "registry-absent" && name != "04_outbounds.json" {
			bindRecovery(stable, name, b, i)
		}
		if bind != nil {
			bind(name, b, i)
		}
	}
	b, i, err := recoveryRead(t.Store.Path, MaxRegistryDocument, true)
	if errors.Is(err, os.ErrNotExist) {
		add("registry-absent", nil, nil)
	} else if err != nil {
		return g, ErrNodeRecoveryRequired
	} else {
		g.registry = b
		add("registry", b, i)
	}
	names, err := recoveryConfigNames(t.ConfigDir)
	if err != nil {
		return g, ErrNodeRecoveryRequired
	}
	total := 0
	for _, name := range names {
		b, i, e := recoveryRead(filepath.Join(t.ConfigDir, name), MaxLegacyDocument, false)
		if e != nil {
			return g, ErrNodeRecoveryRequired
		}
		total += len(b)
		if total > 8<<20 {
			return g, ErrNodeRecoveryRequired
		}
		g.configs[name] = b
		add(name, b, i)
	}
	for _, name := range []string{"nodes.json", "04_outbounds.json", ".registry-absent", ".outbounds-absent"} {
		b, i, e := recoveryRead(filepath.Join(t.PreviousDir, name), MaxRegistryDocument, true)
		if errors.Is(e, os.ErrNotExist) {
			add("previous-absent:"+name, nil, nil)
			continue
		}
		if e != nil {
			return g, ErrNodeRecoveryRequired
		}
		g.previous[name] = b
		add("previous:"+name, b, i)
	}
	g.proof = hex.EncodeToString(h.Sum(nil))
	g.stableProof = hex.EncodeToString(stable.Sum(nil))
	g.content = g.contentDigest()
	return g, nil
}

func (g nodeGeneration) contentDigest() string {
	h := sha256.New()
	if g.registry == nil {
		bindRecovery(h, "registry-absent", nil, nil)
	} else {
		bindRecovery(h, "registry", g.registry, nil)
	}
	names := make([]string, 0, len(g.configs))
	for name := range g.configs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bindRecovery(h, name, g.configs[name], nil)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// validationIdentity binds the actual source copied into the candidate, before
// validation. Journal capture must not bless an external edit made during it.
func validationIdentity(t Transaction) (string, error) {
	h := sha256.New()
	b, i, e := recoveryRead(t.Store.Path, MaxRegistryDocument, true)
	if errors.Is(e, os.ErrNotExist) {
		bindRecovery(h, "registry-absent", nil, nil)
	} else if e != nil {
		return "", ErrNodeRecoveryRequired
	} else {
		bindRecovery(h, "registry", b, i)
	}
	names, e := recoveryConfigNames(t.ConfigDir)
	if e != nil {
		return "", ErrNodeRecoveryRequired
	}
	total := 0
	for _, n := range names {
		b, i, e := recoveryRead(filepath.Join(t.ConfigDir, n), MaxLegacyDocument, false)
		if e != nil {
			return "", ErrNodeRecoveryRequired
		}
		total += len(b)
		if total > 8<<20 {
			return "", ErrNodeRecoveryRequired
		}
		bindRecovery(h, n, b, i)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func preserveCompletedPredecessor(dir string) (string, string, string, error) {
	r, identity, err := readRecoveryReceiptIdentity(dir)
	if err != nil {
		return "", "", "", err
	}
	if r == nil {
		return "", "", "", nil
	}
	if r.Phase != "completed" {
		return "", "", "", ErrNodeRecoveryRequired
	}
	if r.Predecessor != "" {
		b, i, e := recoveryRead(filepath.Join(dir, predecessorName(r.PredecessorSlot)), 8192, true)
		if e != nil || recoveryIdentity(i, b) != r.Predecessor {
			return "", "", "", ErrNodeRecoveryRequired
		}
	}

	b, i, err := recoveryRead(filepath.Join(dir, recoveryReceiptName), 8192, true)
	if err != nil || recoveryIdentity(i, b) != identity {
		return "", "", "", ErrNodeRecoveryRequired
	}
	slot := "a"
	if r.PredecessorSlot == "a" {
		slot = "b"
	}
	p := filepath.Join(dir, predecessorName(slot))
	if atomicWrite(p, b, 0600) != nil || syncNodeDirectory(dir) != nil {
		return "", "", "", ErrNodeRecoveryRequired
	}
	copy, ci, err := recoveryRead(p, 8192, true)
	if err != nil || string(copy) != string(b) {
		return "", "", "", ErrNodeRecoveryRequired
	}
	return recoveryIdentity(ci, copy), identity, slot, nil
}

type transactionJournal struct {
	t            Transaction
	r            recoveryReceipt
	identity     string
	started      time.Time
	stageStarted time.Time
}

func (j *transactionJournal) write(phase, stage string) error {
	if j == nil {
		return nil
	}
	marker, mi, me := recoveryRead(filepath.Join(j.t.PreviousDir, ".pending"), 128, true)
	if me != nil || recoveryIdentity(mi, marker) != j.r.Marker {
		return ErrNodeRecoveryRequired
	}
	if j.r.Predecessor != "" {
		b, i, e := recoveryRead(filepath.Join(j.t.PreviousDir, predecessorName(j.r.PredecessorSlot)), 8192, true)
		if e != nil || recoveryIdentity(i, b) != j.r.Predecessor {
			return ErrNodeRecoveryRequired
		}
	}
	_, id, e := readRecoveryReceiptIdentity(j.t.PreviousDir)
	if e != nil || id != j.identity {
		return ErrNodeRecoveryRequired
	}

	if j.r.Stage != stage || j.stageStarted.IsZero() {
		j.stageStarted = time.Now()
	}
	j.r.StageElapsedMS = time.Since(j.stageStarted).Milliseconds()
	j.r.Phase, j.r.Stage = phase, stage
	j.r.ElapsedMS = time.Since(j.started).Milliseconds()
	var err error
	j.identity, err = writeRecoveryReceipt(j.t.PreviousDir, &j.r)
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	return nil
}

func (j *transactionJournal) failure(stage string, cause error) error {
	if j == nil {
		return cause
	}
	j.r.Reason = "failed"
	if errors.Is(cause, xkeen.ErrLifecycleUnknown) {
		j.r.Reason = "unknown"
	} else if errors.Is(cause, context.DeadlineExceeded) {
		j.r.Reason = "deadline"
	} else if errors.Is(cause, context.Canceled) {
		j.r.Reason = "canceled"
	}
	if j.r.Branch == "previous" {
		j.r.RollbackFailure = stage + ":" + j.r.Reason
	} else if validFailureCode(stage + ":" + j.r.Reason) {
		j.r.ActivationFailure = stage + ":" + j.r.Reason
	}
	_ = j.write("inspection-required", stage)
	return errors.Join(ErrNodeRecoveryRequired, cause)
}

func (t Transaction) newJournal(ctx context.Context, registry Registry, rendered []byte, runtimeChanged bool, intent *nodeIntent) (*transactionJournal, error) {
	// A nil activator is a file-only library operation, never a production
	// lifecycle owner. Every actual Activator must supply process proof.
	if t.Activator == nil {
		return nil, nil
	}
	g, err := captureNodeGeneration(t, nil)
	if err != nil {
		return nil, err
	}
	runtime, err := t.Activator.RuntimeIdentity(ctx)
	if err != nil || (runtime != "stopped" && !recoveryHex(runtime)) {
		return nil, ErrNodeRecoveryRequired
	}
	marker, mi, err := recoveryRead(filepath.Join(t.PreviousDir, ".pending"), 128, true)
	if err != nil || !os.SameFile(mi, intent.info) {
		return nil, ErrNodeRecoveryRequired
	}
	j := &transactionJournal{t: t, started: time.Now(), r: recoveryReceipt{Schema: 1, Owner: "transaction", Branch: "candidate", RuntimeBefore: runtime, Marker: recoveryIdentity(mi, marker), BeforeProof: g.proof, StableProof: g.stableProof, PreviousContent: g.content}}
	if !runtimeChanged {
		j.r.Branch = "metadata"
	}
	g.registry, err = MarshalCanonical(registry)
	if err != nil {
		return nil, err
	}
	if runtimeChanged {
		g.configs["04_outbounds.json"] = rendered
	}
	j.r.CandidateContent = g.contentDigest()
	previous := g
	previous.configs = make(map[string][]byte, len(g.configs))
	for k, v := range g.configs {
		previous.configs[k] = v
	}
	previous.registry = g.previous["nodes.json"]
	if g.previous[".registry-absent"] != nil {
		previous.registry = nil
	}
	if g.previous[".outbounds-absent"] != nil {
		delete(previous.configs, "04_outbounds.json")
	} else {
		previous.configs["04_outbounds.json"] = g.previous["04_outbounds.json"]
	}
	j.r.PreviousContent = previous.contentDigest()
	h := sha256.New()
	bindRecovery(h, "marker", []byte(j.r.Marker), nil)
	bindRecovery(h, "before", []byte(j.r.BeforeProof), nil)
	bindRecovery(h, "candidate", []byte(j.r.CandidateContent), nil)
	bindRecovery(h, "runtime", []byte(runtime), nil)
	j.r.Digest = hex.EncodeToString(h.Sum(nil))
	j.r.Predecessor, j.r.PredecessorOriginal, j.r.PredecessorSlot, err = preserveCompletedPredecessor(t.PreviousDir)
	if err != nil {
		return nil, err
	}
	j.identity = j.r.PredecessorOriginal
	if err = j.write("prepared", "prepared"); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *transactionJournal) bindBranch(ctx context.Context, branch string, expected string) error {
	if j == nil {
		return nil
	}
	g, err := captureNodeGeneration(j.t, nil)
	if err != nil || g.content != expected || g.stableProof != j.r.StableProof {
		return j.failure("generation-drift", ErrNodeRecoveryRequired)
	}
	runtime, err := j.t.Activator.RuntimeIdentity(ctx)
	if err != nil || (runtime != "stopped" && !recoveryHex(runtime)) {
		return j.failure("post-runtime", ErrNodeRecoveryRequired)
	}
	j.r.Branch, j.r.GenerationProof, j.r.RuntimeBefore, j.r.RuntimeAfter = branch, g.proof, runtime, ""
	j.r.Reason = ""
	if branch == "metadata" {
		return j.write("committing", "commit")
	}
	return j.write("activation-intent", "restart")
}

func (j *transactionJournal) finish(ctx context.Context) error {
	if j == nil {
		return nil
	}
	g, err := captureNodeGeneration(j.t, nil)
	if err != nil || g.proof != j.r.GenerationProof {
		return j.failure("generation-drift", ErrNodeRecoveryRequired)
	}
	runtime, err := j.t.Activator.RuntimeIdentity(ctx)
	valid := runtime != "stopped" && runtime != j.r.RuntimeBefore && runtime == j.r.RuntimeAfter
	if j.r.Branch == "metadata" {
		valid = runtime == j.r.RuntimeBefore
	}
	if err != nil || !valid {
		return j.failure("post-runtime", ErrNodeRecoveryRequired)
	}
	j.r.RuntimeAfter = runtime
	if err = j.write("verified", "settlement"); err != nil {
		return err
	}
	m := &Manager{store: j.t.Store, tx: j.t}
	r := activatorRuntime{j.t.Activator}
	s, err := m.recoverySnapshot(ctx, r)
	if err != nil || !s.view.CanVerify || s.receiptIdentity != j.identity {
		return ErrNodeRecoveryRequired
	}
	if _, err = beginRecoveryCompletion(j.t.PreviousDir, j.r.Digest); err != nil {
		return ErrNodeRecoveryRequired
	}
	if settleNodeIntent(filepath.Join(j.t.PreviousDir, ".pending"), s.marker) != nil {
		return ErrNodeRecoveryRequired
	}
	return m.completeRecovery(ctx, r, &j.r, writeRecoveryReceipt)
}

func (t Transaction) commitTracked(ctx context.Context, registry Registry, rendered []byte, runtimeChanged bool, previous Registry, previousExists bool, old []byte, oldExists bool, intent *nodeIntent) error {
	if ctx.Err() != nil {
		return errors.Join(ErrNodeRecoveryRequired, ctx.Err())
	}
	j, err := t.newJournal(ctx, registry, rendered, runtimeChanged, intent)
	if err != nil {
		return errors.Join(ErrNodeRecoveryRequired, err)
	}
	if j != nil {
		g, e := captureNodeGeneration(t, nil)
		if e != nil || g.proof != j.r.BeforeProof {
			return j.failure("generation-drift", ErrNodeRecoveryRequired)
		}
		if err = j.write("committing", "commit"); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		if j != nil {
			return j.failure("commit", ctx.Err())
		}
		return errors.Join(ErrNodeRecoveryRequired, ctx.Err())
	}
	err = t.Store.Save(registry)
	if err == nil && runtimeChanged {
		err = atomicWrite(t.ActiveOutboundsPath, rendered, 0600)
	}
	stage := "commit"
	if err == nil && j != nil {
		branch := j.r.Branch
		if err = j.bindBranch(ctx, branch, j.r.CandidateContent); err != nil {
			return err
		}
	}
	if err == nil && runtimeChanged && t.Activator != nil {
		activation, cancel := context.WithTimeout(ctx, t.Budget.normalized().Activation)
		stage, err = t.activateTracked(activation, registry, j)
		cancel()
	}
	if err == nil {
		if j != nil {
			return j.finish(ctx)
		}
		return intent.Settle()
	}
	if errors.Is(err, xkeen.ErrLifecycleUnknown) {
		if j != nil {
			return j.failure(stage, err)
		}
		return errors.Join(ErrNodeRecoveryRequired, err)
	}
	if j != nil {
		j.r.ActivationFailure = stage + ":failed"
		j.r.ActivationElapsedMS = time.Since(j.stageStarted).Milliseconds()
		if errors.Is(err, context.DeadlineExceeded) {
			j.r.ActivationFailure = stage + ":deadline"
		}
		j.r.GenerationProof = "" // previous files have not been proven yet
		j.r.Branch = "previous"
		if e := j.write("restoring", "restore"); e != nil {
			return e
		}
	}
	rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), t.Budget.normalized().Rollback)
	defer cancel()
	// Keep the original total deadline even if caller cancellation triggered recovery.
	if deadline, ok := ctx.Deadline(); ok {
		var stop context.CancelFunc
		rollback, stop = context.WithDeadline(rollback, deadline)
		defer stop()
	}
	restoreErr := error(nil)
	if runtimeChanged {
		restoreErr = t.restore(previous, previousExists, old, oldExists)
	} else if previousExists {
		restoreErr = t.Store.Save(previous)
	} else {
		restoreErr = os.Remove(t.Store.Path)
		if errors.Is(restoreErr, os.ErrNotExist) {
			restoreErr = nil
		}
	}
	if e := restoreErr; e != nil {
		if j != nil {
			return errors.Join(&RollbackError{Cause: err, Recovery: e}, j.failure("restore", e))
		}
		return &RollbackError{Cause: err, Recovery: e}
	}
	if e := t.verifyRestored(previous, previousExists, old, oldExists); e != nil {
		if j != nil {
			return errors.Join(&RollbackError{Cause: err, Recovery: e}, j.failure("restore", e))
		}
		return &RollbackError{Cause: err, Recovery: e}
	}
	if j != nil {
		branch := "previous"
		if !runtimeChanged {
			branch = "metadata"
		}
		if e := j.bindBranch(rollback, branch, j.r.PreviousContent); e != nil {
			return e
		}
	}
	if runtimeChanged && t.Activator != nil {
		failedStage, e := t.activateTracked(rollback, previous, j)
		if e != nil {
			if j != nil {
				return errors.Join(&RollbackError{Cause: err, Recovery: e}, j.failure(failedStage, e))
			}
			return &RollbackError{Cause: err, Recovery: e}
		}
	}
	if j != nil {
		if e := j.finish(rollback); e != nil {
			return e
		}
	} else if e := intent.Settle(); e != nil {
		return e
	}
	return errors.New("node activation failed; previous generation restored")
}

func (t Transaction) activateTracked(ctx context.Context, r Registry, j *transactionJournal) (string, error) {
	for _, step := range []struct {
		stage string
		run   func() error
	}{
		{"restart", func() error { return t.Activator.Restart(ctx) }},
		{"readiness", func() error { return t.Activator.WaitReady(ctx) }},
		{"inventory", func() error { return t.Activator.VerifyOutboundTags(ctx, enabledTags(r)) }},
	} {
		if j != nil {
			if err := j.write("activation-intent", step.stage); err != nil {
				return step.stage, errors.Join(xkeen.ErrLifecycleUnknown, err)
			}
		}
		if j != nil && step.stage == "restart" {
			g, e := captureNodeGeneration(t, nil)
			r, re := t.Activator.RuntimeIdentity(ctx)
			if e != nil || re != nil || g.proof != j.r.GenerationProof || r != j.r.RuntimeBefore {
				return "generation-drift", errors.Join(xkeen.ErrLifecycleUnknown, ErrNodeRecoveryRequired)
			}
		}
		if err := step.run(); err != nil {
			return step.stage, err
		}
		if j != nil && step.stage == "restart" {
			current, e := t.Activator.RuntimeIdentity(ctx)
			if e != nil || current == "stopped" || current == j.r.RuntimeBefore || !recoveryHex(current) {
				return "post-runtime", errors.Join(xkeen.ErrLifecycleUnknown, ErrNodeRecoveryRequired)
			}
			j.r.RuntimeAfter = current
		}

	}
	return "inventory", nil
}
