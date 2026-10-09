//go:build !linux

package nodes

import (
	"context"
	"os"
)

func recoveryRead(path string, _ int, _ bool) ([]byte, os.FileInfo, error) {
	_, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil, err
	}
	return nil, nil, ErrNodeRecoveryRequired
}
func recoveryIdentity(os.FileInfo, []byte) string  { return "" }
func recoveryConfigNames(string) ([]string, error) { return nil, ErrNodeRecoveryRequired }

type ProcessRecoveryRuntime struct{ ProcRoot, Binary, ConfigDir, ReceiptPath string }

func (ProcessRecoveryRuntime) Snapshot(context.Context) (string, error) {
	return "", ErrNodeRecoveryRequired
}
