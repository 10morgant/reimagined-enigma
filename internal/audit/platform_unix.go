//go:build !windows

package audit

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func createdTime(fi os.FileInfo) time.Time {
	// Portable creation time is not reliably available on Unix-like systems.
	return fi.ModTime()
}

func fileOwner(fi os.FileInfo) string {
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "Unknown (owner lookup unavailable)"
	}

	uid := strconv.FormatUint(uint64(stat.Uid), 10)
	u, err := user.LookupId(uid)
	if err != nil || strings.TrimSpace(u.Username) == "" {
		return fmt.Sprintf("UID:%s", uid)
	}
	return u.Username
}
