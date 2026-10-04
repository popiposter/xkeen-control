//go:build linux

package xkeen

import (
	"github.com/creack/pty"
	"os"
	"os/exec"
	"syscall"
)

func startNativeTerminal(c *exec.Cmd, interactive bool) (*os.File, error) {
	if !interactive {
		reader, writer, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		c.Stdout, c.Stderr = writer, writer
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		err = c.Start()
		_ = writer.Close()
		if err != nil {
			_ = reader.Close()
			return nil, err
		}
		return reader, nil
	}
	// stdin prompts need a PTY, not a controlling terminal that can SIGHUP
	// native background services when the dispatcher/session leader exits.
	f, err := pty.StartWithAttrs(c, &pty.Winsize{Cols: 100, Rows: 24}, &syscall.SysProcAttr{Setpgid: true})
	if err == nil {
		// os.NewFile must see O_NONBLOCK at construction to register Go's poller;
		// setting it later would make bounded input deadlines unsupported.
		fd, e := syscall.Dup(int(f.Fd()))
		if e == nil {
			e = syscall.SetNonblock(fd, true)
		}
		if e != nil {
			if fd >= 0 {
				_ = syscall.Close(fd)
			}
			_ = f.Close()
			_ = c.Process.Kill()
			_ = c.Wait()
			return nil, e
		}
		_ = f.Close()
		f = os.NewFile(uintptr(fd), "native-job-pty")
	}
	return f, err
}
func resizeNativeTerminal(f *os.File, cols, rows uint16) error {
	return pty.Setsize(f, &pty.Winsize{Cols: cols, Rows: rows})
}
func killNativeTerminal(c *exec.Cmd) {
	if c.Process != nil {
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
