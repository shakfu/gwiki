package acp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakfu/gwiki/internal/acp/acptest"
)

func TestMain(m *testing.M) {
	acptest.MaybeRun()
	os.Exit(m.Run())
}

// start runs the stand-in agent with repo as the read root and repo/.gwiki/wiki
// as the write root.
func start(t *testing.T) (*Session, string) {
	t.Helper()
	repo := t.TempDir()
	pages := filepath.Join(repo, ".gwiki", "wiki")
	if err := os.MkdirAll(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Start(context.Background(), Options{
		Command: os.Args[0], Env: map[string]string{acptest.Env: "1"},
		Cwd: repo, MCP: MCPServer{Name: "gwiki", Command: "/bin/gwiki", Args: []string{"mcp"}},
		ReadRoots: []string{repo}, WriteRoots: []string{pages},
		ClientName: "gwiki", ClientVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, repo
}

func next(t *testing.T, s *Session) Event {
	t.Helper()
	select {
	case e := <-s.Events():
		return e
	case <-time.After(10 * time.Second):
		t.Fatal("no event from the agent")
		return nil
	}
}

// said collects the agent's message text until the turn ends, answering
// requests with answer.
func said(t *testing.T, s *Session, answer func(Event)) (string, []Event) {
	t.Helper()
	var text strings.Builder
	var seen []Event
	for {
		e := next(t, s)
		seen = append(seen, e)
		switch e := e.(type) {
		case Text:
			if !e.Thought {
				text.WriteString(e.Text)
			}
		case TurnEnded:
			if e.Err != nil {
				t.Fatalf("turn failed: %v", e.Err)
			}
			return text.String() + " [" + e.StopReason + "]", seen
		default:
			if answer != nil {
				answer(e)
			}
		}
	}
}

func TestSessionStreamsAReply(t *testing.T) {
	s, repo := start(t)
	if s.Agent != "stand-in 1" {
		t.Errorf("Agent = %q", s.Agent)
	}
	page := filepath.Join(repo, ".gwiki", "wiki", "index.md")
	if err := s.Prompt("hello", page); err != nil {
		t.Fatal(err)
	}
	if err := s.Prompt("hello"); err == nil {
		t.Error("a second prompt while one runs was accepted")
	}
	text, seen := said(t, s, nil)
	if want := "Hi there; links: file://" + filepath.ToSlash(page) + "; mcp: gwiki=/bin/gwiki mcp [end_turn]"; text != want {
		t.Fatalf("said %q, want %q", text, want)
	}
	var thought, plan, commands bool
	for _, e := range seen {
		switch e := e.(type) {
		case Text:
			thought = thought || e.Thought && e.Text == "pondering"
		case Plan:
			plan = len(e.Entries) == 2 && e.Entries[0].Status == "completed"
		case Commands:
			commands = len(e.List) == 1 && e.List[0].Name == "review"
		}
	}
	if !thought || !plan || !commands {
		t.Errorf("thought %v, plan %v, commands %v: %#v", thought, plan, commands, seen)
	}
}

// A read gets the editor's buffer for an open page, else the file; a path
// outside the read roots is refused.
func TestSessionReads(t *testing.T) {
	s, repo := start(t)
	file := filepath.Join(repo, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	answer := func(open bool) func(Event) {
		return func(e Event) {
			if r, ok := e.(Read); ok {
				r.Answer("unsaved", open)
			}
		}
	}
	s.Prompt("read " + file)
	if text, _ := said(t, s, answer(false)); text != "read: package main\n [end_turn]" {
		t.Errorf("from disk: %q", text)
	}
	s.Prompt("read " + file)
	if text, _ := said(t, s, answer(true)); text != "read: unsaved [end_turn]" {
		t.Errorf("from the buffer: %q", text)
	}
	s.Prompt("read /etc/hosts")
	if text, _ := said(t, s, answer(false)); !strings.Contains(text, "is outside") {
		t.Errorf("outside: %q", text)
	}
}

// A write under the write roots goes to the editor; one outside is refused.
func TestSessionWrites(t *testing.T) {
	s, repo := start(t)
	inside := filepath.Join(repo, ".gwiki", "wiki", "a.md")
	var got Write
	s.Prompt("write " + inside + " hello")
	text, _ := said(t, s, func(e Event) {
		if w, ok := e.(Write); ok {
			got = w
			w.Answer(nil)
		}
	})
	if text != "ok [end_turn]" || got.Path != inside || got.Content != "hello\n" {
		t.Errorf("inside: %q, %+v", text, got)
	}
	s.Prompt("write " + filepath.Join(repo, "main.go") + " x")
	if text, _ := said(t, s, func(e Event) { t.Errorf("asked to write outside: %#v", e) }); !strings.Contains(text, "gwiki writes only under") {
		t.Errorf("outside: %q", text)
	}
}

// An edit inside the write roots asks the person; one outside is rejected
// unasked, with a notice.
func TestSessionPermissions(t *testing.T) {
	s, repo := start(t)
	inside := filepath.Join(repo, ".gwiki", "wiki", "a.md")
	var asked Permission
	s.Prompt("edit " + inside)
	text, _ := said(t, s, func(e Event) {
		if p, ok := e.(Permission); ok {
			asked = p
			p.Answer("yes")
		}
	})
	if text != "chose yes [end_turn]" || len(asked.Options) != 2 || len(asked.ToolCall.Diffs) != 1 || asked.ToolCall.Kind != "edit" {
		t.Errorf("inside: %q, %+v", text, asked)
	}

	var notice string
	s.Prompt("edit " + filepath.Join(repo, "main.go"))
	text, _ = said(t, s, func(e Event) {
		switch e := e.(type) {
		case Permission:
			t.Error("asked about an edit outside the write roots")
			e.Answer("yes")
		case Notice:
			notice = e.Text
		}
	})
	if text != "chose no [end_turn]" || !strings.Contains(notice, "outside") {
		t.Errorf("outside: %q, notice %q", text, notice)
	}
}

// Cancel ends the turn cancelled, and answers a pending permission cancelled.
func TestSessionCancel(t *testing.T) {
	s, repo := start(t)
	s.Prompt("wait")
	time.Sleep(100 * time.Millisecond)
	s.Cancel()
	if text, _ := said(t, s, nil); text != " [cancelled]" {
		t.Errorf("wait: %q", text)
	}

	s.Prompt("edit " + filepath.Join(repo, ".gwiki", "wiki", "a.md"))
	text, _ := said(t, s, func(e Event) {
		if _, ok := e.(Permission); ok {
			s.Cancel()
		}
	})
	if text != " [cancelled]" {
		t.Errorf("pending permission: %q", text)
	}

	s.Close()
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("Done not closed")
	}
	if err := s.Prompt("hello"); err == nil {
		t.Error("a prompt after Close was accepted")
	}
}
