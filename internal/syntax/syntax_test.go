package syntax

import (
	"errors"
	"strings"
	"testing"
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
