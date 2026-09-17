package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const batchSubscriptionID = "sub-12345678"

func batchRegistry(t *testing.T) Registry {
	t.Helper()
	subscriptionProfile, err := ParseProfile(syntheticProfile)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionNode, err := NewNodeWithID(subscriptionProfile.VLESS, "Provider node", Source{Type: "subscription", SubscriptionID: batchSubscriptionID}, "node-33333333")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.Subscriptions = []Subscription{{ID: batchSubscriptionID, Name: "Provider", URL: "https://subscription.example/token", Enabled: true}}
	registry.Nodes = []Node{
		testNode(t, syntheticProfile, "node-11111111", true),
		testNode(t, syntheticProfileTwo, "node-22222222", false),
		subscriptionNode,
	}
	return registry
}

func TestBatchPreviewRejectsInvalidSelectionWithoutCreatingCandidate(t *testing.T) {
	registry := batchRegistry(t)
	invalid := [][]string{
		{},
		{"bad-id"},
		{"node-11111111", "node-11111111"},
		{"node-99999999"},
	}
	tooMany := make([]string, MaxNodes+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("node-%08x", index)
	}
	invalid = append(invalid, tooMany)

	manager, store, _ := testManager(t, &registry, nil)
	for _, nodeIDs := range invalid {
		if _, err := manager.PreviewBatchState("csrf", nodeIDs, false); !errors.Is(err, ErrBatchInvalid) {
			t.Fatalf("invalid node IDs %v returned %v", nodeIDs, err)
		}
	}
	if len(manager.previews) != 0 {
		t.Fatalf("invalid selection created %d previews", len(manager.previews))
	}
	got, err := store.Load()
	if err != nil || !sameRegistry(got, registry) {
		t.Fatalf("invalid selection changed registry: %+v, %v", got, err)
	}

	registry.Subscriptions[0].Enabled = false
	manager, store, _ = testManager(t, &registry, nil)
	if _, err := manager.PreviewBatchState("csrf", []string{"node-11111111", "node-33333333"}, true); !errors.Is(err, ErrSubscriptionDisabled) {
		t.Fatalf("disabled subscription admission = %v", err)
	}
	got, err = store.Load()
	if err != nil || !sameRegistry(got, registry) {
		t.Fatalf("disabled subscription selection changed registry: %+v, %v", got, err)
	}
}

