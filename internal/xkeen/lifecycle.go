package xkeen

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type LifecycleAction string

const (
	Start   LifecycleAction = "start"
	Stop    LifecycleAction = "stop"
	Restart LifecycleAction = "restart"
)

var (
	ErrLifecycleFailed  = errors.New("native XKeen lifecycle failed")
	ErrLifecycleUnknown = errors.New("native XKeen lifecycle outcome unknown; inspect before retry")
)

// Lifecycle is the fixed-command subprocess boundary. The caller owns admission,
// the transaction receipt and independent runtime verification. Exit zero means
// only that the native foreground command completed; it is not tunnel proof.
type Lifecycle struct {
	// Binary and Timeout are operator/test configuration, never request input.
	Binary  string
	Timeout time.Duration
}

func (l Lifecycle) Run(ctx context.Context, action LifecycleAction) error {
	if action != Start && action != Stop && action != Restart {
		return ErrLifecycleFailed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	limit := l.Timeout
	if limit <= 0 {
		limit = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	if ctx.Err() != nil {
		return ErrLifecycleFailed
	}
	binary := l.Binary
	if binary == "" {
		binary = "/opt/sbin/xkeen"
	}
	command := exec.Command(binary, "-"+string(action))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "XKEEN_FOREGROUND=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "XKEEN_FOREGROUND=1")
	// Native output can contain private configuration; never retain or expose it.
	command.Stdout, command.Stderr = io.Discard, io.Discard
	configureLifecycleProcess(command)
	if command.Start() != nil {
		return ErrLifecycleFailed
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if ctx.Err() != nil {
			return ErrLifecycleUnknown
		}
		if err != nil {
			return ErrLifecycleFailed
		}
		return nil
	case <-ctx.Done():
		killLifecycleProcess(command)
		// Pipes are discarded directly, so inherited output handles cannot hold
		// Wait open. Bound cleanup even if the kernel cannot reap immediately.
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		return ErrLifecycleUnknown
	}
}
