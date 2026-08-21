package xrayapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type BalancerRuntime struct {
	PrincipleTargets []string
	Override         string
}

func ReadBalancerRuntime(parent context.Context, address, tag string, timeout time.Duration) (BalancerRuntime, error) {
	if address == "" {
		address = DefaultAPIAddress
	}
	if tag == "" {
		return BalancerRuntime{}, errors.New("balancer tag is required")
	}
	timeout = effectiveTimeout(timeout)
	dialContext, cancelDial := context.WithTimeout(parent, timeout)
	conn, err := grpc.DialContext(dialContext, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	cancelDial()
	if err != nil {
		return BalancerRuntime{}, errors.New("Xray RoutingService unavailable")
	}
	defer conn.Close()

	requestContext, cancelRequest := context.WithTimeout(parent, timeout)
	response, err := NewRoutingServiceClient(conn).GetBalancerInfo(requestContext, &GetBalancerInfoRequest{Tag: tag})
	cancelRequest()
	if err != nil || response.GetBalancer() == nil {
		return BalancerRuntime{}, errors.New("Xray balancer unavailable")
	}
	result := BalancerRuntime{}
	if principle := response.GetBalancer().GetPrincipleTarget(); principle != nil {
		for _, target := range principle.GetTag() {
			// Xray's least-ping strategy reports an empty principle target when
			// no healthy outbound is selected. Empty is the native "no
			// selection" state, not a foreign outbound tag.
			if strings.TrimSpace(target) != "" {
				result.PrincipleTargets = append(result.PrincipleTargets, target)
			}
		}
	}
	if override := response.GetBalancer().GetOverride(); override != nil {
		result.Override = override.GetTarget()
	}
	return result, nil
}
