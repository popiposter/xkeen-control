package nodes

import (
	"context"
	"runtime"
	"time"
)

// StageTimings is fixed, optional evidence. A present zero means the step ran;
// nil means it was not observed. It never contains configuration or RPC output.
type StageTimings struct {
	RestartMS         *int64 `json:"restartMs,omitempty"`
	ReadinessMS       *int64 `json:"readinessMs,omitempty"`
	InventoryMS       *int64 `json:"inventoryMs,omitempty"`
	ReadinessBudgetMS *int64 `json:"readinessBudgetMs,omitempty"`
}

func nodeReadinessAllowance(remaining, override time.Duration, mips bool) time.Duration {
	remaining = max(remaining, 0)
	allowance := durationMin(30*time.Second, remaining)
	if mips {
		allowance = max(allowance, durationMin(70*time.Second, max(0, remaining-50*time.Second)))
	}
	if override > 0 {
		allowance = durationMin(allowance, override)
	}
	return allowance
}

// Only node activation coordinators call this helper. Generic WaitReady and
// verify-existing keep their historical timeout/override semantics.
func activationReadiness(ctx context.Context, a Activator) (time.Duration, func() error) {
	remaining := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining = time.Until(deadline)
	}
	var command *CommandActivator
	switch v := a.(type) {
	case CommandActivator:
		command = &v
	case *CommandActivator:
		copy := *v
		command = &copy
	}
	override := time.Duration(0)
	if command != nil {
		override = command.ReadyTimeout
	}
	allowance := nodeReadinessAllowance(remaining, override, runtime.GOOS == "linux" && runtime.GOARCH == "mipsle")
	return allowance, func() error {
		phase, cancel := context.WithTimeout(ctx, allowance)
		defer cancel()
		if command != nil {
			command.ReadyTimeout = allowance
			return command.WaitReady(phase)
		}
		return a.WaitReady(phase)
	}
}

func recordStageTiming(target **StageTimings, stage string, elapsed, allowance time.Duration) {
	if *target == nil {
		*target = &StageTimings{}
	}
	ms := elapsed.Milliseconds()
	switch stage {
	case "restart":
		(*target).RestartMS = &ms
	case "readiness":
		(*target).ReadinessMS = &ms
		budget := allowance.Milliseconds()
		(*target).ReadinessBudgetMS = &budget
	case "inventory":
		(*target).InventoryMS = &ms
	}
}

func validStageTimings(t *StageTimings) bool {
	if t == nil {
		return true
	}
	if t.RestartMS == nil {
		return false
	}
	if (t.ReadinessMS == nil) != (t.ReadinessBudgetMS == nil) {
		return false
	}
	if t.InventoryMS != nil && t.ReadinessMS == nil {
		return false
	}
	for _, v := range []*int64{t.RestartMS, t.ReadinessMS, t.InventoryMS, t.ReadinessBudgetMS} {
		if v != nil && (*v < 0 || *v > 375000) {
			return false
		}
	}
	if t.ReadinessBudgetMS != nil && *t.ReadinessBudgetMS > 70000 {
		return false
	}
	return true
}

func durationMin(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
