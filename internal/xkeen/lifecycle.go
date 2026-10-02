package xkeen

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/popiposter/xkeen-control/internal/authority"
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
	// ErrLifecycleAdmission forbids lifecycle fallback and transaction rollback
	// writes: the foreground command could not prove mutation ownership.
	ErrLifecycleAdmission = errors.New("native XKeen lifecycle admission lost; recovery required")
)

// Lifecycle is the fixed-command subprocess boundary. The caller owns admission,
// the transaction receipt and independent runtime verification. Exit zero means
// only that the native foreground command completed; it is not tunnel proof.
type Lifecycle struct {
	// Binary and Timeout are operator/test configuration, never request input.
	Binary string
	// InitPath selects native service control without dispatcher package repair.
	// It is operator configuration, mutually exclusive with Binary.
	InitPath string
	Timeout  time.Duration
}

func (l Lifecycle) Run(ctx context.Context, action LifecycleAction) error {
	err := authority.WithForeground(ctx, os.Environ(), func(env []string) error {
		err := l.runForeground(ctx, action, env)
		if errors.Is(err, ErrLifecycleUnknown) || errors.Is(err, ErrLifecycleAdmission) {
			authority.BlockContext(ctx)
		}
		return err
	})
	if errors.Is(err, authority.ErrOwnershipLost) || errors.Is(err, authority.ErrBlocked) {
		return errors.Join(ErrLifecycleAdmission, err)
	}
	return err
}

func (l Lifecycle) runForeground(ctx context.Context, action LifecycleAction, env []string) error {
	if action != Start && action != Stop && action != Restart {
		return ErrLifecycleFailed
	}
	if l.InitPath != "" && l.Binary != "" {
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
	args := []string{"-" + string(action)}
	if l.InitPath != "" {
		binary = l.InitPath
		args = []string{string(action), "on"}
	} else if binary == "" {
		binary = "/opt/sbin/xkeen"
	}
	command := exec.Command(binary, args...)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "XKEEN_FOREGROUND=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "XKEEN_FOREGROUND=1")
	// Nil output streams connect directly to os.DevNull. An io.Writer such as
	// io.Discard makes os/exec create pipes; native background services inherit
	// those pipes and keep Wait blocked after the foreground command has exited.
	// Never retain or expose native output, which can contain private config.
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
			// Reserved native-operation-gate.sh protocol refusals, not generic
			// command failures: callers must retain intent and cannot fall back
			// to Start or issue rollback writes without admission.
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				switch exit.ExitCode() {
				case 75, 76, 77:
					return ErrLifecycleAdmission
				}
			}
			return ErrLifecycleFailed
		}
		return nil
	case <-ctx.Done():
		killLifecycleProcess(command)
		// Bound cleanup even if the kernel cannot reap immediately.
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		return ErrLifecycleUnknown
	}
}
