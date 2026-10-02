package nodes

import (
	"os"
	"path/filepath"
)

// The intent fences panel node writers across process restarts. It does not
// serialize external native CLI/cron; that requires the native admission seam.
func acquireNodeIntent(previousDir string) (func() error, error) {
	if err := ensurePrivateDir(previousDir); err != nil {
		return nil, err
	}
	path := filepath.Join(previousDir, ".pending")
	pending, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, ErrNodeRecoveryRequired
	}
	_, err = pending.WriteString("node-operation-pending\n")
	if err == nil {
		err = pending.Sync()
	}
	closeErr := pending.Close()
	if err != nil || closeErr != nil || syncNodeDirectory(previousDir) != nil {
		return nil, ErrNodeRecoveryRequired
	}
	return func() error {
		if os.Remove(path) != nil || syncNodeDirectory(previousDir) != nil {
			return ErrNodeRecoveryRequired
		}
		return nil
	}, nil
}
