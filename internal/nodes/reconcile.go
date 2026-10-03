package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/popiposter/xkeen-control/internal/configjson"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

// ReconcileRuntime proves that the authoritative registry and generated active
// outbound artifact describe the same generation, validates the complete Xray
// configuration, then reloads and verifies that exact generation in runtime.
// It does not rewrite node state or unrelated policy files.
func (m *Manager) ReconcileRuntime(ctx context.Context) (resultErr error) {
	if m == nil || m.tx.Activator == nil || m.tx.ActiveOutboundsPath == "" || m.tx.ConfigDir == "" {
		return errors.New("node runtime reconciliation unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	gateTimeout := m.gateTimeout
	if gateTimeout <= 0 {
		gateTimeout = DefaultApplyGateWaitTimeout
	}
	ctx, release, err := m.authority.AcquireContext(ctx, gateTimeout)
	if err != nil {
		return ErrNodeRecoveryRequired
	}
	defer func() {
		if errors.Is(resultErr, ErrNodeRecoveryRequired) {
			m.authority.Block()
		}
		if release() != nil {
			resultErr = errors.Join(resultErr, ErrNodeRecoveryRequired)
		}
	}()
	previousDir := m.tx.PreviousDir
	if previousDir == "" {
		previousDir = filepath.Join(filepath.Dir(m.tx.Store.Path), "previous")
	}
	if _, err := os.Lstat(filepath.Join(previousDir, ".pending")); !errors.Is(err, os.ErrNotExist) {
		return ErrNodeRecoveryRequired
	}
	registry, err := m.store.Load()
	if err != nil {
		return errors.New("node registry unavailable")
	}
	if err := registry.Validate(); err != nil {
		return errors.New("node registry invalid")
	}
	active, err := ReadBoundedFile(m.tx.ActiveOutboundsPath, MaxLegacyDocument)
	if err != nil {
		return errors.New("active outbound artifact unavailable")
	}
	rendered, err := RenderNative(active, registry, registry)
	if err != nil {
		return errors.New("node registry render failed")
	}
	object, err := configjson.DecodeObject(active)
	if err != nil {
		return errors.New("active outbound artifact invalid")
	}
	canonical, err := json.MarshalIndent(object, "", "  ")
	if err != nil || !bytes.Equal(bytes.TrimSpace(rendered), canonical) {
		return errors.New("active outbound artifact does not match node registry")
	}
	return m.tx.reconcileRuntime(ctx, registry, active)
}

func (t Transaction) reconcileRuntime(ctx context.Context, registry Registry, rendered []byte) (err error) {
	budget := t.Budget.normalized()
	deadline := time.Now().Add(budget.Total)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	reconcileContext, cancelReconcile := context.WithDeadline(ctx, deadline)
	defer cancelReconcile()

	candidateDir, err := os.MkdirTemp("", "xkeen-node-reconcile-")
	if err != nil {
		return errors.New("unable to prepare node runtime reconciliation")
	}
	defer os.RemoveAll(candidateDir)
	if err := copyTree(t.ConfigDir, candidateDir); err != nil {
		return errors.New("unable to prepare Xray reconciliation candidate")
	}
	if err := atomicWrite(filepath.Join(candidateDir, "04_outbounds.json"), rendered, 0o600); err != nil {
		return errors.New("unable to prepare Xray reconciliation candidate")
	}

	validationContext, cancelValidation := context.WithTimeout(reconcileContext, budget.CandidateValidation)
	validationErr := t.Activator.ValidateCandidate(validationContext, candidateDir)
	cancelValidation()
	if validationErr != nil {
		return errors.New("Xray reconciliation candidate validation failed")
	}

	if t.PreviousDir == "" {
		t.PreviousDir = filepath.Join(filepath.Dir(t.Store.Path), "previous")
	}
	intent, intentErr := acquireNodeIntent(ctx, t.PreviousDir)
	if intentErr != nil {
		return intentErr
	}
	defer intent.Close()
	settled := true
	defer func() {
		if settled {
			if releaseErr := intent.Settle(); releaseErr != nil {
				err = releaseErr
			}
		}
	}()
	current, loadErr := t.Store.Load()
	active, readErr := ReadBoundedFile(t.ActiveOutboundsPath, MaxLegacyDocument)
	if loadErr != nil || readErr != nil || !reflect.DeepEqual(current, registry) || !bytes.Equal(active, rendered) {
		return errors.New("node configuration changed during reconciliation")
	}
	if err := t.savePrevious(registry, true, rendered, true); err != nil {
		return err
	}
	if syncNodeDirectory(t.PreviousDir) != nil {
		return ErrNodeRecoveryRequired
	}
	settled = false

	activationContext, cancelActivation := context.WithTimeout(reconcileContext, budget.Activation)
	activationErr := t.activate(activationContext, registry)
	cancelActivation()
	if activationErr != nil {
		if errors.Is(activationErr, xkeen.ErrLifecycleUnknown) {
			return errors.Join(ErrNodeRecoveryRequired, activationErr)
		}
		return ErrNodeRecoveryRequired
	}
	settled = true
	return nil
}
