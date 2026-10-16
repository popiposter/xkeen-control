package nativequality

import (
	"reflect"
	"testing"

	"github.com/popiposter/xkeen-control/internal/c1"
)

func TestSingleMemberRecoveryPoolResolves(t *testing.T) {
	routing, nodes, targets := orphanFixture(`["proxy-node-a"]`, `[{"regexp":true,"match":"^proxy-node-a$","value":1}]`)
	if pool, _, err := routingPool(routing, nodes, targets); err != nil || len(pool) != 1 {
		t.Fatal("a one-member provisional pool did not resolve", pool, err)
	}
}

// A pool without the unusable member keeps each remaining member's review
// cost; a member without one gets the neutral cost.
func TestMemberCostsKeepReviewCosts(t *testing.T) {
	routing := `{"routing":{"balancers":[{"tag":"bal-proxy","selector":["proxy-a","proxy-b","proxy-c"],"strategy":{"type":"leastLoad","settings":{"costs":[{"regexp":true,"match":"^proxy-a$","value":1},{"regexp":true,"match":"^proxy-b$","value":5.5}]}}}]}}`
	got := memberCosts(routing, 0, []string{"proxy-b", "proxy-c"})
	want := []c1.NativeQualityCost{{Regexp: true, Match: "^proxy-b$", Value: 5.5}, {Regexp: true, Match: "^proxy-c$", Value: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("member costs", got)
	}
}
