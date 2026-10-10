package nativequality

import (
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/c1"
)

func TestProvisionalPoolIsReplacedByTheFirstRankedReview(t *testing.T) {
	now := time.Now().UTC()
	result := c1.AdaptiveResult{}
	var costs []c1.NativeQualityCost
	active := []string{"proxy-a"}
	selected := []string{"proxy-b", "proxy-c"}
	plan := sweepPlan{Active: active}
	if got := poolDecision(result, costs, active, selected, plan, map[string]bool{"proxy-a": true}, "", now); got == "provisional-pool-replaced" {
		t.Fatal("an ordinary pool was treated as provisional")
	}
	plan.Provisional = true
	if got := poolDecision(result, costs, active, selected, plan, map[string]bool{"proxy-a": true}, "", now); got != "provisional-pool-replaced" {
		t.Fatal("provisional pool decision =", got)
	}
	// The same members still replace the equal recovery costs.
	same := []string{"proxy-b", "proxy-c"}
	if got := poolDecision(result, costs, same, same, plan, nil, "", now); got != "provisional-pool-replaced" {
		t.Fatal("a ranked review that keeps the provisional members =", got)
	}
}

func TestSingleMemberRecoveryPoolResolves(t *testing.T) {
	routing, nodes, targets := orphanFixture(`["proxy-node-a"]`, `[{"regexp":true,"match":"^proxy-node-a$","value":1}]`)
	if pool, _, err := routingPool(routing, nodes, targets); err != nil || len(pool) != 1 {
		t.Fatal("a one-member provisional pool did not resolve", pool, err)
	}
}
