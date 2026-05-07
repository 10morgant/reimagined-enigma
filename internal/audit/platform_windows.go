//go:build windows

package audit

import (
	"os"
	"syscall"
	"time"
)

func createdTime(fi os.FileInfo) time.Time {
	if data, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, data.CreationTime.Nanoseconds())
	}
	return fi.ModTime()
}

func fileOwner(_ os.FileInfo) string {
	return "Unknown (owner lookup unavailable)"
}
