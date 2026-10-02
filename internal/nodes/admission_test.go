package nodes

import (
	"context"
	"errors"
	"testing"

	"github.com/popiposter/xkeen-control/internal/authority"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func TestApplyUnknownBlocksSharedAuthority(t *testing.T) {
	m, _, _ := testManager(t, nil, nil)
	m.tx.Activator = &fakeActivator{restartErr: xkeen.ErrLifecycleUnknown}
	p, err := m.PreviewImport("session", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(context.Background(), "session", p.Token, false); !errors.Is(err, ErrNodeRecoveryRequired) {
		t.Fatal(err)
	}
	if release, err := m.authority.TryAcquire(); !errors.Is(err, authority.ErrBlocked) {
		if release != nil {
			release()
		}
		t.Fatalf("unknown transaction released shared authority: %v", err)
	}
}

type admissionContextKey struct{}
type admissionRollbackActivator struct {
	fakeActivator
	contexts []context.Context
}

func (a *admissionRollbackActivator) Restart(ctx context.Context) error {
	a.contexts = append(a.contexts, ctx)
	return a.fakeActivator.Restart(ctx)
}

func TestRollbackPreservesOperationContext(t *testing.T) {
	m, _, _ := testManager(t, nil, nil)
	a := &admissionRollbackActivator{fakeActivator: fakeActivator{restartErrs: []error{errors.New("synthetic start failure"), nil}}}
	m.tx.Activator = a
	p, err := m.PreviewImport("session", syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), admissionContextKey{}, "operation")
	if _, err := m.Apply(ctx, "session", p.Token, false); err == nil {
		t.Fatal("expected rollback")
	}
	if len(a.contexts) != 2 {
		t.Fatalf("lifecycle calls=%d", len(a.contexts))
	}
	for _, got := range a.contexts {
		if got.Value(admissionContextKey{}) != "operation" {
			t.Fatal("rollback lost operation context")
		}
		if _, ok := got.Deadline(); !ok {
			t.Fatal("unbounded operation")
		}
	}
}
