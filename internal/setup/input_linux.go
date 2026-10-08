//go:build linux

package setup

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func privateSource(ctx context.Context, in *os.File, out io.Writer) (string, error) {
	if in == nil {
		return "", ErrState
	}
	fd := int(in.Fd())
	if term.IsTerminal(fd) {
		state, e := unix.IoctlGetTermios(fd, unix.TCGETS)
		if e != nil {
			return "", ErrState
		}
		newState := *state
		newState.Lflag &^= unix.ECHO | unix.ECHONL
		if unix.IoctlSetTermios(fd, unix.TCSETS, &newState) != nil {
			return "", ErrState
		}
		defer unix.IoctlSetTermios(fd, unix.TCSETS, state)
	}
	_, _ = io.WriteString(out, "Вставьте ссылку на подписку или VLESS-узел (ввод скрыт): ")
	type answer struct {
		s string
		e error
	}
	done := make(chan answer, 1)
	go func() {
		r := bufio.NewReaderSize(io.LimitReader(in, 8193), 8193)
		b, e := r.ReadString('\n')
		if e != nil && e != io.EOF || len(b) > 8192 {
			done <- answer{e: ErrState}
			return
		}
		b = strings.TrimSuffix(strings.TrimSuffix(b, "\n"), "\r")
		if len(b) == 0 || strings.ContainsAny(b, "\x00\r\n") {
			done <- answer{e: ErrState}
			return
		}
		done <- answer{s: b}
	}()
	select {
	case <-ctx.Done():
		return "", ErrState
	case a := <-done:
		_, _ = io.WriteString(out, "\n")
		return a.s, a.e
	}
}
