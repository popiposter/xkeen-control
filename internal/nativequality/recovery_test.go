package nativequality

import (
	"testing"
)

func TestSingleMemberRecoveryPoolResolves(t *testing.T) {
	routing, nodes, targets := orphanFixture(`["proxy-node-a"]`, `[{"regexp":true,"match":"^proxy-node-a$","value":1}]`)
	if pool, _, err := routingPool(routing, nodes, targets); err != nil || len(pool) != 1 {
		t.Fatal("a one-member provisional pool did not resolve", pool, err)
	}
}
