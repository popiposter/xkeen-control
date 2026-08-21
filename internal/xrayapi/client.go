package xrayapi

import (
	"context"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	DefaultAPIAddress   = "127.0.0.1:10085"
	DefaultProbeAddress = "127.0.0.1:10808"
)

// BalancerState is the structured RoutingService projection used by the
// control plane. NativeSelected comes from the strategy's principle target;
// Override comes from XKeen's runtime override on the same balancer.
type BalancerState struct {
	NativeSelected string
	Override       string
}

type OutboundHealth struct {
	Tag       string
	Alive     bool
	DelayMS   int64
	LastSeen  time.Time
	LastTry   time.Time
	LastError string
}

// Snapshot deliberately contains only the fields needed by the read-only
// dashboard. It never carries raw protobuf messages outside this package.
type Snapshot struct {
	APIReachable          bool
	RoutingReachable      bool
	ObservatoryReachable  bool
	Balancer              BalancerState
	OutboundHealth        []OutboundHealth
	RoutingErrorClass     string
	ObservatoryErrorClass string
}

type Reader interface {
	Snapshot(context.Context) Snapshot
	ProbeReachable(context.Context) bool
}

type Client struct {
	Address            string
	ProbeAddr          string
	DialTimeout        time.Duration
	RoutingTimeout     time.Duration
	ObservatoryTimeout time.Duration
	ProbeTimeout       time.Duration
	DialContext        func(context.Context, string, ...grpc.DialOption) (*grpc.ClientConn, error)
}

func NewClient(address, probeAddress string, timeout time.Duration) *Client {
	if address == "" {
		address = DefaultAPIAddress
	}
	if probeAddress == "" {
		probeAddress = DefaultProbeAddress
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &Client{
		Address:            address,
		ProbeAddr:          probeAddress,
		DialTimeout:        timeout,
		RoutingTimeout:     timeout,
		ObservatoryTimeout: timeout,
		ProbeTimeout:       timeout,
		DialContext:        grpc.DialContext,
	}
}

func (c *Client) Snapshot(parent context.Context) Snapshot {
	result := Snapshot{}
	if c == nil {
		result.RoutingErrorClass = "unavailable"
		result.ObservatoryErrorClass = "unavailable"
		return result
	}

	dial := c.DialContext
	if dial == nil {
		dial = grpc.DialContext
	}
	dialContext, cancelDial := context.WithTimeout(parent, effectiveTimeout(c.DialTimeout))
	conn, err := dial(dialContext, c.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	cancelDial()
	if err != nil {
		class := classifyRPCError(err)
		result.RoutingErrorClass = class
		result.ObservatoryErrorClass = class
		return result
	}
	defer conn.Close()
	result.APIReachable = true

	routing := NewRoutingServiceClient(conn)
	routingContext, cancelRouting := context.WithTimeout(parent, effectiveTimeout(c.RoutingTimeout))
	balancer, err := routing.GetBalancerInfo(routingContext, &GetBalancerInfoRequest{Tag: "bal-proxy"})
	cancelRouting()
	if err != nil {
		result.RoutingErrorClass = classifyRPCError(err)
	} else {
		result.RoutingReachable = true
		if balancer.GetBalancer() != nil {
			if native := balancer.GetBalancer().GetPrincipleTarget(); native != nil && len(native.GetTag()) > 0 {
				result.Balancer.NativeSelected = native.GetTag()[0]
			}
			if override := balancer.GetBalancer().GetOverride(); override != nil {
				result.Balancer.Override = override.GetTarget()
			}
		}
	}

	observatory := NewObservatoryServiceClient(conn)
	observatoryContext, cancelObservatory := context.WithTimeout(parent, effectiveTimeout(c.ObservatoryTimeout))
	status, err := observatory.GetOutboundStatus(observatoryContext, &GetOutboundStatusRequest{})
	cancelObservatory()
	if err != nil {
		result.ObservatoryErrorClass = classifyRPCError(err)
	} else {
		result.ObservatoryReachable = true
		if observation := status.GetStatus(); observation != nil {
			for _, item := range observation.GetStatus() {
				if item == nil {
					continue
				}
				result.OutboundHealth = append(result.OutboundHealth, OutboundHealth{
					Tag:       item.GetOutboundTag(),
					Alive:     item.GetAlive(),
					DelayMS:   item.GetDelay(),
					LastSeen:  unixTime(item.GetLastSeenTime()),
					LastTry:   unixTime(item.GetLastTryTime()),
					LastError: item.GetLastErrorReason(),
				})
			}
		}
	}

	return result
}

func (c *Client) ProbeReachable(parent context.Context) bool {
	if c == nil || c.ProbeAddr == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, effectiveTimeout(c.ProbeTimeout))
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", c.ProbeAddr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func effectiveTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return 2 * time.Second
	}
	return value
}

func unixTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(value, 0).UTC()
}

func classifyRPCError(err error) string {
	if err == nil {
		return ""
	}
	if _, ok := err.(net.Error); ok {
		return "timeout"
	}
	if statusCode := grpc.Code(err); statusCode.String() == "DeadlineExceeded" {
		return "timeout"
	}
	return "unavailable"
}
