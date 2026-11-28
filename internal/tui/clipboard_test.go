package tui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shakfu/gwiki/internal/vim"
)

func TestCopyTool(t *testing.T) {
	installed := func(names ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, n := range names {
				if n == name {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	all := installed("pbcopy", "wl-copy", "xclip", "xsel")
	for _, c := range []struct {
		name string
		goos string
		env  map[string]string
		look func(string) (string, error)
		want string // the tool's name; "" is OSC 52
	}{
		{"macOS", "darwin", nil, all, "pbcopy"},
		{"macOS over SSH", "darwin", map[string]string{"SSH_TTY": "/dev/ttys001"}, all, ""},
		{"Linux over SSH with X forwarded", "linux", map[string]string{"SSH_CONNECTION": "a b c d", "DISPLAY": "localhost:10"}, all, ""},
		{"Wayland", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, all, "wl-copy"},
		{"X11", "linux", map[string]string{"DISPLAY": ":0"}, all, "xclip"},
		{"X11 with only xsel", "freebsd", map[string]string{"DISPLAY": ":0"}, installed("xsel"), "xsel"},
		{"Wayland without wl-copy", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, installed("xclip"), ""},
		{"a console", "linux", nil, all, ""},
		{"Windows", "windows", nil, all, ""},
		{"macOS without pbcopy", "darwin", nil, installed(), ""},
	} {
		got := ""
		if tool := copyTool(c.goos, func(k string) string { return c.env[k] }, c.look); tool != nil {
			got = tool[0]
		}
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// script writes a stand-in clipboard tool.
func script(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in tools are /bin/sh scripts")
	}
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// slowMachine raises copyTimeout for one test: a stand-in tool started while
// the rest of the suite runs can take longer than the 2s a real tool gets.
func slowMachine(t *testing.T) {
	saved := copyTimeout
	copyTimeout = time.Minute
	t.Cleanup(func() { copyTimeout = saved })
}

func TestRunCopy(t *testing.T) {
	slowMachine(t)
	out := filepath.Join(t.TempDir(), "clip")
	if err := runCopy([]string{script(t, `cat > "$1"`), out}, "héllo\nworld"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != "héllo\nworld" {
		t.Errorf("the tool got %q", got)
	}

	err := runCopy([]string{script(t, `echo "Error: Can't open display" >&2; exit 1`)}, "x")
	if err == nil || !strings.Contains(err.Error(), "exit status 1") || !strings.Contains(err.Error(), "Can't open display") {
		t.Errorf("a failing tool: %v", err)
	}
	if err := runCopy([]string{filepath.Join(t.TempDir(), "missing")}, "x"); err == nil {
		t.Error("a missing tool reported no error")
	}
}

// xclip leaves a child holding stderr open; the copy returns when the tool
// exits, not when the child does.
func TestRunCopyDoesNotWaitForTheToolsChild(t *testing.T) {
	slowMachine(t)
	// Waiting for the child would take 5s; the bound leaves the tool 4s to
	// start on a loaded machine.
	tool := script(t, "cat > /dev/null; sleep 5 & exit 0")
	start := time.Now()
	if err := runCopy([]string{tool}, "x"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("took %v", d)
	}
}

// A copy that fails on release shows the error, not "copied".
func TestMouseCopyOnSelectShowsTheError(t *testing.T) {
	f := newWikiFixture(t)
	f.m.copy = func(string) error { return errors.New("pbcopy: exit status 1") }
	f.m.focus = focusContent
	x, y := f.find("write docs", wikiTreeWidth+1)
	f.click(x, y)
	f.drag(x+9, y)
	f.release(x+9, y)
	if !f.m.statusErr || f.m.status != "clipboard: pbcopy: exit status 1" || f.m.edit.ed.Mode != vim.Visual {
		t.Errorf("status %q, err %v, mode %v", f.m.status, f.m.statusErr, f.m.edit.ed.Mode)
	}
}
