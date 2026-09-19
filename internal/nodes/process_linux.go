//go:build linux

package nodes

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func configureCommandProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killCommandProcessGroup(command *exec.Cmd) {
	if command.Process == nil {
		return
	}
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	_ = command.Process.Kill()
}

// managedXrayPIDs is deliberately narrower than pidof: an Xray PID is
// managed only while /proc/<pid>/exe names the fixed binary, including the
// kernel's exact " (deleted)" suffix during an atomic binary replacement.
func managedXrayPIDs(ctx context.Context, binary string) map[int]struct{} {
	result := make(map[int]struct{})
	if ctx == nil {
		ctx = context.Background()
	}
	if binary == "" {
		return result
	}
	contents, err := exec.CommandContext(ctx, "pidof", "xray").Output()
	if err != nil || len(contents) > 256 {
		return result
	}
	for _, field := range strings.Fields(string(contents)) {
		pid, parseErr := strconv.Atoi(field)
		if parseErr != nil || pid <= 0 {
			continue
		}
		executable, readErr := os.Readlink("/proc/" + field + "/exe")
		if readErr == nil && (executable == binary || executable == binary+" (deleted)") {
			result[pid] = struct{}{}
		}
	}
	return result
}

func signalXrayPID(pid int, force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(pid, signal)
}
