package nodes

import (
	"context"
	"errors"
	"time"

	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// readinessError carries only an allowlisted class, never RPC details or rules.
type readinessError struct {
	reason string
	cause  error
}

func (e *readinessError) Error() string { return "Xray readiness: " + e.reason }
func (e *readinessError) Unwrap() error { return e.cause }

func failureReason(stage string, err error) string {
	if errors.Is(err, xkeen.ErrLifecycleUnknown) {
		return "unknown"
	}
	var ready *readinessError
	if stage == "readiness" && errors.As(err, &ready) {
		return ready.reason
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	return "failed"
}

func (a CommandActivator) WaitReady(ctx context.Context) error {
	timeout := a.ReadyTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	address := a.APIAddress
	if address == "" {
		address = xrayapi.DefaultAPIAddress
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return waitRoutingReady(ctx, address, 2*time.Second, 500*time.Millisecond)
}

func waitRoutingReady(ctx context.Context, address string, attempt, backoff time.Duration) error {
	// One lazy connection for this bounded invocation; retries never spawn Xray.
	conn, err := grpc.NewClient("passthrough:///"+address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(8<<20)))
	if err != nil {
		return &readinessError{reason: "protocol"}
	}
	defer conn.Close()
	client := xrayapi.NewRoutingServiceClient(conn)
	last := "deadline"
	stopped := func() error {
		reason := last
		if errors.Is(ctx.Err(), context.Canceled) {
			reason = "canceled"
		}
		return &readinessError{reason: reason, cause: ctx.Err()}
	}
	for {
		if ctx.Err() != nil {
			return stopped()
		}
		call, cancel := context.WithTimeout(ctx, attempt)
		_, err = client.ListRule(call, &xrayapi.ListRuleRequest{})
		cancel()
		if ctx.Err() != nil {
			return stopped()
		}
		if err == nil {
			return nil
		}
		switch status.Code(err) {
		case codes.Unavailable:
			last = "unavailable"
		case codes.DeadlineExceeded:
			last = "deadline"
		case codes.Unimplemented:
			return &readinessError{reason: "unsupported"}
		case codes.PermissionDenied, codes.Unauthenticated:
			return &readinessError{reason: "permission"}
		default:
			return &readinessError{reason: "protocol"}
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return stopped()
		case <-timer.C:
		}
	}
}
