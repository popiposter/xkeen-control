package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func testRuntimeIdentity(n int) string {
	d := sha256.Sum256([]byte(fmt.Sprint("runtime-", n)))
	return hex.EncodeToString(d[:])
}
func (f *fakeActivator) RuntimeIdentity(context.Context) (string, error) {
	return testRuntimeIdentity(f.restarts), nil
}
func (f *batchCountingActivator) RuntimeIdentity(context.Context) (string, error) {
	return testRuntimeIdentity(f.restarts), nil
}
func (f *batchFailingActivator) RuntimeIdentity(context.Context) (string, error) {
	return testRuntimeIdentity(f.restarts), nil
}
func (f *gateRollbackBudgetActivator) RuntimeIdentity(context.Context) (string, error) {
	return testRuntimeIdentity(f.restarts), nil
}
func (f *reconcileActivator) RuntimeIdentity(context.Context) (string, error) {
	return testRuntimeIdentity(f.restarted), nil
}
func (f *snapshotLifecycleActivator) RuntimeIdentity(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return testRuntimeIdentity(f.calls), nil
}
func (f *deadlineActivator) RuntimeIdentity(context.Context) (string, error) {
	return testRuntimeIdentity(0), nil
}

func seedNativeFixture(t *testing.T, m *Manager) {
	t.Helper()
	r, e := m.store.Load()
	if e != nil {
		t.Fatal(e)
	}
	b, e := Render(r)
	if e != nil {
		t.Fatal(e)
	}
	if e = atomicWrite(m.tx.ActiveOutboundsPath, b, 0600); e != nil {
		t.Fatal(e)
	}
}
