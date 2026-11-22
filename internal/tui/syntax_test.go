package tui

import (
	"strings"
	"testing"

	"github.com/shakfu/gwiki/internal/syntax"
)

func TestWikiBufferClassesFencedCode(t *testing.T) {
	f := newWikiFixture(t)
	f.m.edit.ed.Load("# Code\n\n```go\nreturn 1\n```\n\nreturn 2\n\n```\nreturn 3\n```\n")
	code := f.m.codeSpans()
	if s := code[3]; len(s) == 0 || s[0] != (syntax.Span{From: 0, To: 6, Class: syntax.Keyword}) {
		t.Fatalf("line 4 spans %v, want return as a keyword", s)
	}
	for _, l := range []int{6, 9} {
		if len(code[l]) != 0 {
			t.Errorf("line %d, outside a go block, has spans %v", l+1, code[l])
		}
	}
}

func TestWikiBufferWarnsOnLargeCode(t *testing.T) {
	f := newWikiFixture(t)
	f.m.edit.ed.Load("```go\n" + strings.Repeat("x = 1\n", 12000) + "```\n")
	f.m.codeSpans()
	if !f.m.statusErr || !strings.Contains(f.m.status, "code at line 2") {
		t.Fatalf("status %q", f.m.status)
	}
}
