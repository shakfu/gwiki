package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shakfu/gwiki/internal/acp"
	"github.com/shakfu/gwiki/internal/acp/acptest"
	"github.com/shakfu/gwiki/internal/wiki"
)

func TestMain(m *testing.M) {
	acptest.MaybeRun()
	os.Exit(m.Run())
}

// agentFixture is the wiki fixture with the stand-in agent configured, wide
// enough for the agent column.
func agentFixture(t *testing.T) *wikiFixture {
	t.Helper()
	f := newWikiFixture(t)
	f.w.Agents = map[string]wiki.Agent{"standin": {Command: os.Args[0], Env: map[string]string{acptest.Env: "1"}}}
	f.m.width, f.m.focus = 140, focusContent
	return f
}

// run delivers a command's messages to the model until one satisfies done,
// following the commands each message returns.
func (f *wikiFixture) run(cmd tea.Cmd, done func(tea.Msg) bool) {
	f.t.Helper()
	queue := []tea.Cmd{cmd}
	deadline := time.Now().Add(15 * time.Second)
	for len(queue) > 0 {
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out; status %q\n%s", f.m.status, f.view())
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if msg == nil {
			continue
		}
		_, next := f.m.Update(msg)
		if done(msg) {
			// The event listener stays for the next turn.
			f.listener = next
			return
		}
		queue = append(queue, next)
	}
	f.t.Fatalf("ran out of messages; status %q\n%s", f.m.status, f.view())
}

func turnEnded(msg tea.Msg) bool {
	e, ok := msg.(agentEventMsg)
	if !ok {
		return false
	}
	_, ended := e.e.(acp.TurnEnded)
	return ended
}

// startAgent runs :agent to the session being ready.
func (f *wikiFixture) startAgent() {
	f.t.Helper()
	cmd := f.press(":agent", "enter")
	f.run(cmd, func(msg tea.Msg) bool { _, ok := msg.(agentStartedMsg); return ok })
	if f.m.agent == nil || f.m.agent.sess == nil {
		f.t.Fatalf("no session: %q", f.m.status)
	}
}

// ask sends a prompt and runs the turn to its end.
func (f *wikiFixture) ask(text string) {
	f.t.Helper()
	f.m.focus = focusContent
	f.press(":ask "+text, "enter")
	f.run(f.listener, turnEnded)
}

func TestAgentConversation(t *testing.T) {
	f := agentFixture(t)
	f.startAgent()
	if f.m.focus != focusAgent || !strings.Contains(f.m.status, "standin (stand-in 1) ready") {
		t.Fatalf("focus %v, status %q", f.m.focus, f.m.status)
	}
	f.ask("hello")
	view := flat(f.view())
	page := filepath.ToSlash(f.m.w.PageFile("index"))
	for _, want := range []string{"> hello", "Hi there;", "[x] look", "[ ] write", "commands: /review", "thinking: pondering"} {
		if !strings.Contains(view, want) {
			t.Errorf("the column lacks %q:\n%s", want, f.view())
		}
	}
	reply := f.m.agent.entries[len(f.m.agent.entries)-1].text
	if !strings.Contains(reply, "links: file://"+page) || !strings.Contains(reply, "mcp: gwiki=") || !strings.Contains(reply, " -C "+f.m.w.Root+" mcp") {
		t.Errorf("reply %q", reply)
	}
}

