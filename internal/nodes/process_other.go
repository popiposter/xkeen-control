//go:build !linux

package nodes

import "os/exec"

func configureCommandProcessGroup(_ *exec.Cmd) {}

func killCommandProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
