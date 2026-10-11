package xrayapi

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

type fakeRoutingServer struct {
	UnimplementedRoutingServiceServer
}

func (fakeRoutingServer) GetBalancerInfo(context.Context, *GetBalancerInfoRequest) (*GetBalancerInfoResponse, error) {
	return &GetBalancerInfoResponse{Balancer: &BalancerMsg{
		PrincipleTarget: &PrincipleTargetInfo{Tag: []string{"proxy-main-01"}},
		Override:        &OverrideInfo{Target: "proxy-us-02"},
	}}, nil
}

type fakeObservatoryServer struct {
	UnimplementedObservatoryServiceServer
}

func (fakeObservatoryServer) GetOutboundStatus(context.Context, *GetOutboundStatusRequest) (*GetOutboundStatusResponse, error) {
	return &GetOutboundStatusResponse{Status: &ObservationResult{Status: []*OutboundStatus{{
		Alive: true, Delay: 37, OutboundTag: "proxy-main-01", LastSeenTime: 1700000000, LastTryTime: 1700000001, LastErrorReason: "UUID-SENTINEL timeout",
	}}}}, nil
}

func TestClientUsesStructuredRoutingAndObservatoryContracts(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	RegisterRoutingServiceServer(server, fakeRoutingServer{})
	RegisterObservatoryServiceServer(server, fakeObservatoryServer{})
	go server.Serve(listener)
	defer server.Stop()

	client := NewClient(listener.Addr().String(), "127.0.0.1:1", time.Second)
	got := client.Snapshot(context.Background())
	if !got.APIReachable || !got.RoutingReachable || !got.ObservatoryReachable {
		t.Fatalf("reachability = %+v", got)
	}
	if got.Balancer.NativeSelected != "proxy-main-01" || got.Balancer.Override != "proxy-us-02" {
		t.Fatalf("balancer = %+v", got.Balancer)
	}
	if len(got.OutboundHealth) != 1 || got.OutboundHealth[0].DelayMS != 37 || got.OutboundHealth[0].LastSeen.IsZero() {
		t.Fatalf("observatory = %+v", got.OutboundHealth)
	}
}

func TestReadBalancerRuntimeUsesRoutingService(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	RegisterRoutingServiceServer(server, fakeRoutingServer{})
	go server.Serve(listener)
	defer server.Stop()

	got, err := ReadBalancerRuntime(context.Background(), listener.Addr().String(), "bal-proxy", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PrincipleTargets) != 1 || got.PrincipleTargets[0] != "proxy-main-01" || got.Override != "proxy-us-02" {
		t.Fatalf("balancer runtime = %+v", got)
	}
}

type controlRoutingServer struct {
	UnimplementedRoutingServiceServer
	override *OverrideBalancerTargetRequest
	add      *AddRuleRequest
	remove   *RemoveRuleRequest
}

func (s *controlRoutingServer) OverrideBalancerTarget(_ context.Context, request *OverrideBalancerTargetRequest) (*OverrideBalancerTargetResponse, error) {
	s.override = request
	return &OverrideBalancerTargetResponse{}, nil
}

func (s *controlRoutingServer) AddRule(_ context.Context, request *AddRuleRequest) (*AddRuleResponse, error) {
	s.add = request
	return &AddRuleResponse{}, nil
}

func (s *controlRoutingServer) RemoveRule(_ context.Context, request *RemoveRuleRequest) (*RemoveRuleResponse, error) {
	s.remove = request
	return &RemoveRuleResponse{}, nil
}

