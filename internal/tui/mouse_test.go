package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/shakfu/gwiki/internal/vim"
)

// find draws the screen and returns the cell where text first shows, in the
// columns from x on.
func (f *wikiFixture) find(text string, fromX int) (int, int) {
	f.t.Helper()
	for y, line := range strings.Split(f.view(), "\n") {
		for i := strings.Index(line, text); i >= 0; {
			if x := ansi.StringWidth(line[:i]); x >= fromX {
				return x, y
			}
			next := strings.Index(line[i+1:], text)
			if next < 0 {
				break
			}
			i += 1 + next
		}
	}
	f.t.Fatalf("screen lacks %q from column %d:\n%s", text, fromX, f.view())
	return 0, 0
}

func (f *wikiFixture) mouse(x, y int, b tea.MouseButton) tea.Cmd {
	_, cmd := f.m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b})
	return cmd
}

func (f *wikiFixture) click(x, y int) { f.mouse(x, y, tea.MouseButtonLeft) }

func TestMouseTreeOpensAndFolds(t *testing.T) {
	f := newWikiFixture(t)
	f.m.focus = focusTree
	x, y := f.find("Grammar", 0)
	f.click(x, y)
	if f.page() != "lexer/grammar" || f.m.focus != focusContent {
		t.Fatalf("page %q, focus %v", f.page(), f.m.focus)
	}
	x, y = f.find("▾ lexer/", 0)
	f.click(x, y)
	if !f.m.collapsed["lexer"] {
		t.Fatal("a click on the marker did not fold lexer/")
	}
}

// The first click on a link puts the cursor on it; a second follows it.
func TestMouseBufferFollowsOnSecondClick(t *testing.T) {
	f := newWikiFixture(t)
	f.m.focus = focusContent
	x, y := f.find("Design sketch]]", wikiTreeWidth+1)
	f.click(x+2, y)
	if f.page() != "index" {
		t.Fatalf("the first click opened %q", f.page())
	}
	if l, ok := f.m.linkAtCursor(); !ok || l.Target != "Design sketch" {
		t.Fatalf("cursor at %+v, not on the link", f.m.edit.ed.Cursor)
	}
	f.click(x+4, y)
	if f.page() != "lexer/design-sketch" {
		t.Fatalf("the second click left the page on %q", f.page())
	}
}

func TestMouseBufferClickPlacesCursor(t *testing.T) {
	f := newWikiFixture(t)
	f.m.focus = focusTree
	x, y := f.find("write docs", wikiTreeWidth+1)
	f.click(x, y)
	line := f.m.edit.ed.Buf.LineString(f.m.edit.ed.Cursor.Line)
	if f.m.focus != focusContent || !strings.HasPrefix(string([]rune(line)[f.m.edit.ed.Cursor.Col:]), "write docs") {
		t.Fatalf("focus %v, cursor %+v on %q", f.m.focus, f.m.edit.ed.Cursor, line)
	}
}

func TestMouseWheelScrollsPane(t *testing.T) {
	f := newWikiFixture(t)
	if err := f.m.open("lexer/grammar"); err != nil {
		t.Fatal(err)
	}
	f.view()
	f.mouse(wikiTreeWidth+10, 5, tea.MouseButtonWheelDown)
	if f.m.edit.ed.Top != wheelStep {
		t.Fatalf("top %d after a notch, want %d", f.m.edit.ed.Top, wheelStep)
	}
	f.mouse(wikiTreeWidth+10, 5, tea.MouseButtonWheelUp)
	if f.m.edit.ed.Top != 0 {
		t.Fatalf("top %d after a notch back, want 0", f.m.edit.ed.Top)
	}
}

func TestMouseListAndTabs(t *testing.T) {
	f := newWikiFixture(t)
	f.m.showList(screenBroken)
	_, y := f.find("Missing page", 0)
	f.click(2, y)
	if f.m.screen != screenRead || f.page() != "index" {
		t.Fatalf("screen %v, page %q", f.m.screen, f.page())
	}

	f.m.showTab(screenLatest)
	x, y := f.find(" tasks ", 0)
	if y != 0 {
		t.Fatalf("tab found on row %d", y)
	}
	f.click(x+1, y)
	if f.m.screen != screenTasks {
		t.Fatalf("screen %v after clicking the tasks tab", f.m.screen)
	}
}

// A repair writes, so a click only selects it.
func TestMouseRepairOnlySelects(t *testing.T) {
	f := newWikiFixture(t)
	f.m.showList(screenBroken)
	f.press("f")
	if f.m.screen != screenOffers || len(f.m.offers) == 0 {
		t.Skipf("no repairs offered: screen %v", f.m.screen)
	}
	before := f.source("index")
	_, y := f.find("REPAIR", 0)
	f.click(2, y+1)
	if f.m.screen != screenOffers || f.source("index") != before {
		t.Fatal("a click applied a repair")
	}
}

func TestMouseIgnoredUnderPromptAndToggled(t *testing.T) {
	f := newWikiFixture(t)
	f.m.focus = focusTree
	f.press(":")
	x, y := f.find("Grammar", 0)
	f.click(x, y)
	if f.page() != "index" {
		t.Fatalf("a click under the prompt opened %q", f.page())
	}
	if cmd := f.press("mouse", "enter"); cmd == nil || f.m.mouseOn {
		t.Fatalf(":mouse: on %v, cmd %v", f.m.mouseOn, cmd)
	}
	if cmd := f.press(":", "mouse", "enter"); cmd == nil || !f.m.mouseOn {
		t.Fatalf(":mouse again: on %v, cmd %v", f.m.mouseOn, cmd)
	}
}

