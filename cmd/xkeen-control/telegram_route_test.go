package main

import (
	"context"
	"errors"
	"testing"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

type routeControl struct {
	rules   []xrayapi.Rule
	added   []xrayapi.Rule
	listErr error
	addErr  error
}

func (c *routeControl) OverrideBalancerTarget(context.Context, string, string) error { return nil }
func (c *routeControl) RemoveRule(context.Context, string) error                     { return nil }
func (c *routeControl) ListRules(context.Context) ([]xrayapi.Rule, error) {
	return c.rules, c.listErr
}
func (c *routeControl) AddRule(_ context.Context, rule xrayapi.Rule, _ bool) error {
	if c.addErr != nil {
		return c.addErr
	}
	c.added = append(c.added, rule)
	c.rules = append(c.rules, xrayapi.Rule{RuleTag: rule.RuleTag})
	return nil
}

func TestTelegramRouteInstallsTheRuleOnceAndFallsBackDirect(t *testing.T) {
	control := &routeControl{}
	route := telegramRoute(control)
	if !route(context.Background()) || !route(context.Background()) || len(control.added) != 1 {
		t.Fatal("rule not installed exactly once", control.added)
	}
	if got := control.added[0]; got.RuleTag != xrayapi.NotifyRuleTag || got.BalancerTag != "bal-proxy" || len(got.Domains) != 1 || got.Domains[0] != "api.telegram.org" {
		t.Fatal("unexpected rule", got)
	}
	// Xray restarted: the runtime rule is gone and is added again.
	control.rules = nil
	if !route(context.Background()) || len(control.added) != 2 {
		t.Fatal("rule not restored after an Xray restart")
	}
	if telegramRoute(&routeControl{listErr: errors.New("api down")})(context.Background()) {
		t.Fatal("unreachable Xray routed through the proxy")
	}
	if telegramRoute(&routeControl{addErr: errors.New("no balancer")})(context.Background()) {
		t.Fatal("refused rule routed through the proxy")
	}
	if telegramRoute(nil)(context.Background()) {
		t.Fatal("nil control routed through the proxy")
	}
}
