package vim

import "testing"

// search runs a '/' search for each pattern.
func search(e *Editor, patterns ...string) {
	for _, p := range patterns {
		e.Key("/")
		e.Type(p)
		e.Key("enter")
	}
}

func cmdline(t *testing.T, e *Editor) string {
	t.Helper()
	line, ok := e.CommandLine()
	if !ok {
		t.Fatal("not on the command line")
	}
	return line
}

func TestHistoryWalksPastTheLastTwo(t *testing.T) {
	e := New("alpha beta gamma\n")
	search(e, "alpha", "beta", "gamma")
	e.Key("/")
	for _, want := range []string{"/gamma", "/beta", "/alpha", "/alpha"} {
		e.Key("up")
		if got := cmdline(t, e); got != want {
			t.Fatalf("up: %q, want %q", got, want)
		}
	}
	for _, want := range []string{"/beta", "/gamma", "/", "/"} {
		e.Key("down")
		if got := cmdline(t, e); got != want {
			t.Fatalf("down: %q, want %q", got, want)
		}
	}
}

func TestHistoryMatchesTypedPrefix(t *testing.T) {
	e := New("alpha beta gamma\n")
	search(e, "gamma", "beta", "gap")
	e.Key("/")
	e.Type("ga")
	for _, want := range []string{"/gap", "/gamma", "/gamma"} {
		e.Key("up")
		if got := cmdline(t, e); got != want {
			t.Fatalf("up: %q, want %q", got, want)
		}
	}
	e.Key("down")
	e.Key("down")
	if got := cmdline(t, e); got != "/ga" {
		t.Fatalf("down past the newest: %q, want the typed /ga", got)
	}
}

func TestHistoryKeepsKindsApart(t *testing.T) {
	e := New("alpha\n")
	search(e, "alpha")
	e.Keys(":")
	e.Key("up")
	if got := cmdline(t, e); got != ":" {
		t.Fatalf("':' recalled a search: %q", got)
	}
}

func TestHistoryMovesRepeatsToTheEnd(t *testing.T) {
	h := &History{}
	for _, s := range []string{"a", "b", "a"} {
		h.Add(':', s)
	}
	if len(h.lines) != 2 || h.lines[1] != ":a" {
		t.Fatalf("lines %q, want [:b :a]", h.lines)
	}
	for i := 0; i < historySize+5; i++ {
		h.Add(':', string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	if len(h.lines) != historySize {
		t.Fatalf("kept %d lines, want %d", len(h.lines), historySize)
	}
}

func TestHistorySharedBetweenEditors(t *testing.T) {
	a, b := New("x\n"), New("x\n")
	b.History = a.History
	search(a, "x")
	b.Key("/")
	b.Key("up")
	if got := cmdline(t, b); got != "/x" {
		t.Fatalf("second editor recalled %q, want /x", got)
	}
}

func TestScrollKeepsCursorOnScreen(t *testing.T) {
	e := New("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	e.Height = 3
	e.Scroll(4)
	if e.Top != 4 || e.Cursor.Line != 4 {
		t.Fatalf("down: top %d, cursor %d, want 4 and 4", e.Top, e.Cursor.Line)
	}
	e.Scroll(-3)
	if e.Top != 1 || e.Cursor.Line != 3 {
		t.Fatalf("up: top %d, cursor %d, want 1 and 3", e.Top, e.Cursor.Line)
	}
	e.Scroll(-10)
	if e.Top != 0 {
		t.Fatalf("top %d, want 0", e.Top)
	}
}