func TestClientUsesTypedAppendOnlyProbeRuleAndRuntimeOverride(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	control := &controlRoutingServer{}
	server := grpc.NewServer()
	RegisterRoutingServiceServer(server, control)
	go server.Serve(listener)
	defer server.Stop()

	client := NewClient(listener.Addr().String(), "127.0.0.1:1", time.Second)
	if err := client.OverrideBalancerTarget(context.Background(), "bal-proxy", "proxy-main-01"); err != nil {
		t.Fatal(err)
	}
	if err := client.AddRule(context.Background(), Rule{RuleTag: "xkeen-control-probe-liveness", InboundTag: "probe", OutboundTag: "proxy-main-01"}, true); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveRule(context.Background(), "xkeen-control-probe-liveness"); err != nil {
		t.Fatal(err)
	}
	if control.override == nil || control.override.GetBalancerTag() != "bal-proxy" || control.override.GetTarget() != "proxy-main-01" {
		t.Fatalf("override request = %+v", control.override)
	}
	if control.add == nil || !control.add.GetShouldAppend() || control.add.GetConfig().GetType() != "xray.app.router.Config" {
		t.Fatalf("add request = %+v", control.add)
	}
	var config Config
	if err := proto.Unmarshal(control.add.GetConfig().GetValue(), &config); err != nil {
		t.Fatal(err)
	}
	if len(config.GetRule()) != 1 {
		t.Fatalf("typed config rules = %+v", config.GetRule())
	}
	rule := config.GetRule()[0]
	if rule.GetRuleTag() != "xkeen-control-probe-liveness" || rule.GetInboundTag()[0] != "probe" || rule.GetTag() != "proxy-main-01" {
		t.Fatalf("typed rule = %+v", &rule)
	}
	if control.remove == nil || control.remove.GetRuleTag() != "xkeen-control-probe-liveness" {
		t.Fatalf("remove request = %+v", control.remove)
	}
}

type emptyPrincipleTargetRoutingServer struct {
	UnimplementedRoutingServiceServer
}

func (emptyPrincipleTargetRoutingServer) GetBalancerInfo(context.Context, *GetBalancerInfoRequest) (*GetBalancerInfoResponse, error) {
	return &GetBalancerInfoResponse{Balancer: &BalancerMsg{
		// This is the real Xray no-healthy-node shape: one empty tag entry.
		PrincipleTarget: &PrincipleTargetInfo{Tag: []string{""}},
	}}, nil
}

func TestReadBalancerRuntimeTreatsEmptyPrincipleTargetAsNoSelection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	RegisterRoutingServiceServer(server, emptyPrincipleTargetRoutingServer{})
	go server.Serve(listener)
	defer server.Stop()

	got, err := ReadBalancerRuntime(context.Background(), listener.Addr().String(), "bal-proxy", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PrincipleTargets) != 0 || got.Override != "" {
		t.Fatalf("empty native selection was not normalized: %+v", got)
	}
}

type delayedXrayServer struct {
	UnimplementedRoutingServiceServer
	UnimplementedObservatoryServiceServer
	routingDelay     time.Duration
	observatoryDelay time.Duration
}

