package nodes

import (
	"context"
	"os"
	"path/filepath"

	"github.com/popiposter/xkeen-control/internal/authority"
)

type nodeIntent struct {
	file    *os.File
	info    os.FileInfo
	binding authority.NodeIntentBinding
}

// The intent fences panel node writers across process restarts. It does not
// serialize external native CLI/cron; that requires the native admission seam.
func acquireNodeIntent(ctx context.Context, previousDir string) (*nodeIntent, error) {
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
	binding, err := authority.BindNodeIntent(ctx, pending)
	if err != nil {
		_ = pending.Close()
		return nil, ErrNodeRecoveryRequired
	}
	// Only native binding needs a live descriptor through lifecycle. Preserve the
	// original close-before-unlink behavior for ordinary authority (Windows open
	// handles do not permit this file's later deletion).
	if binding == nil && pending.Close() != nil {
		return nil, ErrNodeRecoveryRequired
	}
	return &nodeIntent{file: pending, info: info, binding: binding}, nil
}

func (i *nodeIntent) Close() { _ = i.file.Close() }

func (i *nodeIntent) Settle() error {
	if i.binding != nil && i.binding.Verify() != nil {
		return ErrNodeRecoveryRequired
	}
	info, err := os.Lstat(i.file.Name())
	if err != nil || !os.SameFile(info, i.info) || info.Mode() != i.info.Mode() {
		return ErrNodeRecoveryRequired
	}
	if os.Remove(i.file.Name()) != nil || syncNodeDirectory(filepath.Dir(i.file.Name())) != nil {
		return ErrNodeRecoveryRequired
	}
	if i.binding != nil && i.binding.Clear() != nil {
		return ErrNodeRecoveryRequired
	}
	return nil
}
