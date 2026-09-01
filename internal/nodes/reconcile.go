package nodes

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ReconcileRuntime proves that the authoritative registry and generated active
// outbound artifact describe the same generation, validates the complete Xray
// configuration, then reloads and verifies that exact generation in runtime.
// It does not rewrite node state or unrelated policy files.
func (m *Manager) ReconcileRuntime(ctx context.Context) error {
	if m == nil || m.tx.Activator == nil || m.tx.ActiveOutboundsPath == "" || m.tx.ConfigDir == "" {
		return errors.New("node runtime reconciliation unavailable")
	}
	registry, err := m.store.Load()
	if err != nil {
		return errors.New("node registry unavailable")
	}
	if err := registry.Validate(); err != nil {
		return errors.New("node registry invalid")
	}
	rendered, err := Render(registry)
	if err != nil {
		return errors.New("node registry render failed")
	}
	active, err := ReadBoundedFile(m.tx.ActiveOutboundsPath, MaxLegacyDocument)
	if err != nil || !bytes.Equal(rendered, active) {
		return errors.New("active outbound artifact does not match node registry")
	}
	return m.tx.reconcileRuntime(ctx, registry, rendered)
}

func (t Transaction) reconcileRuntime(ctx context.Context, registry Registry, rendered []byte) error {
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

	activationContext, cancelActivation := context.WithTimeout(reconcileContext, budget.Activation)
	activationErr := t.activate(activationContext, registry)
	cancelActivation()
	if activationErr != nil {
		return errors.New("Xray runtime reconciliation failed")
	}
	return nil
}