func (f *wikiFixture) drag(x, y int) {
	f.m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
}

func (f *wikiFixture) release(x, y int) {
	f.m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
}

// A drag selects from where the button went down to the pointer, in visual
// mode, and "+y copies what it selected.
func TestMouseDragSelects(t *testing.T) {
	f := newWikiFixture(t)
	off := false
	f.w.Config.CopyOnSelect = &off // the yank, not the release, must copy
	var copied string
	f.m.copy = func(text string) error { copied = text; return nil }
	f.m.focus = focusContent
	x, y := f.find("write docs", wikiTreeWidth+1)
	f.click(x, y)
	f.drag(x+4, y)
	f.drag(x+9, y)
	f.release(x+9, y)
	if f.m.edit.ed.Mode != vim.Visual {
		t.Fatalf("mode %v after a drag", f.m.edit.ed.Mode)
	}
	f.press("\"+y")
	if copied != "write docs" {
		t.Fatalf("copied %q", copied)
	}
	// After the release, motion selects nothing.
	f.drag(x, y)
	if f.m.edit.ed.Mode != vim.Normal {
		t.Fatalf("mode %v after motion with the button up", f.m.edit.ed.Mode)
	}
}

// A drag that leaves the pane keeps selecting, to its first or last row; one
// that stays on a character selects nothing; a click ends a selection.
func TestMouseDragEdgesAndClicks(t *testing.T) {
	f := newWikiFixture(t)
	f.m.focus = focusContent
	x, y := f.find("Start at", wikiTreeWidth+1)
	f.click(x, y)
	f.drag(x, y)
	if f.m.edit.ed.Mode != vim.Normal {
		t.Fatalf("mode %v after a drag within one character", f.m.edit.ed.Mode)
	}
	f.drag(x, 1000)
	a, z, ok := f.m.edit.ed.Selection()
	last := len(f.m.edit.rows) - 1
	if !ok || a.Line != 2 || z.Line != f.m.edit.rows[last].line {
		t.Fatalf("selection %+v..%+v (%v), want line 2 to the last drawn", a, z, ok)
	}
	f.drag(-5, -5)
	if a, _, _ := f.m.edit.ed.Selection(); a.Line != f.m.edit.rows[0].line {
		t.Fatalf("selection starts at %+v, want the first drawn row", a)
	}
	f.release(0, 0)
	f.click(x, y)
	if f.m.edit.ed.Mode != vim.Normal || f.m.edit.ed.Cursor.Line != 2 {
		t.Fatalf("mode %v at %+v after a click", f.m.edit.ed.Mode, f.m.edit.ed.Cursor)
	}
}

// A drag begun in insert mode selects nothing: it would end the insertion.
func TestMouseDragInInsertMode(t *testing.T) {
	f := newWikiFixture(t)
	f.m.focus = focusContent
	f.press("i")
	x, y := f.find("Start at", wikiTreeWidth+1)
	f.click(x, y)
	f.drag(x+5, y)
	if f.m.edit.ed.Mode != vim.Insert {
		t.Fatalf("mode %v", f.m.edit.ed.Mode)
	}
}

// By default a drag then a plain y copies; with "clipboard": "" it does not.
func TestMouseDragYankUnnamedplus(t *testing.T) {
	for _, clipboard := range []string{"default", ""} {
		f := newWikiFixture(t)
		off := false
		f.w.Config.CopyOnSelect = &off // the yank, not the release, must copy
		var copied string
		f.m.copy = func(text string) error { copied = text; return nil }
		want := "write docs"
		if clipboard != "default" {
			f.w.Config.Clipboard, want = &clipboard, ""
		}
		f.m.openBuffer("index", []byte(f.m.edit.ed.Buf.Text()), f.m.edit.base)
		f.m.focus = focusContent
		f.m.View()
		x, y := f.find("write docs", wikiTreeWidth+1)
		f.click(x, y)
		f.drag(x+9, y)
		f.release(x+9, y)
		f.press("y")
		if copied != want {
			t.Fatalf("clipboard %q: copied %q, want %q", clipboard, copied, want)
		}
	}
}

// Capture is on at start unless the config sets "mouse": false.
func TestMouseConfigStartsOn(t *testing.T) {
	f := newWikiFixture(t)
	if !f.m.mouseOn {
		t.Fatal("capture off by default")
	}
	off := false
	f.w.Config.Mouse = &off
	m, err := NewWiki(f.w)
	if err != nil {
		t.Fatal(err)
	}
	if m.mouseOn {
		t.Fatal(`capture on with "mouse": false`)
	}
}

// By default letting go of a drag copies its selection and keeps it; a click
// copies nothing, and with copy_on_select false a drag copies nothing.
func TestMouseCopyOnSelect(t *testing.T) {
	for _, on := range []bool{true, false} {
		f := newWikiFixture(t)
		copied := "unset"
		f.m.copy = func(text string) error { copied = text; return nil }
		if !on {
			f.w.Config.CopyOnSelect = &on
		}
		f.m.focus = focusContent
		x, y := f.find("write docs", wikiTreeWidth+1)
		f.click(x, y)
		f.release(x, y)
		if copied != "unset" {
			t.Fatalf("on=%v: a click copied %q", on, copied)
		}
		f.click(x, y)
		f.drag(x+9, y)
		f.release(x+9, y)
		want := "unset"
		if on {
			want = "write docs"
		}
		if copied != want || f.m.edit.ed.Mode != vim.Visual {
			t.Fatalf("on=%v: copied %q, mode %v", on, copied, f.m.edit.ed.Mode)
		}
	}
}
