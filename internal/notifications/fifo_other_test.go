//go:build !linux

package notifications

import "testing"

func makeFIFO(t *testing.T, path string) { t.Helper(); t.Skip("Linux FIFO fixture") }
