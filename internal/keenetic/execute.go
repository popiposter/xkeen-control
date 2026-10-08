package keenetic

import (
	"context"
	"io"
	"os/exec"
	"time"
)

type boundedOutput struct{ data []byte }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 1<<20 {
		return 0, ErrCapability
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func execute(ctx context.Context, c command, change string) ([]byte, error) {
	cmd := ""
	switch c {
	case version:
		cmd = "show version"
	case interfaces:
		cmd = "show interface"
	case policies:
		cmd = "show ip policy"
	case running:
		cmd = "show running-config"
	case startup:
		cmd = "more flash:startup-config"
	case dnsRuntime:
		cmd = "show dns-proxy"
	case mutation:
		cmd = change
	default:
		return nil, ErrCapability
	}
	if cmd == "" {
		return nil, ErrCapability
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p := exec.CommandContext(ctx, "/bin/ndmc", "-c", cmd)
	p.WaitDelay = time.Second
	b := &boundedOutput{}
	p.Stdout = b
	p.Stderr = io.Discard
	if p.Run() != nil {
		return nil, ErrCapability
	}
	return b.data, nil
}
