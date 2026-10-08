//go:build !linux

package setup

func PanelInstallGuard() error { return ErrUnsupported }

func acquireLock(string, bool) (func(), error) { return nil, ErrUnsupported }
func readReceipt(string) (*Receipt, error)     { return nil, ErrUnsupported }
func writeReceipt(string, Receipt) error       { return ErrUnsupported }
