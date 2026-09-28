//go:build !(linux || openbsd || dragonfly || solaris || illumos || darwin || freebsd || netbsd)

package wiki

import "io/fs"

// ctime is 0 where the platform has no inode change time; size and mtime
// alone then detect changes.
func ctime(fs.FileInfo) int64 { return 0 }
