package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/shakfu/gwiki/internal/display"
	"github.com/shakfu/gwiki/internal/vim"
)

// zone is a rectangle of the last frame and what a click or the wheel does
// there. View records the zones as it draws, so hit testing uses the layout
// the user saw. Handlers get the position relative to the zone.
type zone struct {
	x, y, w, h int
	click      func(x, y int)
	wheel      func(by int)
	drag       func(x, y int) // the button held down, maybe outside the zone
	drop       func()         // the button let go after a press here
}

// wheelStep is the rows one wheel notch moves.
const wheelStep = 3

// gutter is the line-number column the buffer draws before each row.
const gutter = 5

// addZone records a zone; either handler may be nil.
func (m *WikiModel) addZone(x, y, w, h int, click func(x, y int), wheel func(by int)) {
	m.add(zone{x: x, y: y, w: w, h: h, click: click, wheel: wheel})
}

func (m *WikiModel) add(z zone) {
	if z.w > 0 && z.h > 0 {
		m.zones = append(m.zones, z)
	}
}

// mouse handles a click or a wheel notch on the zone under the pointer, and a
// drag on the zone the button went down in. A prompt keeps the keyboard's
// focus, so the mouse does nothing while one is open.
func (m *WikiModel) mouse(msg tea.MouseMsg) {
	if m.prompt != nil || msg.Action == tea.MouseActionRelease {
		if h := m.held; h != nil && h.drop != nil && m.prompt == nil {
			h.drop()
		}
		m.held = nil
		return
	}
	if msg.Action == tea.MouseActionMotion {
		if h := m.held; h != nil && msg.Button == tea.MouseButtonLeft {
			h.drag(msg.X-h.x, msg.Y-h.y)
		}
		return
	}
	var by int
	switch msg.Button {
	case tea.MouseButtonLeft:
	case tea.MouseButtonWheelUp:
		by = -wheelStep
	case tea.MouseButtonWheelDown:
		by = wheelStep
	default:
		return
	}
	m.ctrlC, m.held = false, nil
	for i := len(m.zones) - 1; i >= 0; i-- {
		z := m.zones[i]
		if msg.X < z.x || msg.X >= z.x+z.w || msg.Y < z.y || msg.Y >= z.y+z.h {
			continue
		}
		switch {
		case by == 0 && z.click != nil:
			z.click(msg.X-z.x, msg.Y-z.y)
			if z.drag != nil {
				m.held = &z
			}
		case by != 0 && z.wheel != nil:
			z.wheel(by)
		default:
			continue
		}
		return
	}
}

// setMouse turns mouse capture on or off. Off gives the terminal back its own
// text selection.
func (m *WikiModel) setMouse(on bool) {
	m.mouseOn = on
	if on {
		m.after = tea.EnableMouseCellMotion
		m.setStatus("mouse on")
		return
	}
	m.after = tea.DisableMouse
	m.setStatus("mouse off; the terminal selects text")
}

// bufRow is a drawn row of the buffer: a line and the rune columns it shows.
type bufRow struct {
	line, from, to int
}

// clickBuffer puts the cursor where the page was clicked, ending a visual
// selection as vim does. A click on the link already under the cursor follows
// it, so the first click shows where a link goes and the second goes there.
func (m *WikiModel) clickBuffer(x, y int) {
	e := m.edit
	wasFocused := m.focus == focusContent
	m.focus = focusContent
	e.dragFrom = nil
	if e.preview || e.ed.Mode == vim.Command || y >= len(e.rows) {
		return
	}
	if e.ed.Mode == vim.Visual || e.ed.Mode == vim.VisualLine {
		e.ed.Key("esc")
	}
	before, onLink := m.linkAtCursor()
	at := e.posAt(x, y)
	e.ed.SetCursor(at)
	if e.ed.Mode == vim.Normal {
		e.dragFrom = &at
	}
	after, ok := m.linkAtCursor()
	if !wasFocused || !onLink || !ok {
		return
	}
	a, _ := linkSpan(before)
	b, _ := linkSpan(after)
	if a != b {
		return
	}
	if err := m.follow(after); err != nil {
		m.setError(err)
	}
}

