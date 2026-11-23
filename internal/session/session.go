// Package session tells what kind of terminal session gwiki runs in.
package session

// Remote reports an SSH session: the terminal is on another machine, so a
// program started here, such as a browser or a clipboard tool, acts on the
// wrong one.
func Remote(getenv func(string) string) bool {
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if getenv(name) != "" {
			return true
		}
	}
	return false
}
