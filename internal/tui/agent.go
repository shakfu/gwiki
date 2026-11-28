package tui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shakfu/gwiki/internal/acp"
	"github.com/shakfu/gwiki/internal/display"
	"github.com/shakfu/gwiki/internal/render"
	"github.com/shakfu/gwiki/internal/vim"
	"github.com/shakfu/gwiki/internal/wiki"
)

// The agent column holds a conversation with an ACP agent, one the per-user
// config.json names; see package acp. gwiki is the editor: it gives the agent
// its own MCP server, the open page's unsaved text when the agent reads it,
// and writes under .gwiki/wiki and .gwiki/templates for it, through the
// wiki's checks.

// agentEntry is one item of the conversation.
type agentEntry struct {
	kind entryKind
	text string
	tool acp.ToolCall
}

type entryKind int

const (
	entryUser entryKind = iota
	entryReply
	entryThought
	entryTool
	entryNotice
	entryError
)

// agentPane is the agent column's state.
type agentPane struct {
	name     string
	sess     *acp.Session
	starting bool
	open     bool

	entries  []agentEntry
	plan     []acp.PlanEntry
	commands []acp.Command
	perm     *acp.Permission // asked, not yet answered
	scroll   int             // rows up from the end; 0 follows it
}

type agentStartedMsg struct {
	name string
	sess *acp.Session
	err  error
}

type agentEventMsg struct {
	sess *acp.Session
	e    acp.Event
}

// agentWide is the terminal width from which the agent column shows beside
// the page; below it, the column takes the page's place while focused.
const agentWide = 120

// agentWidth is the agent column beside the page, or 0.
func (m *WikiModel) agentWidth() int {
	if m.agent == nil || !m.agent.open || m.width < agentWide {
		return 0
	}
	return max(40, m.width/3)
}

// agentInstead reports the agent column drawn in the page's place, on a
// narrow terminal.
func (m *WikiModel) agentInstead() bool {
	return m.agent != nil && m.agent.open && m.width < agentWide && m.focus == focusAgent
}

// startAgent opens the agent column, starting the agent named, or the only
// one configured, unless it runs.
func (m *WikiModel) startAgent(name string) error {
	agents := m.w.Agents
	if len(agents) == 0 {
		path, _ := wiki.UserConfigPath(os.Getenv)
		return fmt.Errorf("no agents: add one under \"agents\" in %s", path)
	}
	if name == "" {
		if m.agent != nil {
			m.agent.open = !m.agent.open
			if m.agent.open {
				m.focus = focusAgent
			} else if m.focus == focusAgent {
				m.focus = focusContent
			}
			return nil
		}
		if len(agents) > 1 {
			return fmt.Errorf("name an agent: %s", strings.Join(slices.Sorted(maps.Keys(agents)), ", "))
		}
		for n := range agents {
			name = n
		}
	}
	cfg, ok := agents[name]
	if !ok {
		return fmt.Errorf("no agent %q; there are %s", name, strings.Join(slices.Sorted(maps.Keys(agents)), ", "))
	}
	if m.agent != nil && m.agent.name == name && (m.agent.sess != nil || m.agent.starting) {
		m.agent.open, m.focus = true, focusAgent
		return nil
	}
	if m.agent != nil && m.agent.sess != nil {
		m.agent.sess.Close()
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"-C", m.w.Root, "mcp"}
	if m.w.User {
		args = []string{"-u", "mcp"}
	}
	opts := acp.Options{
		Command: cfg.Command, Args: cfg.Args, Env: cfg.Env,
		Cwd:        m.w.Root,
		MCP:        acp.MCPServer{Name: "gwiki", Command: self, Args: args},
		ReadRoots:  []string{m.w.Repo},
		WriteRoots: []string{m.w.PagesPath(), m.w.TemplatesPath()},
		ClientName: "gwiki", ClientVersion: Version,
	}
	m.agent = &agentPane{name: name, starting: true, open: true}
	m.focus = focusAgent
	m.after = func() tea.Msg {
		s, err := acp.Start(context.Background(), opts)
		return agentStartedMsg{name: name, sess: s, err: err}
	}
	m.setStatus("starting " + name + "...")
	return nil
}

// Version is gwiki's version, which the agent is told.
var Version = "dev"

