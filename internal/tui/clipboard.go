package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/shakfu/gwiki/internal/session"
)

// copyTool is the program that sets this machine's clipboard, or nil to send
// OSC 52, which asks the terminal to set it. OSC 52 is the only way from an
// SSH session, where a tool would set the far machine's clipboard, and it is
// used on Windows, whose clip.exe garbles text that is not ASCII. Elsewhere a
// tool is preferred: Terminal.app ignores OSC 52, and iTerm2 and tmux drop it
// unless configured to allow it, with no way for gwiki to tell.
func copyTool(goos string, getenv func(string) string, look func(string) (string, error)) []string {
	if session.Remote(getenv) {
		return nil
	}
	var tools [][]string
	switch goos {
	case "darwin":
		tools = [][]string{{"pbcopy"}}
	case "windows":
	default:
		if getenv("WAYLAND_DISPLAY") != "" {
			tools = append(tools, []string{"wl-copy"})
		}
		if getenv("DISPLAY") != "" {
			tools = append(tools, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
		}
	}
	for _, t := range tools {
		if _, err := look(t[0]); err == nil {
			return t
		}
	}
	return nil
}

// copyTimeout bounds a clipboard tool, which should return at once.
const copyTimeout = 2 * time.Second

// systemCopy returns the clipboard setter for this machine: a tool when
// copyTool finds one, else osc52.
func systemCopy(getenv func(string) string, osc52 func(string)) func(string) error {
	tool := copyTool(runtime.GOOS, getenv, exec.LookPath)
	if tool == nil {
		return func(text string) error { osc52(text); return nil }
	}
	return func(text string) error { return runCopy(tool, text) }
}

// runCopy feeds text to a clipboard tool.
func runCopy(tool []string, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool[0], tool[1:]...)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// xclip and wl-copy leave a child serving the clipboard, holding stderr
	// open; once the tool itself exits, stop waiting for it.
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Run(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s: %w: %s", tool[0], err, msg)
		}
		return fmt.Errorf("%s: %w", tool[0], err)
	}
	return nil
}
