package c1

import (
	"errors"
	"testing"
	"time"
)

func TestBenchmarkPlanScalesForCurrentRegistrySizes(t *testing.T) {
	policy := DefaultPolicy()
	for _, test := range []struct {
		nodes   int
		payload int64
	}{
		{97, 20 * MiB},
		{128, 16 * MiB},
		{256, 8 * MiB},
	} {
		plan, err := policy.Plan(test.nodes)
		if err != nil || plan.PayloadBytesPerNode != test.payload || plan.EligibleNodes != test.nodes || plan.MaximumWallTime != time.Duration(test.nodes)*DefaultPerNodeTimeout {
			t.Fatalf("plan(%d) = %+v, %v", test.nodes, plan, err)
		}
	}
}

func TestBenchmarkPlanFailsClosedBelowMinimumPayload(t *testing.T) {
	policy := DefaultPolicy()
	policy.TotalBudgetBytes = 3 * MiB
	plan, err := policy.Plan(1)
	if !errors.Is(err, ErrInsufficientBudget) || !plan.InsufficientBudget {
		t.Fatalf("insufficient plan = %+v, %v", plan, err)
	}
}

func TestEligibleTagsIncludesAllEnabledCanonicalNodes(t *testing.T) {
	nodes := []NodeState{{Tag: "proxy-b", Enabled: true}, {Tag: "proxy-a", Enabled: true}, {Tag: "proxy-a", Enabled: true}, {Tag: "proxy-disabled", Enabled: false}, {Tag: "direct", Enabled: true}}
	got := EligibleTags(nodes, 256)
	if len(got) != 2 || got[0] != "proxy-a" || got[1] != "proxy-b" {
		t.Fatalf("eligible tags = %v", got)
	}
}