func (s delayedXrayServer) GetBalancerInfo(ctx context.Context, _ *GetBalancerInfoRequest) (*GetBalancerInfoResponse, error) {
	select {
	case <-time.After(s.routingDelay):
		return &GetBalancerInfoResponse{Balancer: &BalancerMsg{PrincipleTarget: &PrincipleTargetInfo{Tag: []string{"proxy-main-01"}}}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s delayedXrayServer) GetOutboundStatus(ctx context.Context, _ *GetOutboundStatusRequest) (*GetOutboundStatusResponse, error) {
	select {
	case <-time.After(s.observatoryDelay):
		return &GetOutboundStatusResponse{Status: &ObservationResult{Status: []*OutboundStatus{{OutboundTag: "proxy-main-01", Alive: true}}}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestClientSeparatesRoutingAndObservatoryDeadlines(t *testing.T) {
	tests := []struct {
		name                 string
		routingDelay         time.Duration
		observatoryDelay     time.Duration
		routingReachable     bool
		observatoryReachable bool
		routingTimeout       bool
	}{
		{name: "routing degradation preserves observatory", routingDelay: 80 * time.Millisecond, observatoryDelay: 1 * time.Millisecond, routingReachable: false, observatoryReachable: true, routingTimeout: true},
		{name: "observatory degradation preserves routing", routingDelay: 1 * time.Millisecond, observatoryDelay: 80 * time.Millisecond, routingReachable: true, observatoryReachable: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := grpc.NewServer()
			RegisterRoutingServiceServer(server, delayedXrayServer{routingDelay: test.routingDelay, observatoryDelay: test.observatoryDelay})
			RegisterObservatoryServiceServer(server, delayedXrayServer{routingDelay: test.routingDelay, observatoryDelay: test.observatoryDelay})
			go server.Serve(listener)
			defer server.Stop()

			client := NewClient(listener.Addr().String(), "127.0.0.1:1", time.Second)
			client.RoutingTimeout = 20 * time.Millisecond
			client.ObservatoryTimeout = 200 * time.Millisecond
			if !test.routingTimeout {
				client.RoutingTimeout = 200 * time.Millisecond
				client.ObservatoryTimeout = 20 * time.Millisecond
			}
			got := client.Snapshot(context.Background())
			if !got.APIReachable || got.RoutingReachable != test.routingReachable || got.ObservatoryReachable != test.observatoryReachable {
				t.Fatalf("partial reachability = %+v", got)
			}
			if got.RoutingReachable && got.RoutingErrorClass != "" {
				t.Fatalf("unexpected routing error = %q", got.RoutingErrorClass)
			}
			if !got.RoutingReachable && got.RoutingErrorClass != "timeout" {
				t.Fatalf("routing error = %q", got.RoutingErrorClass)
			}
			if got.ObservatoryReachable && got.ObservatoryErrorClass != "" {
				t.Fatalf("unexpected observatory error = %q", got.ObservatoryErrorClass)
			}
			if !got.ObservatoryReachable && got.ObservatoryErrorClass != "timeout" {
				t.Fatalf("observatory error = %q", got.ObservatoryErrorClass)
			}
		})
	}
}

// The notify rule matches one exact domain from the probe inbound and targets
// the pool balancer, in both Xray domain layouts.
func TestNotifyRuleEncodingBothDomainLayouts(t *testing.T) {
	rule := NotifyRule()
	if !validRule(rule) {
		t.Fatal("notify rule rejected")
	}
	for _, bad := range []Rule{
		{RuleTag: NotifyRuleTag, InboundTag: "probe", BalancerTag: "bal-other", Domains: []string{NotifyDomain}},
		{RuleTag: NotifyRuleTag, InboundTag: "probe", BalancerTag: NotifyBalancer, Domains: []string{"example.com"}},
		{RuleTag: NotifyRuleTag, InboundTag: "redirect", BalancerTag: NotifyBalancer, Domains: []string{NotifyDomain}},
		{RuleTag: "xkeen-control-probe-rtt", InboundTag: "probe", OutboundTag: "proxy-a", Domains: []string{NotifyDomain}},
	} {
		if validRule(bad) {
			t.Fatal("accepted", bad)
		}
	}
	for _, nested := range []bool{false, true} {
		encoded, err := encodeRule(rule, nested)
		if err != nil {
			t.Fatal(err)
		}
		var config Config
		if err := proto.Unmarshal(encoded, &config); err != nil || len(config.Rule) != 1 {
			t.Fatal("config did not decode", err)
		}
		got := config.Rule[0]
		if got.GetBalancingTag() != NotifyBalancer || got.GetRuleTag() != NotifyRuleTag || len(got.GetInboundTag()) != 1 || got.GetInboundTag()[0] != "probe" {
			t.Fatal("rule fields", got)
		}
		// Field 2 carries the domain: Domain{type Full, value} directly, or
		// wrapped as DomainRule.custom (field 2).
		raw := got.ProtoReflect().GetUnknown()
		num, typ, n := protowire.ConsumeTag(raw)
		if num != 2 || typ != protowire.BytesType || n < 0 {
			t.Fatal("domain field missing", nested)
		}
		domain, _ := protowire.ConsumeBytes(raw[n:])
		if nested {
			num, _, n = protowire.ConsumeTag(domain)
			if num != 2 {
				t.Fatal("DomainRule.custom missing")
			}
			domain, _ = protowire.ConsumeBytes(domain[n:])
		}
		want := protowire.AppendString(protowire.AppendTag(protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 3), 2, protowire.BytesType), NotifyDomain)
		if string(domain) != string(want) {
			t.Fatalf("domain bytes %x want %x", domain, want)
		}
	}
	plain, err := encodeRule(Rule{RuleTag: "xkeen-control-probe-rtt", InboundTag: "probe", OutboundTag: "proxy-a"}, false)
	var config Config
	if err != nil || proto.Unmarshal(plain, &config) != nil || config.Rule[0].GetTag() != "proxy-a" || len(config.Rule[0].ProtoReflect().GetUnknown()) != 0 {
		t.Fatal("probe rule encoding changed")
	}
}
