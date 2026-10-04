// Package display makes stored text safe to print to a terminal.
//
// Titles, bodies, tags and names arrive from other people's logs through git,
// so they are untrusted input. Printed raw, an escape sequence in a title can
// clear the screen, retitle the window, or write the clipboard through OSC 52,
// and a newline can forge rows in a table.
package display

import "strings"

// Line returns s for display on a single line. Tabs become spaces; every other
// character Control names, newlines included, becomes '?'.
func Line(s string) string {
	if clean(s, false) {
		return s
	}
	return strings.Map(func(r rune) rune { return replace(r, false) }, s)
}

// Block returns s for display over several lines. Newlines and tabs are kept;
// every other character Control names becomes '?'.
func Block(s string) string {
	if clean(s, true) {
		return s
	}
	return strings.Map(func(r rune) rune { return replace(r, true) }, s)
}

// Control reports whether r must not reach a terminal as is: a C0 or C1
// control character, DEL, a bidirectional embedding, override or isolate, or
// an invisible space. ZWJ and ZWNJ are kept; scripts and emoji need them.
func Control(r rune) bool {
	switch {
	case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true // reorders the text around it
	case r == 0x200b, r == 0x2060, r == 0xfeff:
		return true // zero-width space, word joiner, BOM
	}
	return false
}

func replace(r rune, multiline bool) rune {
	switch {
	case multiline && (r == '\n' || r == '\t'):
		return r
	case r == '\t':
		return ' '
	case Control(r):
		return '?'
	}
	return r
}

// clean reports whether s needs no replacement, so the common case allocates
// nothing.
func clean(s string, multiline bool) bool {
	for _, r := range s {
		if replace(r, multiline) != r {
			return false
		}
	}
	return true
}
