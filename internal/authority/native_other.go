//go:build !linux

package authority

func acquireNative(string) (nativeClaim, error) { return nil, ErrNativeUnavailable }
