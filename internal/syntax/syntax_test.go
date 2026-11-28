package syntax

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// classesOf names each span's text and class on one line.
func classesOf(line string, spans []Span) map[string]Class {
	r := []rune(line)
	out := map[string]Class{}
	for _, s := range spans {
		out[string(r[s.From:s.To])] = s.Class
	}
	return out
}

func TestHighlightGo(t *testing.T) {
	code := "// Package x.\npackage x\n\nfunc f() int { return 42 + len(\"héllo\") }"
	lines, err := Highlight("go", code)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4", len(lines))
	}
	if got := classesOf("// Package x.", lines[0]); got["// Package x."] != Comment {
		t.Errorf("line 1: %v", got)
	}
	got := classesOf("func f() int { return 42 + len(\"héllo\") }", lines[3])
	for text, want := range map[string]Class{"func": Keyword, "f": Function, "int": Type, "return": Keyword, "42": Number, `"héllo"`: String, "len": Type} {
		if got[text] != want {
			t.Errorf("%q: class %d, want %d (all: %v)", text, got[text], want, got)
		}
	}
}

// A token spanning lines, a block comment here, is split at each newline
// into spans in each line's own columns.
func TestHighlightSplitsMultilineTokens(t *testing.T) {
	lines, err := Highlight("c", "/* one\n  two */ int x;")
	if err != nil {
		t.Fatal(err)
	}
	if want := []Span{{0, 6, Comment}}; len(lines[0]) != 1 || lines[0][0] != want[0] {
		t.Errorf("line 1 spans %v, want %v", lines[0], want)
	}
	if len(lines[1]) == 0 || lines[1][0] != (Span{0, 8, Comment}) {
		t.Errorf("line 2 spans %v, want a comment at 0..8 first", lines[1])
	}
}

func TestHighlightByAliasAndExtension(t *testing.T) {
	for _, lang := range []string{"python", "py", "sh", "Go", "yaml"} {
		lines, err := Highlight(lang, "x = 1")
		if err != nil || lines == nil {
			t.Errorf("%s: lines %v, err %v", lang, lines, err)
		}
	}
}

func TestHighlightUnknownIsPlain(t *testing.T) {
	for _, lang := range []string{"", "  ", "no-such-language-xyz"} {
		lines, err := Highlight(lang, "anything")
		if lines != nil || err != nil {
			t.Errorf("%q: lines %v, err %v; want nil, nil", lang, lines, err)
		}
	}
}

func TestHighlightCapsSize(t *testing.T) {
	_, err := Highlight("go", strings.Repeat("x", maxBytes+1))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err %v, want ErrTooLarge", err)
	}
}

// fakeLex replaces the lexer for one test. The fake blocks until release is
// closed or it is told to stop, and counts its runs.
func fakeLex(t *testing.T) (release chan struct{}, runs *atomic.Int32) {
	t.Helper()
	release, runs = make(chan struct{}), &atomic.Int32{}
	old, oldBudget := lex, budget
	lex = func(_, _ string, stop func() bool) ([][]Span, error) {
		runs.Add(1)
		for {
			select {
			case <-release:
				return [][]Span{{{0, 1, Keyword}}}, nil
			case <-time.After(time.Millisecond):
				if stop() {
					return nil, ErrTooSlow
				}
			}
		}
	}
	budget = 20 * time.Millisecond
	t.Cleanup(func() {
		lex, budget = old, oldBudget
		mu.Lock()
		clear(cache)
		held = 0
		mu.Unlock()
	})
	return release, runs
}

// A block that outlasts the budget is plain and cached as such, so drawing it
// again does not lex it again; while its lex is still running, other blocks
// are plain at once; once it stops, they are lexed.
func TestHighlightGivesUpAndRecovers(t *testing.T) {
	release, runs := fakeLex(t)
	if _, err := Highlight("go", "slow"); !errors.Is(err, ErrTooSlow) {
		t.Fatalf("err %v, want ErrTooSlow", err)
	}
	if _, err := Highlight("go", "slow"); !errors.Is(err, ErrTooSlow) || runs.Load() != 1 {
		t.Fatalf("again: err %v after %d runs, want a cached ErrTooSlow", err, runs.Load())
	}
	// The fake stops within a millisecond of being abandoned.
	for deadline := time.Now().Add(time.Second); stale.Load() > 0; {
		if time.Now().After(deadline) {
			t.Fatal("the abandoned lex never stopped")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	lines, err := Highlight("go", "fast")
	if err != nil || len(lines) != 1 {
		t.Fatalf("after: lines %v, err %v", lines, err)
	}
}

// While an abandoned lex runs, Highlight returns ErrBusy without waiting, and
// does not cache it.
func TestHighlightBusyIsNotCached(t *testing.T) {
	release, _ := fakeLex(t)
	stale.Add(1)
	start := time.Now()
	_, err := Highlight("go", "x")
	stale.Add(-1)
	if !errors.Is(err, ErrBusy) || time.Since(start) > budget/2 {
		t.Fatalf("err %v after %v, want ErrBusy at once", err, time.Since(start))
	}
	close(release)
	if _, err := Highlight("go", "x"); err != nil {
		t.Fatalf("after: err %v, want the block lexed", err)
	}
}

// The cache is emptied once the spans it holds pass cacheBytes.
func TestHighlightCacheIsBounded(t *testing.T) {
	fakeLex(t)
	big := make([][]Span, 1)
	big[0] = make([]Span, 10000)
	lex = func(_, _ string, _ func() bool) ([][]Span, error) { return big, nil }
	for i := 0; i < 100; i++ {
		if _, err := Highlight("go", fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		h, n := held, len(cache)
		mu.Unlock()
		if h > cacheBytes || n == 0 {
			t.Fatalf("held %d bytes in %d entries, cap %d", h, n, cacheBytes)
		}
	}
}

func TestQuartoCellLanguage(t *testing.T) {
	for info, want := range map[string]string{"go": "go", " python ": "python", "{python}": "python", "{r, echo=FALSE}": "r", "{r,": "r", "{}": "", "{.python}": "python", "{.python .numberLines}": "python", ".hidden": ".hidden"} {
		if got := language(info); got != want {
			t.Errorf("language(%q) = %q, want %q", info, got, want)
		}
	}
	if spans, err := Highlight("{python}", "print(1)"); err != nil || spans == nil {
		t.Errorf("Highlight({python}) = %v, %v", spans, err)
	}
}
