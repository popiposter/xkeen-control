package xrayapi

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

type Rule struct {
	RuleTag     string
	InboundTag  string
	OutboundTag string
	// BalancerTag and Domains form the panel's notification rule: exact
	// domains from the probe inbound to a balancer.
	BalancerTag string
	Domains     []string
}

// NotifyRule sends the panel's own Telegram Bot API traffic from the probe
// inbound through the node pool balancer. It matches only that domain, so
// probe traffic keeps its own temporary rules.
const (
	NotifyRuleTag  = "xkeen-control-notify"
	NotifyBalancer = "bal-proxy"
	NotifyDomain   = "api.telegram.org"
)

func NotifyRule() Rule {
	return Rule{RuleTag: NotifyRuleTag, InboundTag: "probe", BalancerTag: NotifyBalancer, Domains: []string{NotifyDomain}}
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
	add := func(nested bool) error {
		encoded, err := encodeRule(rule, nested)
		if err != nil {
			return errors.New("temporary routing rule encoding failed")
		}
		return c.withRouting(ctx, func(client RoutingServiceClient, callCtx context.Context) error {
			_, err := client.AddRule(callCtx, &AddRuleRequest{Config: &TypedMessage{Type: "xray.app.router.Config", Value: encoded}, ShouldAppend: shouldAppend})
			return err
		})
	}
	err := add(false)
	if err != nil && len(rule.Domains) > 0 && invalidWireData(err) {
		// Newer Xray wraps the domain in geodata.DomainRule and rejects the
		// older layout as invalid wire data. Only that rejection retries: an
		// older Xray would accept the nested bytes as a domain that never
		// matches.
		return add(true)
	}
	return err
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

func invalidWireData(err error) bool {
	text := err.Error()
	return strings.Contains(text, "invalid wire-format data") || strings.Contains(text, "cannot parse")
}

func validRule(rule Rule) bool {
	if rule.RuleTag == NotifyRuleTag {
		return rule.InboundTag == "probe" && rule.OutboundTag == "" && rule.BalancerTag == NotifyBalancer && len(rule.Domains) == 1 && rule.Domains[0] == NotifyDomain
	}
	return len(rule.RuleTag) >= len("xkeen-control-probe-") && len(rule.RuleTag) <= 128 && rule.InboundTag == "probe" && validUnifiedControlTag(rule.OutboundTag) && rule.BalancerTag == "" && len(rule.Domains) == 0
}

// encodeRule builds the router Config with one rule. The trimmed proto has no
// domain messages, so exact domains are appended by hand as RoutingRule field
// 2. Up to v26.3 that field is a Domain (type=1 Full=3, value=2); from v26.9
// it is a geodata.DomainRule whose custom=2 member holds that Domain.
func encodeRule(rule Rule, nested bool) ([]byte, error) {
	routingRule := &RoutingRule{RuleTag: rule.RuleTag, InboundTag: []string{rule.InboundTag}}
	if rule.BalancerTag != "" {
		routingRule.TargetTag = &RoutingRule_BalancingTag{BalancingTag: rule.BalancerTag}
	} else {
		routingRule.TargetTag = &RoutingRule_Tag{Tag: rule.OutboundTag}
	}
	body, err := proto.Marshal(routingRule)
	if err != nil {
		return nil, err
	}
	for _, domain := range rule.Domains {
		var message []byte
		message = protowire.AppendTag(message, 1, protowire.VarintType)
		message = protowire.AppendVarint(message, 3)
		message = protowire.AppendTag(message, 2, protowire.BytesType)
		message = protowire.AppendString(message, domain)
		if nested {
			var wrapped []byte
			wrapped = protowire.AppendTag(wrapped, 2, protowire.BytesType)
			message = protowire.AppendBytes(wrapped, message)
		}
		body = protowire.AppendTag(body, 2, protowire.BytesType)
		body = protowire.AppendBytes(body, message)
	}
	var config []byte
	config = protowire.AppendTag(config, 2, protowire.BytesType)
	config = protowire.AppendBytes(config, body)
	return config, nil
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
