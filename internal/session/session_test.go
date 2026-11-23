package session

import "testing"

func TestRemote(t *testing.T) {
	for env, want := range map[string]bool{"": false, "SSH_CONNECTION": true, "SSH_CLIENT": true, "SSH_TTY": true, "DISPLAY": false} {
		got := Remote(func(k string) string {
			if k == env {
				return "x"
			}
			return ""
		})
		if got != want {
			t.Errorf("%q set: %v, want %v", env, got, want)
		}
	}
}
