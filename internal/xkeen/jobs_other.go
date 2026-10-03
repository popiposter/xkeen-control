//go:build !linux

package xkeen

import (
	"os"
	"os/exec"
)

func startNativeTerminal(*exec.Cmd, bool) (*os.File, error) { return nil, ErrJob }
func resizeNativeTerminal(*os.File, uint16, uint16) error   { return ErrJob }
func killNativeTerminal(c *exec.Cmd) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}
