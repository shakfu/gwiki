//go:build darwin || freebsd || netbsd

package wiki

import (
	"os"
	"syscall"
)

// changeTime is a file's inode change time in nanoseconds, or 0 where the
// platform does not report one.
func changeTime(info os.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctimespec.Nano()
	}
	return 0
}
