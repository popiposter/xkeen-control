//go:build !linux

package setup

import (
	"context"
	"io"
	"os"
)

func Run(context.Context, *os.File, io.Writer) error { return ErrUnsupported }
func Recover(context.Context, io.Writer) error       { return ErrUnsupported }
func Abort(context.Context, io.Writer) error         { return ErrUnsupported }
