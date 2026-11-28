//go:build !unix

package quarto

import (
	"fmt"
	"os"
)

// userID names the staging directory; the temporary directory is already the
// user's own on Windows.
func userID() string { return "user" }

func private(dir string) error {
	if info, err := os.Lstat(dir); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("%s is not a directory; remove it", dir)
	}
	return nil
}
