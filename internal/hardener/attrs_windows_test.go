//go:build windows

package hardener

import (
	"os"
	"syscall"
	"testing"
	"time"
)

type attributeInfo struct {
	attributes any
	mode       os.FileMode
}

func (i attributeInfo) Name() string       { return "sample" }
func (i attributeInfo) Size() int64        { return 0 }
func (i attributeInfo) Mode() os.FileMode  { return i.mode }
func (i attributeInfo) ModTime() time.Time { return time.Time{} }
func (i attributeInfo) IsDir() bool        { return i.mode.IsDir() }
func (i attributeInfo) Sys() any           { return i.attributes }

func TestWindowsReparseAttributes(t *testing.T) {
	for _, tc := range []struct {
		name string
		info attributeInfo
		want bool
	}{
		{"ordinary", attributeInfo{attributes: &syscall.Win32FileAttributeData{}}, false},
		{"reparse", attributeInfo{attributes: &syscall.Win32FileAttributeData{FileAttributes: syscall.FILE_ATTRIBUTE_REPARSE_POINT}}, true},
		{"symlink", attributeInfo{mode: os.ModeSymlink}, true},
		{"unavailable attributes", attributeInfo{}, true},
		{"nil attributes", attributeInfo{attributes: (*syscall.Win32FileAttributeData)(nil)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := linked(tc.info); got != tc.want {
				t.Fatalf("linked=%v, want %v", got, tc.want)
			}
		})
	}
}
