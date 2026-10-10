package nativequality

import (
	"reflect"
	"testing"

	"github.com/popiposter/xkeen-control/internal/c1"
	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func orphanFixture(selector string, costs string) (string, []c1.NodeState, []xkeen.ConfigTarget) {
	routing := `{"routing":{"balancers":[{"tag":"bal-proxy","selector":` + selector + `,"strategy":{"type":"leastLoad","settings":{"costs":` + costs + `}}}]}}`
	var nodes []c1.NodeState
	var targets []xkeen.ConfigTarget
	for _, tag := range []string{"proxy-node-a", "proxy-node-b", "proxy-node-c"} {
		nodes = append(nodes, c1.NodeState{Tag: tag, Enabled: true})
		targets = append(targets, xkeen.ConfigTarget{Kind: "outbound", Tag: tag})
	}
	return routing, nodes, targets
}

const threeCosts = `[{"regexp":true,"match":"^proxy-node-a$","value":1},{"regexp":true,"match":"^proxy-node-b$","value":2},{"regexp":true,"match":"^proxy-node-gone$","value":3}]`

func TestResolveActivePoolReportsExactWeightedOrphans(t *testing.T) {
	routing, nodes, targets := orphanFixture(`["proxy-node-a","proxy-node-b","proxy-node-gone"]`, threeCosts)
	pool, orphans, _, err := resolveActivePool(routing, nodes, targets, true)
	if err != nil || !reflect.DeepEqual(pool, []string{"proxy-node-a", "proxy-node-b"}) || !reflect.DeepEqual(orphans, []string{"proxy-node-gone"}) {
		t.Fatalf("orphan resolution = %v %v %v", pool, orphans, err)
	}
	if _, _, err := routingPool(routing, nodes, targets); err == nil {
		t.Fatal("strict routingPool accepted an orphaned selector")
	}
	// A disabled member whose outbound is still present is an orphan too.
	routing, nodes, targets = orphanFixture(`["proxy-node-a","proxy-node-b"]`, threeCosts)
	nodes[1].Enabled = false
	pool, orphans, _, err = resolveActivePool(routing, nodes, targets, true)
	if err != nil || !reflect.DeepEqual(pool, []string{"proxy-node-a"}) || !reflect.DeepEqual(orphans, []string{"proxy-node-b"}) {
		t.Fatalf("disabled member resolution = %v %v %v", pool, orphans, err)
	}
}

func TestResolveActivePoolStillRefusesUnweightedOrBroadMismatches(t *testing.T) {
	for name, routing := range map[string]string{
		"unweighted exact selector": func() string { r, _, _ := orphanFixture(`["proxy-node-a","proxy-node-gone"]`, `[{"regexp":true,"match":"^proxy-node-a$","value":1}]`); return r }(),
		"unresolved broad prefix":   func() string { r, _, _ := orphanFixture(`["proxy-node-a","proxy-other-"]`, threeCosts); return r }(),
	} {
		_, nodes, targets := orphanFixture(`[]`, `[]`)
		if _, _, _, err := resolveActivePool(routing, nodes, targets, true); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
