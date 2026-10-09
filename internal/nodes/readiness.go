package nodes

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/popiposter/xkeen-control/internal/xkeen"
	"github.com/popiposter/xkeen-control/internal/xrayapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
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
	return waitRoutingReadyDial(ctx, address, attempt, backoff, (&net.Dialer{}).DialContext)
}

// The dial seam is private to transport-deadline fixtures.
func waitRoutingReadyDial(ctx context.Context, address string, attempt, delay time.Duration, dial func(context.Context, string, string) (net.Conn, error)) error {
	// Bound transport establishment too: an RPC deadline alone does not stop
	// gRPC's background dial or HTTP/2 handshake (default connect budget is 20s).
	transportBackoff := backoff.DefaultConfig
	transportBackoff.BaseDelay = delay
	transportBackoff.MaxDelay = attempt
	transportBackoff.Jitter = 0 // gRPC applies jitter after MaxDelay; never exceed the attempt cap.
	conn, err := grpc.NewClient("passthrough:///"+address,
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: transportBackoff, MinConnectTimeout: attempt}),
		grpc.WithContextDialer(func(transport context.Context, target string) (net.Conn, error) {
			bounded, cancel := context.WithTimeout(transport, attempt)
			defer cancel()
			stop := context.AfterFunc(ctx, cancel)
			defer stop()
			if deadline, ok := ctx.Deadline(); ok {
				var end context.CancelFunc
				bounded, end = context.WithDeadline(bounded, deadline)
				defer end()
			}
			return dial(bounded, "tcp", target)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(8<<20)))
	if err != nil {
		return &readinessError{reason: "protocol"}
	}
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
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
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return stopped()
		case <-timer.C:
		}
	}
}
