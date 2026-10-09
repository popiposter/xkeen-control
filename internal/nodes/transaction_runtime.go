package nodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"path/filepath"

	"github.com/popiposter/xkeen-control/internal/xkeen"
)

func (a CommandActivator) RuntimeIdentity(ctx context.Context) (string, error) {
	binary := a.XrayBinary
	if binary == "" {
		binary = "xray"
	}
	if !filepath.IsAbs(binary) {
		var err error
		binary, err = exec.LookPath(binary)
		if err != nil {
			return "", ErrNodeRecoveryRequired
		}
	}
	dir := a.ConfigDir
	if dir == "" {
		dir = filepath.Dir(a.ActiveOutboundsPath)
	}
	editor := &xkeen.ConfigEditor{XrayBinary: binary, Dir: dir}
	return readLiveRuntimeIdentity(ctx, editor)
}

func readLiveRuntimeIdentity(ctx context.Context, editor *xkeen.ConfigEditor) (string, error) {
	before, err := editor.ReadConfigProcess(ctx)
	if err != nil {
		return "", ErrNodeRecoveryRequired
	}
	after, err := editor.ReadConfigProcess(ctx)
	if err != nil || before != after {
		return "", ErrNodeRecoveryRequired
	}
	if after == "" {
		return "stopped", nil
	}
	digest := sha256.Sum256([]byte(after))
	return hex.EncodeToString(digest[:]), nil
}

type activatorRuntime struct{ Activator }

func (r activatorRuntime) Snapshot(ctx context.Context) (string, error) {
	return r.RuntimeIdentity(ctx)
}
