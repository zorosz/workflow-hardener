//go:build windows

package hardener

import (
	"os"
	"syscall"
)

func linked(info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	attributes, ok := info.Sys().(*syscall.Win32FileAttributeData)
	// Fail closed if the platform's attributes cannot be inspected.
	return !ok || attributes == nil || attributes.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
