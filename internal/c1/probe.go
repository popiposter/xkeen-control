package c1

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/popiposter/xkeen-keenetic/internal/xrayapi"
)

const (
	ProbeInboundTag  = "probe"
	ProbeAddress     = "127.0.0.1:10808"
	LivenessRuleTag  = "xkeen-control-probe-liveness"
	BenchmarkRuleTag = "xkeen-control-probe-benchmark"
)

var ErrProbeCleanup = errors.New("temporary probe routing cleanup failed")
var ErrProbeBlocked = errors.New("temporary probe routing is awaiting cleanup")

type ProbeRouter struct {
	api       xrayapi.RoutingController
	lease     chan struct{}
	mu        sync.Mutex
	blocked   bool
	cleanupAt time.Time
}

func NewProbeRouter(api xrayapi.RoutingController) *ProbeRouter {
	return &ProbeRouter{api: api, lease: make(chan struct{}, 1)}
}

func (p *ProbeRouter) WithTarget(ctx context.Context, kind, target string, action func(context.Context) error) (err error) {
	if p == nil || p.api == nil {
		return errors.New("probe routing unavailable")
	}
	select {
	case p.lease <- struct{}{}:
		defer func() { <-p.lease }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := p.reconcileLocked(ctx); err != nil {
		return err
	}
	ruleTag := ruleTagFor(kind)
	if err := p.removeIfPresent(ctx, ruleTag); err != nil {
		return errors.New("probe rule preparation failed")
	}
	if err := p.api.AddRule(ctx, xrayapi.Rule{RuleTag: ruleTag, InboundTag: ProbeInboundTag, OutboundTag: target}, true); err != nil {
		return errors.New("probe rule installation failed")
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cleanupErr := p.removeIfPresent(cleanupContext, ruleTag)
		cancel()
		if cleanupErr != nil {
			p.mu.Lock()
			p.blocked = true
			p.cleanupAt = time.Now().UTC()
			p.mu.Unlock()
			if err == nil {
				err = ErrProbeCleanup
			}
			return
		}
		p.mu.Lock()
		p.blocked = false
		p.mu.Unlock()
	}()
	if action == nil {
		return nil
	}
	return action(ctx)
}

func (p *ProbeRouter) Reconcile(ctx context.Context) error {
	if p == nil || p.api == nil {
		return errors.New("probe routing unavailable")
	}
	select {
	case p.lease <- struct{}{}:
		defer func() { <-p.lease }()
	case <-ctx.Done():
		return ctx.Err()
	}
	rules, err := p.api.ListRules(ctx)
	if err != nil {
		p.mu.Lock()
		p.blocked = true
		p.cleanupAt = time.Now().UTC()
		p.mu.Unlock()
		return ErrProbeBlocked
	}
	for _, rule := range rules {
		if rule.RuleTag != LivenessRuleTag && rule.RuleTag != BenchmarkRuleTag {
			continue
		}
		if err := p.api.RemoveRule(ctx, rule.RuleTag); err != nil {
			p.mu.Lock()
			p.blocked = true
			p.cleanupAt = time.Now().UTC()
			p.mu.Unlock()
			return ErrProbeBlocked
		}
	}
	p.mu.Lock()
	p.blocked = false
	p.mu.Unlock()
	return nil
}

func (p *ProbeRouter) reconcileLocked(ctx context.Context) error {
	p.mu.Lock()
	blocked := p.blocked
	p.mu.Unlock()
	if blocked {
		// Removal is idempotent on the Xray API. Only clear the degraded gate
		// after both known C.1 rule tags have been explicitly removed.
		for _, tag := range []string{LivenessRuleTag, BenchmarkRuleTag} {
			if err := p.removeIfPresent(ctx, tag); err != nil {
				return ErrProbeBlocked
			}
		}
		p.mu.Lock()
		p.blocked = false
		p.mu.Unlock()
	}
	return nil
}

func (p *ProbeRouter) removeIfPresent(ctx context.Context, tag string) error {
	rules, err := p.api.ListRules(ctx)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.RuleTag == tag {
			return p.api.RemoveRule(ctx, tag)
		}
	}
	return nil
}

func (p *ProbeRouter) Blocked() bool {
	if p == nil {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.blocked
}

func ruleTagFor(kind string) string {
	if kind == "liveness" {
		return LivenessRuleTag
	}
	if kind == "benchmark" {
		return BenchmarkRuleTag
	}
	return fmt.Sprintf("xkeen-control-probe-%s", kind)
}
