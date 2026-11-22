// Package syntax splits fenced code into classed spans for the reader and the
// editor to colour.
//
// Lexing is chroma's. Page text is untrusted and chroma's regexps backtrack,
// each match bounded only by a 250 ms timeout, so a block is lexed within a
// size cap and a time budget, and results are cached by language and text.
package syntax

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Class is the kind of a span, which picks its colour.
type Class uint8

const (
	Plain Class = iota
	Keyword
	Type
	Function
	String
	Number
	Comment
)

// Span is a classed run of one line, in rune columns.
type Span struct {
	From, To int
	Class    Class
}

const (
	maxBytes = 64 << 10
	budget   = 50 * time.Millisecond
)

// ErrTooLarge and ErrTooSlow report a block left partly or wholly plain.
var (
	ErrTooLarge = fmt.Errorf("over %d KiB, not highlighted", maxBytes>>10)
	ErrTooSlow  = fmt.Errorf("took over %v, highlighted in part", budget)
)

type result struct {
	lines [][]Span
	err   error
}

var (
	mu    sync.Mutex
	cache = map[string]result{}
)

const cacheSize = 256

// Highlight returns the spans of each line of code, split on '\n', for a
// fenced block's language. An unknown or empty language gives nil and no
// error. An error comes with whatever spans were found before it.
func Highlight(lang, code string) ([][]Span, error) {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return nil, nil
	}
	key := lang + "\x00" + code
	mu.Lock()
	r, ok := cache[key]
	mu.Unlock()
	if ok {
		return r.lines, r.err
	}
	r.lines, r.err = highlight(lang, code)
	mu.Lock()
	if len(cache) >= cacheSize {
		clear(cache)
	}
	cache[key] = r
	mu.Unlock()
	return r.lines, r.err
}

func highlight(lang, code string) ([][]Span, error) {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return nil, nil
	}
	if len(code) > maxBytes {
		return nil, ErrTooLarge
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", lang, err)
	}
	lines := make([][]Span, strings.Count(code, "\n")+1)
	line, col := 0, 0
	start := time.Now()
	for tok := it(); tok != chroma.EOF; tok = it() {
		if time.Since(start) > budget {
			return lines, ErrTooSlow
		}
		class := classOf(tok.Type)
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				line, col = line+1, 0
			}
			n := len([]rune(part))
			if class != Plain && n > 0 && line < len(lines) {
				lines[line] = append(lines[line], Span{col, col + n, class})
			}
			col += n
		}
	}
	return lines, nil
}

func classOf(t chroma.TokenType) Class {
	switch {
	case t == chroma.CommentPreproc || t == chroma.CommentPreprocFile || t == chroma.NameDecorator || t == chroma.NameTag:
		return Keyword
	case t.InCategory(chroma.Comment):
		return Comment
	case t == chroma.KeywordType || t == chroma.NameBuiltin || t == chroma.NameClass || t == chroma.NameAttribute:
		return Type
	case t.InCategory(chroma.Keyword):
		return Keyword
	case t == chroma.NameFunction || t == chroma.NameFunctionMagic:
		return Function
	case t.InSubCategory(chroma.LiteralString):
		return String
	case t.InSubCategory(chroma.LiteralNumber):
		return Number
	}
	return Plain
}
