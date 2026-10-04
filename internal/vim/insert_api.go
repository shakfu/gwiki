package vim

// ReplaceText puts text in place of the text between a and z and leaves the
// cursor after it, as the host does to complete a word. In insert mode it
// joins the insert's undo step; it calls the Changed hook like typing does.
func (e *Editor) ReplaceText(a, z Pos, text string) {
	e.recordReplace(a, z, text)
	e.change(func() { e.SetCursor(e.Buf.Replace(a, z, text)) })
}

// recordReplace makes "." repeat a completion as typed text: the ctrl-n or
// ctrl-p that asked for it becomes backspaces over the replaced text and the
// new text's runes. Only a replacement ending at the cursor on one line fits.
func (e *Editor) recordReplace(a, z Pos, text string) {
	n := len(e.seq)
	if !e.changing || e.replaying || n == 0 || e.Mode != Insert ||
		(e.seq[n-1] != "ctrl+n" && e.seq[n-1] != "ctrl+p") ||
		a.Line != z.Line || z != e.Cursor || a.Col > z.Col {
		return
	}
	// Backspace removes a whole indent step inside leading spaces.
	if a.Col < indentOf(e.Buf.Line(a.Line)) {
		return
	}
	keys := e.seq[:n-1]
	for range z.Col - a.Col {
		keys = append(keys, "backspace")
	}
	for _, r := range text {
		keys = append(keys, string(r)) // a single rune is typed as is
	}
	e.seq = keys
}
