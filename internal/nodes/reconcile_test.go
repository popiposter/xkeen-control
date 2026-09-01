package nodes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type reconcileActivator struct {
	validated int
	restarted int
	ready     int
	verified  int
}

func (a *reconcileActivator) ValidateCandidate(_ context.Context, dir string) error {
	contents, err := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if err != nil || len(contents) == 0 {
		return errors.New("candidate missing")
	}
	a.validated++
	return nil
}

func (a *reconcileActivator) Restart(context.Context) error {
	a.restarted++
	return nil
}

func (a *reconcileActivator) WaitReady(context.Context) error {
	a.ready++
	return nil
}

func (a *reconcileActivator) VerifyOutboundTags(_ context.Context, expected []string) error {
	if len(expected) == 0 {
		return errors.New("expected tags missing")
	}
	a.verified++
	return nil
}

func TestReconcileRuntimeRequiresCoherentPersistentState(t *testing.T) {
	manager, _, active := testManager(t, nil, nil)
	preview, err := manager.PreviewImport("csrf", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}

	activator := &reconcileActivator{}
	manager.tx.ConfigDir = filepath.Dir(active)
	manager.tx.Activator = activator
	if err := manager.ReconcileRuntime(context.Background()); err != nil {
		t.Fatalf("reconcile coherent state: %v", err)
	}
	if activator.validated != 1 || activator.restarted != 1 || activator.ready != 1 || activator.verified != 1 {
		t.Fatalf("reconcile calls = validate:%d restart:%d ready:%d verify:%d", activator.validated, activator.restarted, activator.ready, activator.verified)
	}

	if err := os.WriteFile(active, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.ReconcileRuntime(context.Background()); err == nil {
		t.Fatal("reconcile accepted active outbounds that do not render from the registry")
	}
	if activator.restarted != 1 {
		t.Fatalf("incoherent state reached runtime activation: restarts=%d", activator.restarted)
	}
}
