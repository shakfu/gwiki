package tui

import "testing"

// The ':' prompt and every page's buffer share one command history, which
// survives opening another page.
func TestWikiCommandHistoryIsShared(t *testing.T) {
	f := newWikiFixture(t)
	f.m.focus = focusTree
	f.press(":", "tasks", "enter")
	if f.m.screen != screenTasks {
		t.Fatalf("screen %v after :tasks", f.m.screen)
	}
	f.press(":", "up")
	if got := f.m.input.String(); got != "tasks" {
		t.Fatalf("prompt recalled %q, want tasks", got)
	}
	f.press("esc")

	if err := f.m.open("lexer/grammar"); err != nil {
		t.Fatal(err)
	}
	f.m.focus = focusContent
	f.press(":", "up")
	if got, _ := f.m.edit.ed.CommandLine(); got != ":tasks" {
		t.Fatalf("buffer recalled %q, want :tasks", got)
	}
	f.press("esc")

	// A line entered in the buffer reaches the prompt.
	f.press(":", "broken", "enter")
	f.m.focus = focusTree
	f.press(":", "up")
	if got := f.m.input.String(); got != "broken" {
		t.Fatalf("prompt recalled %q, want broken", got)
	}
	f.press("up")
	if got := f.m.input.String(); got != "tasks" {
		t.Fatalf("second up recalled %q, want tasks", got)
	}
	f.press("down", "down")
	if got := f.m.input.String(); got != "" {
		t.Fatalf("down past the newest left %q, want the empty typed line", got)
	}
}