func TestBatchStateUsesOneDeterministicCandidateAndTransactionBoundary(t *testing.T) {
	registry := batchRegistry(t)
	manager, store, _ := testManager(t, &registry, nil)
	activator := &batchCountingActivator{}
	manager.tx.Activator = activator

	preview, err := manager.PreviewBatchState("csrf", []string{"node-33333333", "node-11111111"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Operation != "batch-disable" || len(preview.Changes) != 2 || preview.Noop {
		t.Fatalf("batch disable preview = %+v", preview)
	}
	result, err := manager.Apply(context.Background(), "csrf", preview.Token, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation != "batch-disable" || len(result.Changes) != 2 || activator.restarts != 1 || activator.validations != 1 {
		t.Fatalf("batch transaction boundary = result:%+v activator:%+v", result, activator)
	}
	updated, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range updated.Nodes {
		switch node.ID {
		case "node-11111111", "node-33333333":
			if node.Enabled {
				t.Fatalf("selected node remained enabled: %s", node.ID)
			}
		case "node-22222222":
			if node.Enabled {
				t.Fatalf("unselected node changed: %s", node.ID)
			}
		}
	}
	if !updated.Subscriptions[0].Enabled {
		t.Fatal("batch state changed the parent subscription")
	}

	preview, err = manager.PreviewBatchState("csrf", []string{"node-33333333", "node-11111111"}, true)
	if err != nil || preview.Operation != "batch-enable" || len(preview.Changes) != 2 {
		t.Fatalf("batch enable preview = %+v, %v", preview, err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
}

func TestBatchNoopDoesNotInvokeTransaction(t *testing.T) {
	registry := batchRegistry(t)
	registry.Nodes[1].Enabled = true
	manager, store, _ := testManager(t, &registry, nil)
	activator := &batchCountingActivator{}
	manager.tx.Activator = activator

	preview, err := manager.PreviewBatchState("csrf", []string{"node-22222222", "node-11111111"}, true)
	if err != nil || !preview.Noop || len(preview.Changes) != 0 {
		t.Fatalf("no-op batch preview = %+v, %v", preview, err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err != nil {
		t.Fatal(err)
	}
	if activator.validations != 0 || activator.restarts != 0 {
		t.Fatalf("no-op invoked transaction: %+v", activator)
	}
	updated, err := store.Load()
	if err != nil || !sameRegistry(updated, registry) {
		t.Fatalf("no-op changed registry: %+v, %v", updated, err)
	}
}

func TestBatchApplyRejectsStaleBaseWithoutTransaction(t *testing.T) {
	registry := batchRegistry(t)
	manager, store, _ := testManager(t, &registry, nil)
	activator := &batchCountingActivator{}
	manager.tx.Activator = activator

	preview, err := manager.PreviewBatchState("csrf", []string{"node-11111111"}, false)
	if err != nil {
		t.Fatal(err)
	}
	intervening := registry
	intervening.Nodes[1].Name = "Intervening node update"
	intervening.Subscriptions[0].Name = "Intervening subscription update"
	if err := store.Save(intervening); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("stale batch apply = %v", err)
	}
	if activator.validations != 0 || activator.restarts != 0 {
		t.Fatalf("stale batch apply invoked transaction: %+v", activator)
	}
	if len(manager.previews) != 0 {
		t.Fatalf("stale batch preview remained available: %d", len(manager.previews))
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); !errors.Is(err, ErrPreviewExpired) {
		t.Fatalf("stale batch token remained usable: %v", err)
	}
	updated, err := store.Load()
	if err != nil || !sameRegistry(updated, intervening) {
		t.Fatalf("stale batch apply changed committed registry: %+v, %v", updated, err)
	}
}

func TestBatchRemoveKeepsSubscriptionsAndIsOrderIndependent(t *testing.T) {
	registry := batchRegistry(t)
	left, leftStore, _ := testManager(t, &registry, nil)
	right, rightStore, _ := testManager(t, &registry, nil)
	leftPreview, err := left.PreviewBatchRemove("left", []string{"node-33333333", "node-11111111"})
	if err != nil {
		t.Fatal(err)
	}
	rightPreview, err := right.PreviewBatchRemove("right", []string{"node-11111111", "node-33333333"})
	if err != nil {
		t.Fatal(err)
	}
	if leftPreview.Operation != "batch-remove" || len(leftPreview.Changes) != 2 || !sameChanges(leftPreview.Changes, rightPreview.Changes) {
		t.Fatalf("order-dependent remove previews: left=%+v right=%+v", leftPreview, rightPreview)
	}
	if _, err := left.Apply(context.Background(), "left", leftPreview.Token, false); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Apply(context.Background(), "right", rightPreview.Token, false); err != nil {
		t.Fatal(err)
	}
	leftResult, err := leftStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	rightResult, err := rightStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	leftJSON, _ := json.Marshal(leftResult)
	rightJSON, _ := json.Marshal(rightResult)
	if string(leftJSON) != string(rightJSON) {
		t.Fatalf("order-dependent candidates: %s != %s", leftJSON, rightJSON)
	}
	if len(leftResult.Subscriptions) != 1 || len(leftResult.Nodes) != 1 || leftResult.Nodes[0].ID != "node-22222222" {
		t.Fatalf("batch remove changed unselected records: %+v", leftResult)
	}
}

func TestBatchActivationFailureUsesExistingCompleteRollback(t *testing.T) {
	registry := batchRegistry(t)
	manager, store, _ := testManager(t, &registry, nil)
	activator := &batchFailingActivator{}
	manager.tx.Activator = activator
	preview, err := manager.PreviewBatchState("csrf", []string{"node-11111111", "node-33333333"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), "csrf", preview.Token, false); err == nil || !strings.Contains(err.Error(), "previous generation restored") {
		t.Fatalf("batch activation error = %v", err)
	}
	restored, err := store.Load()
	if err != nil || !sameRegistry(restored, registry) {
		t.Fatalf("batch rollback registry = %+v, %v", restored, err)
	}
}

func sameChanges(left, right []Change) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

type batchCountingActivator struct {
	validations int
	restarts    int
}

func (a *batchCountingActivator) ValidateCandidate(context.Context, string) error {
	a.validations++
	return nil
}
func (a *batchCountingActivator) Restart(context.Context) error {
	a.restarts++
	return nil
}
func (*batchCountingActivator) WaitReady(context.Context) error { return nil }
func (*batchCountingActivator) VerifyOutboundTags(context.Context, []string) error {
	return nil
}

type batchFailingActivator struct {
	restarts int
}

func (*batchFailingActivator) ValidateCandidate(context.Context, string) error { return nil }
func (a *batchFailingActivator) Restart(context.Context) error {
	a.restarts++
	if a.restarts == 1 {
		return errors.New("synthetic restart failure")
	}
	return nil
}
func (*batchFailingActivator) WaitReady(context.Context) error { return nil }
func (*batchFailingActivator) VerifyOutboundTags(context.Context, []string) error {
	return nil
}
