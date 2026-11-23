//go:build darwin || freebsd || netbsd

package wiki

import (
	"io/fs"
	"syscall"
)

// changeTime is a file's inode change time in nanoseconds, or 0 if unknown.
func changeTime(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctimespec.Nano()
	}
	return 0
}
