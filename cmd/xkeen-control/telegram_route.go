package main

import (
	"context"
	"sync"
	"time"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
)

// telegramRoute returns the notification route check: it makes sure Xray
// holds the panel's Telegram rule (re-adding it after an Xray restart wiped
// runtime rules) and reports whether Bot API requests may use Xray's local
// proxy. Without the rule they go direct.
func telegramRoute(control xrayapi.RoutingController) func(context.Context) bool {
	var mu sync.Mutex
	return func(parent context.Context) bool {
		if control == nil {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		rules, err := control.ListRules(ctx)
		if err != nil {
			return false
		}
		for _, rule := range rules {
			if rule.RuleTag == xrayapi.NotifyRuleTag {
				return true
			}
		}
		return control.AddRule(ctx, xrayapi.NotifyRule(), true) == nil
	}
}
