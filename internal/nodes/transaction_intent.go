package nodes

import (
	"context"
	"os"
	"path/filepath"
)

type nodeIntent struct {
	file *os.File
	info os.FileInfo
}

// The intent fences panel node writers across process restarts. It does not
// serialize external native CLI/cron; the panel does not exclude those writers.
func acquireNodeIntent(ctx context.Context, previousDir string) (*nodeIntent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(previousDir); err != nil {
		return nil, err
	}
	path := filepath.Join(previousDir, ".pending")
	pending, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, ErrNodeRecoveryRequired
	}
	_, err = pending.WriteString("node-operation-pending\n")
	if err == nil {
		err = pending.Sync()
	}
	if err != nil || syncNodeDirectory(previousDir) != nil {
		_ = pending.Close()
		return nil, ErrNodeRecoveryRequired
	}
	info, err := pending.Stat()
	if err != nil {
		_ = pending.Close()
		return nil, ErrNodeRecoveryRequired
	}
	if pending.Close() != nil {
		return nil, ErrNodeRecoveryRequired
	}
	return &nodeIntent{file: pending, info: info}, nil
}

func (i *nodeIntent) Close() { _ = i.file.Close() }

func (i *nodeIntent) Settle() error {
	return settleNodeIntent(i.file.Name(), i.info)
}

func settleNodeIntent(path string, original os.FileInfo) error {
	info, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, original) || info.Mode() != original.Mode() {
		return ErrNodeRecoveryRequired
	}
	if os.Remove(path) != nil || syncNodeDirectory(filepath.Dir(path)) != nil {
		return ErrNodeRecoveryRequired
	}
	return nil
}
