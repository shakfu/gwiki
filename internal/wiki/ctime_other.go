//go:build !(darwin || freebsd || netbsd || linux || openbsd)

package wiki

import "os"

// changeTime is 0: this platform's file info has no inode change time.
func changeTime(os.FileInfo) int64 { return 0 }
