package xrayapi

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

type Rule struct {
	RuleTag     string
	InboundTag  string
	OutboundTag string
}

type RoutingController interface {
	OverrideBalancerTarget(context.Context, string, string) error
	AddRule(context.Context, Rule, bool) error
	RemoveRule(context.Context, string) error
	ListRules(context.Context) ([]Rule, error)
}

func (c *Client) OverrideBalancerTarget(ctx context.Context, balancerTag, target string) error {
	if balancerTag == "" {
		balancerTag = "bal-proxy"
	}
	return c.withRouting(ctx, func(client RoutingServiceClient, callCtx context.Context) error {
		_, err := client.OverrideBalancerTarget(callCtx, &OverrideBalancerTargetRequest{BalancerTag: balancerTag, Target: target})
		return err
	})
}

func (c *Client) AddRule(ctx context.Context, rule Rule, shouldAppend bool) error {
	if !validRule(rule) {
		return errors.New("temporary routing rule is invalid")
	}
	routingRule := &RoutingRule{TargetTag: &RoutingRule_Tag{Tag: rule.OutboundTag}, RuleTag: rule.RuleTag, InboundTag: []string{rule.InboundTag}}
	encoded, err := proto.Marshal(&Config{Rule: []*RoutingRule{routingRule}})
	if err != nil {
		return errors.New("temporary routing rule encoding failed")
	}
	return c.withRouting(ctx, func(client RoutingServiceClient, callCtx context.Context) error {
		_, err := client.AddRule(callCtx, &AddRuleRequest{Config: &TypedMessage{Type: "xray.app.router.Config", Value: encoded}, ShouldAppend: shouldAppend})
		return err
	})
}

func (c *Client) RemoveRule(ctx context.Context, ruleTag string) error {
	if ruleTag == "" || len(ruleTag) > 128 {
		return errors.New("temporary routing rule tag is invalid")
	}
	return c.withRouting(ctx, func(client RoutingServiceClient, callCtx context.Context) error {
		_, err := client.RemoveRule(callCtx, &RemoveRuleRequest{RuleTag: ruleTag})
		return err
	})
}

func (c *Client) ListRules(ctx context.Context) ([]Rule, error) {
	var result []Rule
	err := c.withRouting(ctx, func(client RoutingServiceClient, callCtx context.Context) error {
		response, err := client.ListRule(callCtx, &ListRuleRequest{})
		if err != nil {
			return err
		}
		for _, item := range response.GetRules() {
			if item == nil {
				continue
			}
			result = append(result, Rule{RuleTag: item.GetRuleTag(), OutboundTag: item.GetTag()})
		}
		return nil
	})
	return result, err
}

func (c *Client) withRouting(parent context.Context, call func(RoutingServiceClient, context.Context) error) error {
	if c == nil {
		return errors.New("Xray RoutingService unavailable")
	}
	dial := c.DialContext
	if dial == nil {
		dial = grpc.DialContext
	}
	timeout := effectiveTimeout(c.RoutingTimeout)
	dialContext, cancelDial := context.WithTimeout(parent, timeout)
	conn, err := dial(dialContext, c.Address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	cancelDial()
	if err != nil {
		return errors.New("Xray RoutingService unavailable")
	}
	defer conn.Close()
	callContext, cancelCall := context.WithTimeout(parent, timeout)
	defer cancelCall()
	return call(NewRoutingServiceClient(conn), callContext)
}

func validRule(rule Rule) bool {
	return len(rule.RuleTag) >= len("xkeen-control-probe-") && len(rule.RuleTag) <= 128 && rule.InboundTag == "probe" && validUnifiedControlTag(rule.OutboundTag)
}

func validUnifiedControlTag(tag string) bool {
	if len(tag) <= len("proxy-") || len(tag) > 128 || !strings.HasPrefix(tag, "proxy-") {
		return false
	}
	for _, r := range tag[len("proxy-"):] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