// waitAgent delivers the session's next event.
func waitAgent(s *acp.Session) tea.Cmd {
	return func() tea.Msg {
		select {
		case e := <-s.Events():
			return agentEventMsg{sess: s, e: e}
		case <-s.Done():
			return nil
		}
	}
}

func (m *WikiModel) agentStarted(msg agentStartedMsg) tea.Cmd {
	a := m.agent
	if a == nil || a.name != msg.name {
		if msg.sess != nil {
			msg.sess.Close()
		}
		return nil
	}
	a.starting = false
	if msg.err != nil {
		a.entries = append(a.entries, agentEntry{kind: entryError, text: msg.err.Error()})
		m.setError(msg.err)
		return nil
	}
	a.sess = msg.sess
	label := a.name
	if msg.sess.Agent != "" {
		label += " (" + msg.sess.Agent + ")"
	}
	m.setStatus(label + " ready; :ask sends a prompt")
	return waitAgent(msg.sess)
}

// ask sends a prompt, with the open page's file as context.
func (m *WikiModel) ask(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New(":ask needs a prompt")
	}
	a := m.agent
	switch {
	case a == nil:
		return errors.New("no agent running; :agent starts one")
	case a.starting:
		return errors.New(a.name + " is still starting")
	case a.sess == nil:
		return errors.New(a.name + " has stopped; :agent " + a.name + " starts it again")
	}
	var files []string
	if m.cur != nil {
		files = append(files, m.w.PageFile(m.cur.info.Path))
	}
	if err := a.sess.Prompt(text, files...); err != nil {
		return err
	}
	a.entries = append(a.entries, agentEntry{kind: entryUser, text: text})
	a.open, a.scroll = true, 0
	return nil
}

// promptAgent asks for a prompt on the input line.
func (m *WikiModel) promptAgent() {
	if m.agent == nil {
		return
	}
	m.askWiki("ask "+m.agent.name+": ", "", m.ask)
}

// cancelTurn stops the agent's turn, or leaves the column.
func (m *WikiModel) cancelTurn() {
	if a := m.agent; a != nil && a.sess != nil && a.sess.Busy() {
		a.sess.Cancel()
		m.setStatus("cancelling")
		return
	}
	m.toContent()
}

// agentEvent handles one event and waits for the next.
func (m *WikiModel) agentEvent(msg agentEventMsg) tea.Cmd {
	a := m.agent
	if a == nil || a.sess != msg.sess {
		answerStale(msg.e)
		return nil
	}
	switch e := msg.e.(type) {
	case acp.Text:
		kind := entryReply
		if e.Thought {
			kind = entryThought
		}
		if n := len(a.entries); n > 0 && a.entries[n-1].kind == kind {
			a.entries[n-1].text += e.Text
		} else {
			a.entries = append(a.entries, agentEntry{kind: kind, text: e.Text})
		}
	case acp.ToolCall:
		i := slices.IndexFunc(a.entries, func(x agentEntry) bool { return x.kind == entryTool && x.tool.ID == e.ID })
		if i < 0 {
			a.entries = append(a.entries, agentEntry{kind: entryTool, tool: e})
		} else {
			a.entries[i].tool = e
		}
	case acp.Plan:
		a.plan = e.Entries
	case acp.Commands:
		a.commands = e.List
	case acp.Notice:
		a.entries = append(a.entries, agentEntry{kind: entryNotice, text: e.Text})
	case acp.Permission:
		a.perm = &e
		a.open = true
		m.offerPermission()
	case acp.Read:
		m.agentRead(e)
	case acp.Write:
		m.agentWrite(e)
	case acp.TurnEnded:
		switch {
		case e.Err != nil:
			a.entries = append(a.entries, agentEntry{kind: entryError, text: e.Err.Error()})
		case e.StopReason != "end_turn":
			a.entries = append(a.entries, agentEntry{kind: entryNotice, text: "turn ended: " + strings.ReplaceAll(e.StopReason, "_", " ")})
		}
		if !m.statusErr {
			m.setStatus(a.name + " done")
		}
	case acp.Exited:
		a.entries = append(a.entries, agentEntry{kind: entryError, text: e.Err.Error()})
		a.sess.Close()
		a.sess, a.perm = nil, nil
		m.setError(e.Err)
		return nil
	}
	return waitAgent(msg.sess)
}

