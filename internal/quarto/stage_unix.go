//go:build unix

package quarto

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

func userID() string { return strconv.Itoa(os.Getuid()) }

// private refuses a directory another user could have made or can enter, as
// one planted in a shared /tmp would be.
func private(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(st.Uid) != os.Getuid() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is not a directory only this user can enter; remove it", dir)
	}
	return nil
}
