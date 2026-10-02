//go:build !linux

package xkeen

import "os"

func attachmentDirectoryOwned(os.FileInfo) bool { return true }

// Production attachment is Linux-only; host fixtures do not qualify durability.
func syncAttachmentDirectory(string) error { return nil }
