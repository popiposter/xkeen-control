package nodes

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/popiposter/xkeen-control/internal/xrayapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

type readyServer struct {
	xrayapi.UnimplementedRoutingServiceServer
	response  *xrayapi.ListRuleResponse
	call      func(context.Context) error
	calls     atomic.Int32
	active    atomic.Int32
	overlap   atomic.Bool
	closed    chan struct{}
	connected atomic.Int32
}

func (s *readyServer) ListRule(ctx context.Context, _ *xrayapi.ListRuleRequest) (*xrayapi.ListRuleResponse, error) {
	s.calls.Add(1)
	if s.active.Add(1) != 1 {
		s.overlap.Store(true)
	}
	defer s.active.Add(-1)
	if s.call != nil {
		if err := s.call(ctx); err != nil {
			return nil, err
		}
	}
	if s.response != nil {
		return s.response, nil
	}
	return &xrayapi.ListRuleResponse{}, nil
}
func (*readyServer) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context   { return ctx }
func (*readyServer) HandleRPC(context.Context, stats.RPCStats)                         {}
func (*readyServer) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context { return ctx }
func (s *readyServer) HandleConn(_ context.Context, v stats.ConnStats) {
	if _, ok := v.(*stats.ConnBegin); ok {
		s.connected.Add(1)
	}
	if _, ok := v.(*stats.ConnEnd); ok {
		select {
		case s.closed <- struct{}{}:
		default:
		}
	}
}
func startReadyServer(t *testing.T, s *readyServer) string {
	t.Helper()
	s.closed = make(chan struct{}, 8)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := grpc.NewServer(grpc.StatsHandler(s))
	xrayapi.RegisterRoutingServiceServer(server, s)
	go server.Serve(l)
	t.Cleanup(server.Stop)
	return l.Addr().String()
}
func assertReadyClosed(t *testing.T, s *readyServer) {
	t.Helper()
	if s.connected.Load() == 0 {
		return
	}
	select {
	case <-s.closed:
	case <-time.After(time.Second):
		t.Fatal("readiness connection not closed")
	}
}
func TestWaitReadyEmptySuccessDoesNotExecuteBinary(t *testing.T) {
	s := &readyServer{}
	a := CommandActivator{APIAddress: startReadyServer(t, s), XrayBinary: "/nonexistent/readiness-must-not-exec", ReadyTimeout: time.Second}
	if err := a.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.calls.Load() != 1 {
		t.Fatal(s.calls.Load())
	}
	assertReadyClosed(t, s)
	if a.ValidateCandidate(context.Background(), t.TempDir()) == nil {
		t.Fatal("candidate validation stopped using executable")
	}
}
func TestWaitReadyPermanentFailuresAreSanitized(t *testing.T) {
	for _, tt := range []struct {
		code   codes.Code
		reason string
	}{{codes.Unimplemented, "unsupported"}, {codes.PermissionDenied, "permission"}, {codes.Unauthenticated, "permission"}, {codes.Internal, "protocol"}, {codes.ResourceExhausted, "protocol"}} {
		t.Run(tt.reason+tt.code.String(), func(t *testing.T) {
			s := &readyServer{call: func(context.Context) error { return status.Error(tt.code, "PRIVATE server detail") }}
			err := waitRoutingReady(context.Background(), startReadyServer(t, s), time.Second, time.Millisecond)
			var ready *readinessError
			if !errors.As(err, &ready) || ready.reason != tt.reason || strings.Contains(err.Error(), "PRIVATE") || s.calls.Load() != 1 {
				t.Fatal(err, s.calls.Load())
			}
			assertReadyClosed(t, s)
		})
	}
}
func TestWaitReadyTransientRetryIsSequentialAndBackedOff(t *testing.T) {
	s := &readyServer{}
	s.call = func(context.Context) error {
		if s.calls.Load() < 3 {
			return status.Error(codes.Unavailable, "private")
		}
		return nil
	}
	addr := startReadyServer(t, s)
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitRoutingReady(ctx, addr, 100*time.Millisecond, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if s.calls.Load() != 3 || s.overlap.Load() || time.Since(start) < 50*time.Millisecond {
		t.Fatal("retry bounds", s.calls.Load())
	}
	assertReadyClosed(t, s)
}
func TestWaitReadyDeadlineAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "canceled"}[canceled], func(t *testing.T) {
			s := &readyServer{call: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
			addr := startReadyServer(t, s)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			if canceled {
				s.call = func(ctx context.Context) error { cancel(); <-ctx.Done(); return ctx.Err() }
			}
			start := time.Now()
			err := waitRoutingReady(ctx, addr, 30*time.Millisecond, 10*time.Millisecond)
			wanted := context.DeadlineExceeded
			if canceled {
				wanted = context.Canceled
			}
			if !errors.Is(err, wanted) || time.Since(start) > time.Second || s.calls.Load() > 9 {
				t.Fatal(err, s.calls.Load())
			}
			assertReadyClosed(t, s)
		})
	}
}
func TestWaitReadyUnavailableExhaustionAndPreCanceled(t *testing.T) {
	s := &readyServer{call: func(context.Context) error { return status.Error(codes.Unavailable, "private") }}
	addr := startReadyServer(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := waitRoutingReady(ctx, addr, 20*time.Millisecond, 25*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || failureReason("readiness", err) != "unavailable" || s.calls.Load() > 12 {
		t.Fatal(err, s.calls.Load())
	}
	assertReadyClosed(t, s)
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	before := s.calls.Load()
	if err = waitRoutingReady(ctx, addr, time.Second, time.Second); !errors.Is(err, context.Canceled) || s.calls.Load() != before {
		t.Fatal(err)
	}
}
func TestWaitReadyUsesShorterParentDeadline(t *testing.T) {
	s := &readyServer{call: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	a := CommandActivator{APIAddress: startReadyServer(t, s), ReadyTimeout: time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := a.WaitReady(ctx); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatal(err)
	}
	assertReadyClosed(t, s)
}

func TestWaitReadyBoundsResponseSize(t *testing.T) {
	s := &readyServer{response: &xrayapi.ListRuleResponse{Rules: []*xrayapi.ListRuleItem{{Tag: strings.Repeat("x", (8<<20)+1)}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := waitRoutingReady(ctx, startReadyServer(t, s), time.Second, time.Millisecond)
	if failureReason("readiness", err) != "protocol" || s.calls.Load() != 1 {
		t.Fatal(err, s.calls.Load())
	}
	assertReadyClosed(t, s)
}
