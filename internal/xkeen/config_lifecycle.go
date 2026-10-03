package xkeen

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// RestoreSaved discards a never-applied set without a restart. After an Apply
// attempt it instead stages the pre-apply files and requires a new explicit
// Restart. Neither path starts a command or overwrites external drift.
func (e *ConfigEditor) RestoreSaved(ctx context.Context, baseline string) (string, error) {
	release, err := e.Lease.Acquire(ctx, time.Second)
	if err != nil {
		return "", err
	}
	defer release()
	before, err := e.Snapshot(ctx)
	pending, pendingErr := e.readPending()
	if err != nil || pendingErr != nil || pending == nil || before.Digest != baseline || pending.Expected != baseline || pending.ApplyState == "running" {
		return "", ErrConfig
	}
	after, err := candidateSet(before, pending.Original)
	if err != nil {
		return "", err
	}
	if err := e.validateSet(ctx, after); err != nil {
		return "", err
	}
	needsRestart := pending.ApplyID != "" || pending.RestartRequired
	pending.Expected = after.Digest
	pending.ApplyID, pending.BeforeProcess = "", ""
	pending.ApplyState = "restored"
	pending.RestartRequired = needsRestart
	if e.writeGeneration("pending.json", pending) != nil {
		return "", ErrConfig
	}
	digest, err := e.promoteSet(ctx, before, after)
	if err != nil {
		return "", err
	}
	if !needsRestart {
		if e.removePending() != nil {
			return "", ErrConfig
		}
	}
	return digest, nil
}

func (e *ConfigEditor) RestorePrevious(ctx context.Context, baseline string) (string, error) {
	release, err := e.Lease.Acquire(ctx, time.Second)
	if err != nil {
		return "", err
	}
	defer release()
	before, err := e.Snapshot(ctx)
	pending, pendingErr := e.readPending()
	previous, previousErr := e.readGeneration("previous.json")
	if err != nil || pendingErr != nil || previousErr != nil || pending != nil || previous == nil || before.Digest != baseline || previous.Expected != baseline {
		return "", ErrConfig
	}
	return e.saveCandidate(ctx, before, previous.Original)
}

func (e *ConfigEditor) removePending() error {
	if err := os.Remove(filepath.Join(e.PreviousDir, "pending.json")); err != nil {
		return ErrConfig
	}
	return syncConfigDirectory(e.PreviousDir)
}

// Called with the job's ordinary panel lease held, immediately before native
// exec. This associates one explicit Apply with its exact saved configuration.
func (e *ConfigEditor) beginApply(ctx context.Context, baseline, id string) error {
	pending, err := e.readPending()
	before, snapshotErr := e.Snapshot(ctx)
	if err != nil || snapshotErr != nil || pending == nil || before.Digest != baseline || pending.Expected != baseline || pending.ApplyState == "running" || pending.ApplyState == "unknown" {
		return ErrConfig
	}
	process, err := e.configProcess(ctx)
	if err != nil {
		return err
	}
	if err := e.validateSet(ctx, before); err != nil {
		return err
	}
	current, snapshotErr := e.Snapshot(ctx)
	currentProcess, processErr := e.configProcess(ctx)
	if snapshotErr != nil || processErr != nil || current.Digest != baseline || currentProcess != process {
		return ErrConfig
	}
	pending.ApplyID, pending.BeforeProcess, pending.ApplyState = id, process, "running"
	pending.RestartRequired = true
	return e.writeGeneration("pending.json", pending)
}

// Process exit does not prove a config was loaded. Only an independently seen
// new Xray process, exact executable/confdir and unchanged saved digest can clear
// the pending set. Tunnel/DNS quality is a separate observation, never inferred.
func (e *ConfigEditor) finishApply(ctx context.Context, id, jobState string) string {
	pending, err := e.readPending()
	if err != nil || pending == nil || pending.ApplyID != id {
		return "unknown"
	}
	if jobState != "completed" {
		pending.ApplyState = jobState
		if e.writeGeneration("pending.json", pending) != nil {
			return "unknown"
		}
		return jobState
	}
	if e.verifyApplied(ctx, pending) == nil {
		return "applied"
	}
	pending.ApplyState = "unknown"
	_ = e.writeGeneration("pending.json", pending)
	return "unknown"
}

// InspectApplied rechecks a completed/interrupted result without replaying
// Restart. The shared recovery lease allows this readback while jobs are fenced.
func (e *ConfigEditor) InspectApplied(ctx context.Context, id string) error {
	release, err := e.Lease.AcquireForRecovery(ctx, time.Second)
	if err != nil {
		return err
	}
	defer release()
	pending, err := e.readPending()
	if err != nil || pending == nil || pending.ApplyID != id {
		return ErrConfig
	}
	if pending.ApplyState == "running" {
		pending.ApplyState = "unknown"
		if e.writeGeneration("pending.json", pending) != nil {
			return ErrConfig
		}
	}
	return e.verifyApplied(ctx, pending)
}

func (e *ConfigEditor) verifyApplied(ctx context.Context, pending *pendingConfig) error {
	saved, err := e.Snapshot(ctx)
	if err != nil || saved.Digest != pending.Expected {
		return ErrConfig
	}
	process, err := e.configProcess(ctx)
	if err != nil || process == "" || process == pending.BeforeProcess {
		return ErrConfig
	}
	// Recheck both independently observed facts before confirming the generation.
	again, err := e.Snapshot(ctx)
	processAgain, processErr := e.configProcess(ctx)
	if err != nil || processErr != nil || again.Digest != pending.Expected || processAgain != process {
		return ErrConfig
	}
	changed := false
	for name, original := range pending.Original {
		if string(saved.files[name]) != string(original) {
			changed = true
		}
	}
	if changed {
		previous := &pendingConfig{Expected: pending.Expected, Original: pending.Original}
		if e.writeGeneration("previous.json", previous) != nil {
			return ErrConfig
		}
	}
	return e.removePending()
}