// answerStale answers a request from a session no longer shown, so its agent
// does not wait.
func answerStale(e acp.Event) {
	switch e := e.(type) {
	case acp.Permission:
		e.Answer("")
	case acp.Read:
		e.Answer("", false)
	case acp.Write:
		e.Answer(errors.New("the session was closed"))
	}
}

// offerPermission puts a pending permission on the input line, once no other
// prompt holds it.
func (m *WikiModel) offerPermission() {
	a := m.agent
	if a == nil || a.perm == nil || m.prompt != nil {
		return
	}
	p := *a.perm
	var choices []string
	for i, o := range p.Options {
		choices = append(choices, fmt.Sprintf("%d %s", i+1, o.Name))
	}
	title := p.ToolCall.Title
	if title == "" {
		title = p.ToolCall.Kind
	}
	m.askWiki(display.Line(title)+"? "+strings.Join(choices, ", ")+": ", "", func(answer string) error {
		a.perm = nil
		n, err := strconv.Atoi(strings.TrimSpace(answer))
		if err != nil || n < 1 || n > len(p.Options) {
			p.Answer("")
			return errors.New("no option chosen; the request was cancelled")
		}
		p.Answer(p.Options[n-1].ID)
		return nil
	})
	m.prompt.cancel = func() {
		a.perm = nil
		p.Answer("")
	}
}

// agentRead gives the agent the open page's unsaved text.
func (m *WikiModel) agentRead(r acp.Read) {
	if m.cur != nil && m.edit != nil && m.edit.ed.Dirty && filepath.Clean(m.w.PageFile(m.cur.info.Path)) == r.Path {
		r.Answer(m.edit.ed.Text(), true)
		return
	}
	r.Answer("", false)
}

// agentWrite writes for the agent under .gwiki/wiki or .gwiki/templates:
// a page through the wiki's checks, any other file in place. The open page
// is not written over unsaved edits.
func (m *WikiModel) agentWrite(w acp.Write) {
	err := m.writeForAgent(w.Path, w.Content)
	if err == nil {
		m.afterWrite()
		if m.cur != nil && filepath.Clean(m.w.PageFile(m.cur.info.Path)) == w.Path {
			m.reload()
		}
		rel, _ := filepath.Rel(m.w.Root, w.Path)
		m.setStatus(m.agent.name + " wrote " + filepath.ToSlash(rel))
	}
	w.Answer(err)
}

func (m *WikiModel) writeForAgent(path, content string) error {
	if m.cur != nil && m.edit != nil && m.edit.ed.Dirty && filepath.Clean(m.w.PageFile(m.cur.info.Path)) == path {
		return errors.New("the page has unsaved edits in gwiki; it is written when they are saved or discarded")
	}
	if rel, err := filepath.Rel(m.w.TemplatesPath(), path); err == nil && !strings.HasPrefix(rel, "..") {
		return m.w.PutTemplate(rel, []byte(content))
	}
	rel, err := filepath.Rel(m.w.PagesPath(), path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%s is outside the wiki", path)
	}
	if wiki.IsPageFile(rel) {
		_, err := m.w.WritePageFile(rel, []byte(content))
		return err
	}
	return m.w.PutFile(rel, []byte(content), true)
}

// ---------------------------------------------------------------- drawing

// viewAgent draws the agent column.
func (m *WikiModel) viewAgent(width, height int) []string {
	a := m.agent
	head := " " + a.name
	switch {
	case a.starting:
		head += "  starting..."
	case a.sess == nil:
		head += "  stopped"
	case a.sess.Busy():
		head += "  working...  esc cancels"
	}
	style := styleDim
	if m.focus == focusAgent {
		style = styleHeader
	}
	out := []string{style.Render(clip(head, width))}

	var body []string
	for _, e := range a.entries {
		body = append(body, m.drawEntry(e, width)...)
	}
	if len(a.plan) > 0 {
		body = append(body, styleDim.Render(" plan"))
		for _, p := range a.plan {
			box := "[ ]"
			switch p.Status {
			case "in_progress":
				box = "[~]"
			case "completed":
				box = "[x]"
			}
			body = append(body, wrapPlain(box+" "+p.Content, width, 1, stylePlain)...)
		}
	}
	if len(a.commands) > 0 {
		var names []string
		for _, c := range a.commands {
			names = append(names, "/"+c.Name)
		}
		body = append(body, wrapPlain("commands: "+strings.Join(names, " "), width, 1, styleDim)...)
	}
	if len(a.entries) == 0 && !a.starting && a.sess != nil {
		body = append(body, styleDim.Render(" :ask, or i here, sends a prompt"))
	}

	room := height - len(out)
	a.scroll = max(0, min(a.scroll, len(body)-room))
	end := len(body) - a.scroll
	start := max(0, end-room)
	return append(out, body[start:end]...)
}

