package vim

import (
	"slices"
	"strings"
)

// historySize is how many lines a History keeps.
const historySize = 100

// History is the command and search lines entered, oldest first. Each line
// keeps its kind, ':', '/' or '?', as its first character.
type History struct {
	lines []string
}

// Add records a line, moving an earlier copy of it to the end, as vim does.
func (h *History) Add(kind rune, text string) {
	if text == "" {
		return
	}
	line := string(kind) + text
	h.lines = slices.DeleteFunc(h.lines, func(l string) bool { return l == line })
	h.lines = append(h.lines, line)
	if n := len(h.lines) - historySize; n > 0 {
		h.lines = h.lines[n:]
	}
}

// Recall walks a History for lines of one kind beginning with what was typed
// before the walk began. The zero Recall starts a new walk.
type Recall struct {
	on    bool
	at    int // index into the History's lines; len(lines) is the typed line
	typed string
}

// Step returns the next older line when back is set, else the next newer one.
// Past the newest it returns the typed line; past the oldest it keeps line.
func (r *Recall) Step(h *History, kind rune, line string, back bool) string {
	if !r.on {
		*r = Recall{on: true, at: len(h.lines), typed: line}
	}
	prefix := string(kind) + r.typed
	if back {
		for i := r.at - 1; i >= 0; i-- {
			if strings.HasPrefix(h.lines[i], prefix) {
				r.at = i
				return h.lines[i][len(string(kind)):]
			}
		}
		return line
	}
	for i := r.at + 1; i < len(h.lines); i++ {
		if strings.HasPrefix(h.lines[i], prefix) {
			r.at = i
			return h.lines[i][len(string(kind)):]
		}
	}
	r.at = len(h.lines)
	return r.typed
}
