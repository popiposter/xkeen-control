//go:build !linux

package setup

import "os"

func MaintenanceFile() (*os.File, error) { return nil, ErrUnsupported }

func PanelInstallGuard() error { return ErrUnsupported }

func acquireLock(string, bool) (func(), error)         { return nil, ErrUnsupported }
func acquireExistingLock(string, bool) (func(), error) { return nil, ErrUnsupported }
func readReceipt(string) (*Receipt, error)             { return nil, ErrUnsupported }
func writeReceipt(string, Receipt) error               { return ErrUnsupported }
