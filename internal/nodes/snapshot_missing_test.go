package nodes

import (
	"context"
	"errors"
	"testing"
)

func TestSnapshotFailsClosedWhenAuthoritativeRegistryIsMissing(t *testing.T) {
	manager, _, _ := testManager(t, nil, nil)
	if _, err := manager.Snapshot(context.Background()); !errors.Is(err, ErrSnapshotUnavailable) {
		t.Fatalf("missing authoritative registry snapshot = %v", err)
	}
}