// A read of the open page gets its unsaved text; a write goes through the
// wiki, refused over unsaved edits; a file that is not a page is written too.
func TestAgentReadsAndWrites(t *testing.T) {
	f := agentFixture(t)
	f.startAgent()
	index := f.m.w.PageFile("index")
	f.m.focus = focusContent
	f.press("i", "X", "esc")
	f.ask("read " + index)
	if got := f.m.agent.entries[len(f.m.agent.entries)-1].text; !strings.HasPrefix(got, "read: X# Home") {
		t.Fatalf("read the open page: %q", got)
	}
	f.ask("write " + index + " theirs")
	if got := f.m.agent.entries[len(f.m.agent.entries)-1].text; !strings.Contains(got, "unsaved edits") {
		t.Fatalf("write over unsaved edits: %q", got)
	}
	f.press("u")

	pages := filepath.Join(f.root, ".gwiki", "wiki")
	f.ask("write " + filepath.Join(pages, "notes", "new.qmd") + " # New")
	if got := f.source("index"); !strings.HasPrefix(got, "# Home") {
		t.Fatalf("index changed: %q", got)
	}
	if raw, err := os.ReadFile(filepath.Join(pages, "notes", "new.qmd")); err != nil || string(raw) != "# New\n" {
		t.Fatalf("new.qmd = %q, %v", raw, err)
	}
	if info, err := f.m.w.Find("notes/new"); err != nil || info.Ext != ".qmd" {
		t.Fatalf("not indexed: %+v, %v", info, err)
	}
	f.ask("write " + filepath.Join(pages, "chart.svg") + " <svg/>")
	if raw, _ := os.ReadFile(filepath.Join(pages, "chart.svg")); string(raw) != "<svg/>\n" {
		t.Fatalf("chart.svg = %q", raw)
	}
	f.ask("write " + filepath.Join(f.root, ".gwiki", "config.json") + " {}")
	if got := f.m.agent.entries[len(f.m.agent.entries)-1].text; !strings.Contains(got, "gwiki writes only under") {
		t.Fatalf("write to config.json: %q", got)
	}
}

// A permission request takes the input line: a number chooses, esc cancels.
func TestAgentPermission(t *testing.T) {
	f := agentFixture(t)
	f.startAgent()
	page := filepath.Join(f.root, ".gwiki", "wiki", "index.md")
	f.m.focus = focusContent
	f.press(":ask edit "+page, "enter")
	f.run(f.listener, func(msg tea.Msg) bool { return f.m.prompt != nil })
	if !strings.Contains(f.m.prompt.label, "1 Allow, 2 Reject") {
		t.Fatalf("prompt %q", f.m.prompt.label)
	}
	f.press("1", "enter")
	f.run(f.listener, turnEnded)
	view := flat(f.view())
	if !strings.Contains(view, "chose yes") || !strings.Contains(view, "[edit] Edit /") || !strings.Contains(view, "- a") || !strings.Contains(view, "+ b") {
		t.Fatalf("after allowing:\n%s", f.view())
	}

	f.press(":ask edit "+page, "enter")
	f.run(f.listener, func(msg tea.Msg) bool { return f.m.prompt != nil })
	f.press("esc")
	f.run(f.listener, turnEnded)
	if !strings.Contains(flat(f.view()), "cancelled") {
		t.Fatalf("after esc:\n%s", f.view())
	}
}

// esc in the column cancels a turn; with no turn, it returns to the page.
func TestAgentCancel(t *testing.T) {
	f := agentFixture(t)
	f.startAgent()
	f.press(":ask wait", "enter")
	time.Sleep(100 * time.Millisecond)
	f.m.focus = focusAgent
	f.press("esc")
	f.run(f.listener, turnEnded)
	if !strings.Contains(flat(f.view()), "turn ended: cancelled") {
		t.Fatalf("after esc:\n%s", f.view())
	}
	f.press("esc")
	if f.m.focus != focusContent {
		t.Fatalf("focus %v", f.m.focus)
	}
}

// On a narrow terminal the column takes the page's place while focused.
func TestAgentNarrow(t *testing.T) {
	f := agentFixture(t)
	f.m.width = 100
	f.startAgent()
	if f.m.agentWidth() != 0 || !f.m.agentInstead() || !strings.Contains(f.view(), " standin") || strings.Contains(f.view(), "# Home") {
		t.Fatalf("narrow, focused:\n%s", f.view())
	}
	f.m.focus = focusContent
	if f.m.agentInstead() || !strings.Contains(f.view(), "# Home") {
		t.Fatalf("narrow, the page focused:\n%s", f.view())
	}
}

func TestAgentNotConfigured(t *testing.T) {
	f := newWikiFixture(t)
	f.m.focus = focusContent
	f.w.Agents = nil
	if f.press(":agent", "enter"); !strings.Contains(f.view(), `no agents: add one under "agents"`) {
		t.Fatalf("view:\n%s", f.view())
	}
	if f.press(":ask hi", "enter"); !strings.Contains(f.view(), "no agent running") {
		t.Fatalf("view:\n%s", f.view())
	}
}
