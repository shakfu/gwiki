package vim

// ReplaceText puts text in place of the text between a and z and leaves the
// cursor after it, as the host does to complete a word. In insert mode it
// joins the insert's undo step; it calls the Changed hook like typing does.
func (e *Editor) ReplaceText(a, z Pos, text string) {
	e.change(func() { e.SetCursor(e.Buf.Replace(a, z, text)) })
}