// posAt is the buffer position drawn at x, y of the pane. A row past the
// pane's edge takes the first or last row drawn.
func (e *editing) posAt(x, y int) vim.Pos {
	r := e.rows[max(0, min(y, len(e.rows)-1))]
	runes := []rune(e.ed.Buf.LineString(r.line))
	col := r.from + colAt(runes[r.from:min(r.to, len(runes))], x-gutter)
	if r.to > r.from && col >= r.to && r.to < len(runes) {
		col = r.to - 1 // the end of a wrapped row, not the next row's start
	}
	return vim.Pos{Line: r.line, Col: col}
}

// dragBuffer selects from where the button went down to the pointer, as v
// and a motion do; y then yanks the selection.
func (m *WikiModel) dragBuffer(x, y int) {
	e := m.edit
	if e == nil || e.dragFrom == nil || e.preview || len(e.rows) == 0 {
		return
	}
	if mode := e.ed.Mode; mode != vim.Normal && mode != vim.Visual {
		return
	}
	at := e.posAt(x, y)
	if e.ed.Mode == vim.Normal && at == *e.dragFrom {
		return // the button moved within one character
	}
	e.ed.Select(*e.dragFrom, at)
}

// dropBuffer copies a drag's selection when config.json sets copy_on_select,
// as terminals copy their own selection on release.
func (m *WikiModel) dropBuffer() {
	e := m.edit
	if e == nil || e.dragFrom == nil || !m.w.Config.CopyOnSelect {
		return
	}
	if _, ok := e.ed.CopySelection(); ok {
		m.setStatus("copied")
	}
}

// colAt is the index of the rune drawn at column x, counting from the first
// rune, as styleRow draws them; past the end it is len(runes).
func colAt(runes []rune, x int) int {
	if x < 0 {
		return 0
	}
	at := 0
	for i, r := range runes {
		w := ansi.StringWidth(string(r))
		switch {
		case r == '\t':
			w = display.TabWidth
		case display.Control(r):
			w = 1
		}
		if x < at+w {
			return i
		}
		at += w
	}
	return len(runes)
}

// wheelBuffer scrolls the page, or its preview.
func (m *WikiModel) wheelBuffer(by int) {
	e := m.edit
	if e.preview {
		e.previewTop = max(0, min(e.previewTop+by, e.previewRows-1))
		return
	}
	e.ed.Scroll(by)
}

// clickTree selects a row: a click on a directory's marker folds it, and
// anywhere else opens the row as enter does.
func (m *WikiModel) clickTree(x, y int) {
	i := m.treeScroll + y
	if i >= len(m.rows) {
		return
	}
	m.focus, m.treeCursor = focusTree, i
	row := m.rows[i]
	if marker := 1 + 2*row.depth; row.dir != "" && x >= marker && x < marker+2 {
		m.treeFold()
		return
	}
	m.treeEnter()
}

// clickList selects a row of a list screen and opens it. A repair is only
// selected: it writes, so it waits for enter.
func (m *WikiModel) clickList(i int) {
	if i >= m.listLen() {
		return
	}
	m.cursor = i
	switch m.screen {
	case screenOffers:
	case screenSearch, screenOpen:
		m.findEnter()
	default:
		m.listEnter()
	}
}

// clickStats runs a stats row's action.
func (m *WikiModel) clickStats(col, row int) {
	cols, _ := m.homeSelection()
	if col >= len(cols) || row >= len(cols[col]) || cols[col][row].action == nil {
		return
	}
	m.homeCol = col
	for i, r := range selectable(cols[col]) {
		if r == row {
			m.homeRow[col] = i
		}
	}
	m.homeEnter()
}
