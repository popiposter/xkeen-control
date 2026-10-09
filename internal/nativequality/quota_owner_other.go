//go:build !linux

package nativequality

import "os"

// Production quality scheduling is Linux-only.
func quotaOwnerOK(os.FileInfo) bool           { return false }
func acquireQuotaLock(string) (func(), error) { return nil, errQuota }
