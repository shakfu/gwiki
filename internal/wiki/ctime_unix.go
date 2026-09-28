//go:build linux || openbsd || dragonfly || solaris || illumos

package wiki

import (
	"io/fs"
	"syscall"
)

// ctime is a file's inode change time in nanoseconds, or 0 if unknown.
func ctime(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctim.Nano()
	}
	return 0
}