func (m *WikiModel) drawEntry(e agentEntry, width int) []string {
	switch e.kind {
	case entryUser:
		return append([]string{""}, wrapPlain("> "+e.text, width, 1, styleBold)...)
	case entryReply:
		doc := render.Render([]byte(e.text), render.Options{Width: max(10, width-2), Styles: readerStyles, Selected: -1})
		out := make([]string, len(doc.Lines))
		for i, l := range doc.Lines {
			out[i] = " " + l
		}
		return out
	case entryThought:
		lines := strings.Split(strings.TrimSpace(e.text), "\n")
		return []string{styleDim.Render(clip(" thinking: "+lines[len(lines)-1], width))}
	case entryTool:
		return drawTool(e.tool, width)
	case entryNotice:
		return wrapPlain(e.text, width, 1, styleDim)
	}
	return wrapPlain(e.text, width, 1, styleError)
}

// drawTool is a tool call's line, with its diffs, at most maxDiffLines each.
func drawTool(t acp.ToolCall, width int) []string {
	status, ok := map[string]lipgloss.Style{"completed": styleOK, "failed": styleError, "in_progress": styleWarn}[t.Status]
	if !ok {
		status = styleDim
	}
	kind := t.Kind
	if kind == "" {
		kind = "tool"
	}
	line := " [" + kind + "] " + display.Line(t.Title) + "  "
	out := []string{clip(line+status.Render(strings.ReplaceAll(t.Status, "_", " ")), width)}
	for _, d := range t.Diffs {
		out = append(out, styleDim.Render(clip("   "+d.Path, width)))
		for i, l := range diffLines(d) {
			if i == maxDiffLines {
				out = append(out, styleDim.Render("   ..."))
				break
			}
			s := styleOK
			if strings.HasPrefix(l, "-") {
				s = styleError
			}
			out = append(out, s.Render(clip("   "+display.Line(l), width)))
		}
	}
	if t.Output != "" && t.Status == "failed" {
		out = append(out, wrapPlain(t.Output, width, 3, styleError)...)
	}
	return out
}

const maxDiffLines = 12

// diffLines is a short line diff: the lines between the common head and tail,
// removed then added.
func diffLines(d acp.Diff) []string {
	old, now := splitLines(d.Old), splitLines(d.New)
	head := 0
	for head < len(old) && head < len(now) && old[head] == now[head] {
		head++
	}
	tail := 0
	for tail < len(old)-head && tail < len(now)-head && old[len(old)-1-tail] == now[len(now)-1-tail] {
		tail++
	}
	var out []string
	for _, l := range old[head : len(old)-tail] {
		out = append(out, "- "+l)
	}
	for _, l := range now[head : len(now)-tail] {
		out = append(out, "+ "+l)
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// wrapPlain wraps text to width, each row indented by indent spaces.
func wrapPlain(text string, width, indent int, style lipgloss.Style) []string {
	var out []string
	room := max(1, width-indent)
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		r := []rune(display.Line(line))
		starts := vim.Wrap(r, room)
		for i, s := range starts {
			e := len(r)
			if i+1 < len(starts) {
				e = starts[i+1]
			}
			out = append(out, style.Render(strings.Repeat(" ", indent)+strings.TrimRight(string(r[s:e]), " ")))
		}
	}
	return out
}

// agentScroll moves the column by rows; up is positive.
func (m *WikiModel) agentScroll(by int) {
	if m.agent != nil {
		m.agent.scroll = max(0, m.agent.scroll+by)
	}
}
