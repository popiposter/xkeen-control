//go:build !linux

package xkeen

import "os/exec"

func configureLifecycleProcess(_ *exec.Cmd) {}
func killLifecycleProcess(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
