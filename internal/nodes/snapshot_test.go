package nodes

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type snapshotLifecycleActivator struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
	fail    bool
}

func (a *snapshotLifecycleActivator) ValidateCandidate(context.Context, string) error { return nil }

func (a *snapshotLifecycleActivator) Restart(context.Context) error {
	a.mu.Lock()
	a.calls++
	call := a.calls
	fail := a.fail
	a.mu.Unlock()
	if call == 1 {
		close(a.started)
		select {
		case <-a.release:
		case <-time.After(5 * time.Second):
			return errors.New("snapshot fixture timed out")
		}
		if fail {
			return errors.New("snapshot fixture activation failed")
		}
	}
	return nil
}

func (*snapshotLifecycleActivator) WaitReady(context.Context) error { return nil }

func (*snapshotLifecycleActivator) VerifyOutboundTags(context.Context, []string) error { return nil }

func TestSnapshotWaitsForApplyAndReturnsFinallyCommittedGeneration(t *testing.T) {
	for _, test := range []struct {
		name      string
		fail      bool
		expectErr bool
	}{
		{name: "commit", fail: false},
		{name: "rollback", fail: true, expectErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldRegistry := NewRegistry()
			oldNode := testNode(t, syntheticProfile, "node-11111111", true)
			oldRegistry.Nodes = []Node{oldNode}
			manager, store, _ := testManager(t, &oldRegistry, nil)
			manager.gateTimeout = time.Second
			activator := &snapshotLifecycleActivator{started: make(chan struct{}), release: make(chan struct{}), fail: test.fail}
			manager.tx.Activator = activator
			preview, err := manager.PreviewImport("snapshot-session", syntheticProfileTwo)
			if err != nil {
				t.Fatal(err)
			}
			applyResult := make(chan error, 1)
			go func() {
				_, applyErr := manager.Apply(context.Background(), "snapshot-session", preview.Token, false)
				applyResult <- applyErr
			}()
			select {
			case <-activator.started:
			case <-time.After(5 * time.Second):
				t.Fatal("apply did not reach activation")
			}

			snapshotResult := make(chan Registry, 1)
			snapshotErr := make(chan error, 1)
			go func() {
				value, snapshotErrValue := manager.Snapshot(context.Background())
				snapshotResult <- value
				snapshotErr <- snapshotErrValue
			}()
			select {
			case <-snapshotResult:
				t.Fatal("snapshot returned before apply was committed or rolled back")
			case <-time.After(50 * time.Millisecond):
			}
			close(activator.release)

			applyErr := <-applyResult
			if test.expectErr {
				if applyErr == nil {
					t.Fatal("failed activation unexpectedly committed")
				}
			} else if applyErr != nil {
				t.Fatal(applyErr)
			}
			got := <-snapshotResult
			if err := <-snapshotErr; err != nil {
				t.Fatal(err)
			}
			if test.expectErr {
				if !sameRegistry(got, oldRegistry) {
					t.Fatal("rollback snapshot exposed the transient registry")
				}
			} else if sameRegistry(got, oldRegistry) {
				t.Fatal("commit snapshot returned the pre-apply registry")
			}
			got.Nodes[0].Name = "mutated snapshot copy"
			stored, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if stored.Nodes[0].Name == "mutated snapshot copy" {
				t.Fatal("snapshot returned storage-backed mutable state")
			}
		})
	}
}

func TestSnapshotDoesNotEnterRuntimeCoordinator(t *testing.T) {
	manager, _, _ := testManager(t, nil, nil)
	manager.coordinator = coordinatorRejectsSnapshot{}
	if _, err := manager.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type coordinatorRejectsSnapshot struct{}

func (coordinatorRejectsSnapshot) BeginApply(context.Context) (func(), error) {
	return nil, errors.New("coordinator must not be entered by snapshot")
}
