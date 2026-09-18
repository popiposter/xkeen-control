//go:build !linux

package nodes

import (
	"context"
	"os/exec"
)

func configureCommandProcessGroup(_ *exec.Cmd) {}

func killCommandProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}

func managedXrayPIDs(context.Context, string) map[int]struct{} { return map[int]struct{}{} }

func signalXrayPID(int, bool) error { return nil }
