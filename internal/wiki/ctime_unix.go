//go:build linux || openbsd || dragonfly || solaris || illumos

package wiki

import (
	"io/fs"
	"syscall"
)

// changeTime is a file's inode change time in nanoseconds, or 0 if unknown.
func changeTime(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctim.Nano()
	}
	return 0
}
