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

func TestWaitReadyHangingDialHasAttemptAndPhaseBounds(t *testing.T) {
	for _, phase := range []time.Duration{80 * time.Millisecond, 500 * time.Millisecond} {
		t.Run(phase.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), phase)
			defer cancel()
			durations := make(chan time.Duration, 16)
			var running atomic.Int32
			dial := func(call context.Context, _, _ string) (net.Conn, error) {
				running.Add(1)
				start := time.Now()
				deadline, ok := call.Deadline()
				if !ok || deadline.After(start.Add(101*time.Millisecond)) {
					t.Error("dial is not bounded by attempt")
				}
				<-call.Done()
				running.Add(-1)
				durations <- time.Since(start)
				return nil, call.Err()
			}
			err := waitRoutingReadyDial(ctx, "127.0.0.1:1", 100*time.Millisecond, 20*time.Millisecond, dial)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			select {
			case duration := <-durations:
				if duration > 300*time.Millisecond {
					t.Fatal("dial outlived attempt", duration)
				}
			case <-time.After(time.Second):
				t.Fatal("dial not canceled")
			}
			// The transport may be finishing cancellation concurrently with Close.
			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			for running.Load() != 0 {
				select {
				case <-durations:
				case <-deadline.C:
					t.Fatal("dial leaked after owner exit")
				}
			}
		})
	}
}
func TestWaitReadySilentHandshakeIsBounded(t *testing.T) {
	for _, tt := range []struct {
		name           string
		phase, attempt time.Duration
	}{{"attempt", 600 * time.Millisecond, 80 * time.Millisecond}, {"phase", 100 * time.Millisecond, time.Second}} {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			closed := make(chan time.Duration, 1)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				start := time.Now()
				_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 1024)
				for {
					if _, e = conn.Read(buf); e != nil {
						closed <- time.Since(start)
						return
					}
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), tt.phase)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- waitRoutingReady(ctx, listener.Addr().String(), tt.attempt, 20*time.Millisecond) }()
			select {
			case duration := <-closed:
				limit := tt.attempt
				if tt.phase < limit {
					limit = tt.phase
				}
				if duration > limit+250*time.Millisecond {
					t.Fatal("HTTP/2 handshake outlived bound", duration)
				}
			case <-time.After(time.Second):
				t.Fatal("silent transport not closed")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("readiness owner did not exit")
			}
		})
	}
}
