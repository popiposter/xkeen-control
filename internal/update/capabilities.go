package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CapabilityReport reports fixed prerequisites, not permission to update or
// proof that an interrupted transaction completed.
type CapabilityReport struct {
	Ready   bool   `json:"ready"`
	Sync    string `json:"sync"`
	Timeout string `json:"timeout"`
	Flock   string `json:"flock"`
	Reason  string `json:"reason,omitempty"`
}

type capabilityDependencies struct {
	lookup   func(string) (string, error)
	run      func(context.Context, string, []string, []*os.File) (int, error)
	tempRoot string
}

func productionCapabilities() capabilityDependencies {
	return capabilityDependencies{lookup: func(name string) (string, error) {
		if name == "sync" {
			if i, e := os.Stat("/opt/bin/sync"); e == nil && i.Mode().IsRegular() && i.Mode().Perm()&0111 != 0 {
				return "/opt/bin/sync", nil
			}
		}
		return exec.LookPath(name)
	}, run: runCapability, tempRoot: "/tmp"}
}

func runCapability(ctx context.Context, path string, args []string, files []*os.File) (int, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.ExtraFiles = files
	err := cmd.Run()
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return -1, err
}

func (m *Manager) durabilityReady() error {
	_, err := checkCapabilities(context.Background(), []string{m.paths.MarkerPath, filepath.Dir(m.paths.MarkerPath)}, productionCapabilities())
	return err
}

// InspectCapabilities uses the identical admission checks on disposable RAM
// data. It has no installed-state, network, service or reservation dependency.
func InspectCapabilities(ctx context.Context) (CapabilityReport, error) {
	return inspectCapabilities(ctx, productionCapabilities())
}

func inspectCapabilities(ctx context.Context, d capabilityDependencies) (CapabilityReport, error) {
	r := emptyCapabilityReport()
	if ctx.Err() != nil {
		return capabilityFailure(r, "probe", ctx.Err())
	}
	dir, err := os.MkdirTemp(d.tempRoot, "xkeen-capabilities-*")
	if err != nil {
		return capabilityFailure(r, "temporary-storage", err)
	}
	defer os.RemoveAll(dir)
	file, err := os.CreateTemp(dir, "sync-*")
	if err != nil {
		return capabilityFailure(r, "temporary-storage", err)
	}
	path := file.Name()
	if err = file.Close(); err != nil {
		return capabilityFailure(r, "temporary-storage", err)
	}
	return checkCapabilities(ctx, []string{path, dir}, d)
}

func emptyCapabilityReport() CapabilityReport {
	return CapabilityReport{Sync: "not-run", Timeout: "not-run", Flock: "not-run"}
}

func capabilityFailure(r CapabilityReport, stage string, err error) (CapabilityReport, error) {
	r.Reason = stage + "-unsupported"
	if errors.Is(err, context.Canceled) {
		r.Reason = stage + "-canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		r.Reason = stage + "-timeout"
	}
	return r, errors.New("panel update capability: " + r.Reason)
}

func checkCapabilities(parent context.Context, paths []string, d capabilityDependencies) (CapabilityReport, error) {
	r := emptyCapabilityReport()
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	syncPath, err := d.lookup("sync")
	if err != nil {
		r.Sync = "failed"
		return capabilityFailure(r, "sync", err)
	}
	for _, path := range paths {
		probe, close := context.WithTimeout(ctx, 5*time.Second)
		status, e := d.run(probe, syncPath, []string{"-f", path}, nil)
		close()
		if e != nil || status != 0 {
			r.Sync = "failed"
			return capabilityFailure(r, "sync", e)
		}
	}
	r.Sync = "ready"
	timeoutPath, err := d.lookup("timeout")
	if err != nil {
		r.Timeout = "failed"
		return capabilityFailure(r, "timeout", err)
	}
	probe, close := context.WithTimeout(ctx, 3*time.Second)
	status, err := d.run(probe, timeoutPath, []string{"-k", "1", "1", "true"}, nil)
	close()
	if err != nil || status != 0 {
		r.Timeout = "failed"
		return capabilityFailure(r, "timeout", err)
	}
	r.Timeout = "ready"
	if err = flockReady(ctx, d); err != nil {
		r.Flock = "failed"
		return capabilityFailure(r, "flock", err)
	}
	r.Flock = "ready"
	r.Ready = true
	return r, nil
}

// Parent-owned descriptions survive each direct child, so the two expected
// conflicts prove exclusion and same-description EX->SH conversion. No shell
// positional arguments or independent per-step deadlines are involved.
func flockReady(parent context.Context, d capabilityDependencies) error {
	path, err := d.lookup("flock")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(d.tempRoot, "xkeen-flock-check-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	g, err := os.Open(f.Name())
	if err != nil {
		return err
	}
	defer g.Close()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	for _, step := range []struct {
		mode, fd string
		want     int
	}{{"-x", "3", 0}, {"-s", "4", 1}, {"-s", "3", 0}, {"-s", "4", 0}, {"-x", "4", 1}} {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status, e := d.run(ctx, path, []string{"-n", step.mode, step.fd}, []*os.File{f, g})
		if e != nil {
			return e
		}
		if status != step.want {
			return errors.New("flock descriptor semantics unavailable")
		}
	}
	return nil
}
