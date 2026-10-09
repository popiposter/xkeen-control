package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestNodeReadinessAllowance(t *testing.T) {
	for _, row := range []struct{ remaining, mips, standard int }{{120, 70, 30}, {100, 50, 30}, {80, 30, 30}, {50, 30, 30}, {20, 20, 20}, {0, 0, 0}, {-1, 0, 0}} {
		for _, mips := range []bool{false, true} {
			expected := row.standard
			if mips {
				expected = row.mips
			}
			if got := nodeReadinessAllowance(time.Duration(row.remaining)*time.Second, 0, mips); got != time.Duration(expected)*time.Second {
				t.Fatal(row, mips, got)
			}
			if got := nodeReadinessAllowance(time.Duration(row.remaining)*time.Second, 10*time.Second, mips); got != time.Duration(min(10, expected))*time.Second {
				t.Fatal(row, mips, got)
			}
		}
	}
	if nodeReadinessAllowance(100*time.Second, time.Hour, true) != 50*time.Second {
		t.Fatal("override enlarged allocation")
	}
	if nodeReadinessAllowance(120*time.Second, 0, true)+50*time.Second > 120*time.Second {
		t.Fatal("reserve starved")
	}
	if DefaultActivationTimeout != 120*time.Second || DefaultRollbackTimeout != 120*time.Second {
		t.Fatal("phase changed")
	}
}
func TestNodeCoordinatorUsesTargetAndShortOverride(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	allowance, _ := activationReadiness(ctx, CommandActivator{})
	expected := 30 * time.Second
	if runtime.GOOS == "linux" && runtime.GOARCH == "mipsle" {
		expected = 70 * time.Second
	}
	if allowance > expected || allowance < expected-time.Second {
		t.Fatal(allowance, expected)
	}
	allowance, _ = activationReadiness(ctx, &CommandActivator{ReadyTimeout: time.Millisecond})
	if allowance != time.Millisecond {
		t.Fatal(allowance)
	}
	short, end := context.WithTimeout(ctx, time.Millisecond)
	defer end()
	allowance, _ = activationReadiness(short, CommandActivator{})
	if allowance < 0 || allowance > time.Millisecond {
		t.Fatal(allowance)
	}
}
func TestNodeExtendedReadinessScaledSuccess(t *testing.T) {
	s := &readyServer{call: func(ctx context.Context) error {
		select {
		case <-time.After(400 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	address := startReadyServer(t, s)
	// Scale only the coordinator's computed time for a deterministic local RPC.
	allowance := nodeReadinessAllowance(120*time.Second, 0, true) / 100
	ctx, cancel := context.WithTimeout(context.Background(), allowance)
	defer cancel()
	if err := waitRoutingReady(ctx, address, time.Second, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if allowance <= 300*time.Millisecond || s.calls.Load() != 1 {
		t.Fatal("no extension", allowance)
	}
	assertReadyClosed(t, s)
}
func TestNodeShortBudgetNeverReady(t *testing.T) {
	s := &readyServer{call: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	allowance, ready := activationReadiness(ctx, CommandActivator{APIAddress: startReadyServer(t, s)})
	if allowance <= 0 || allowance > 80*time.Millisecond {
		t.Fatal(allowance)
	}
	if err := ready(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestStageTimingPresenceAndBounds(t *testing.T) {
	var timing *StageTimings
	recordStageTiming(&timing, "restart", 0, 0)
	raw, _ := json.Marshal(timing)
	if string(raw) != `{"restartMs":0}` || !validStageTimings(timing) {
		t.Fatal(string(raw))
	}
	zero := int64(0)
	negative := int64(-1)
	oversized := int64(375001)
	budget := int64(70001)
	for _, bad := range []*StageTimings{{}, {RestartMS: &negative}, {RestartMS: &oversized}, {RestartMS: &zero, ReadinessMS: &zero}, {RestartMS: &zero, InventoryMS: &zero}, {RestartMS: &zero, ReadinessMS: &zero, ReadinessBudgetMS: &budget}} {
		if validStageTimings(bad) {
			t.Fatal("malformed timing accepted", bad)
		}
	}
}
