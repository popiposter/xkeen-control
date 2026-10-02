//go:build linux

package authority

import (
	"errors"
	"github.com/popiposter/xkeen-control/internal/authority/nativegate"
	"os"
)

type nativeOwner struct {
	*nativegate.Lease
	root string
}

func acquireNative(root string) (nativeClaim, error) {
	lease, err := nativegate.Acquire(root, nativegate.ConfigChange)
	if errors.Is(err, nativegate.ErrBusy) {
		return nil, ErrBusy
	}
	if err != nil {
		return nil, ErrNativeUnavailable
	}
	return &nativeOwner{Lease: lease, root: root}, nil
}

func (owner *nativeOwner) Verify() error {
	_, err := nativegate.Join(owner.root, owner.Token())
	return err
}

func (owner *nativeOwner) BindNodeIntent(created *os.File) (NodeIntentBinding, error) {
	return owner.Lease.BindNodeIntent(created)
}
