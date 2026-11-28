// Package acp runs an ACP agent for the interface, which is the editor in the
// Agent Client Protocol: it starts the agent as a subprocess, opens a session
// with gwiki's own MCP server, sends prompts, and turns what the agent sends
// into the events below.
//
// The SDK's types stay in this package, so its releases, which are before
// 1.0, change this package only. The agent may read any file in the
// repository and write only under the write roots; edits it asks permission
// for outside them are rejected without asking. That covers what passes
// through gwiki. An agent's own tools write to the disk directly, and only the
// agent's settings govern those.
package acp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/coder/acp-go-sdk"
)

// Options say how to start an agent and what it may reach.
type Options struct {
	Command string
	Args    []string
	Env     map[string]string

	// Cwd is the session's directory, the repository.
	Cwd string

	// MCP is the MCP server the agent is given: gwiki itself.
	MCP MCPServer

	// ReadRoots and WriteRoots bound the files the agent reads and writes
	// through gwiki. Each is absolute.
	ReadRoots, WriteRoots []string

	ClientName, ClientVersion string
}

// MCPServer is a stdio MCP server.
type MCPServer struct {
	Name, Command string
	Args          []string
}

// Event is what the agent sent, in the order sent.
type Event interface{ event() }

// Text is a chunk of the agent's reply, or of its reasoning.
type Text struct {
	Thought bool
	Text    string
}

// ToolCall is a tool call's state, sent whole each time it changes.
type ToolCall struct {
	ID, Title, Kind, Status string
	Paths                   []string
	Diffs                   []Diff
	Output                  string
}

// Diff is a change a tool call made or proposes. Old is empty and New true for
// a new file.
type Diff struct {
	Path     string
	Old, New string
	Created  bool
}

// Plan is the agent's plan, sent whole.
type Plan struct{ Entries []PlanEntry }

// PlanEntry is one step: pending, in_progress or completed.
type PlanEntry struct{ Content, Status string }

// Commands are the slash commands the agent offers.
type Commands struct{ List []Command }

// Command is a slash command, run by sending "/name input" as a prompt.
type Command struct{ Name, Description string }

// Notice is something gwiki did for the agent unasked, such as rejecting an
// edit outside the write roots.
type Notice struct{ Text string }

// Permission asks the person to choose among Options for ToolCall. Answer
// must be called once.
type Permission struct {
	ToolCall ToolCall
	Options  []Option
	reply    chan string
}

// Option is one answer to a Permission: its Kind is allow_once,
// allow_always, reject_once or reject_always.
type Option struct{ ID, Name, Kind string }

// Answer gives the chosen option's ID, or "" to cancel.
func (p Permission) Answer(id string) {
	select {
	case p.reply <- id:
	default:
	}
}

// Read asks for a file's unsaved text, which only the editor has. Answer with
// open false for a file that is not open with changes; it is read from disk.
type Read struct {
	Path  string
	reply chan readReply
}

type readReply struct {
	text string
	open bool
}

// Answer gives the buffer's text, or open false.
func (r Read) Answer(text string, open bool) {
	select {
	case r.reply <- readReply{text, open}:
	default:
	}
}

// Write asks the editor to write a file under a write root, which it does
// through the wiki so that a page's checks apply. Answer must be called once.
type Write struct {
	Path, Content string
	reply         chan error
}

// Answer gives the write's outcome.
func (w Write) Answer(err error) {
	select {
	case w.reply <- err:
	default:
	}
}

// TurnEnded ends a prompt: StopReason is end_turn, max_tokens,
// max_turn_requests, refusal or cancelled, unless Err is set.
type TurnEnded struct {
	StopReason string
	Err        error
}

// Exited reports the agent's process ending.
type Exited struct{ Err error }

func (Text) event()       {}
func (ToolCall) event()   {}
func (Plan) event()       {}
func (Commands) event()   {}
func (Notice) event()     {}
func (Permission) event() {}
func (Read) event()       {}
func (Write) event()      {}
func (TurnEnded) event()  {}
func (Exited) event()     {}

// Session is a running agent with one session open.
type Session struct {
	opts   Options
	cmd    *exec.Cmd
	conn   *sdk.ClientSideConnection
	id     sdk.SessionId
	events chan Event
	done   chan struct{}
	stderr *tail

	// Agent is the agent's name and version, as it gave them.
	Agent string

	mu      sync.Mutex
	running bool
	// waits is the running turn's context for waits on the person, ended
	// by Cancel; the prompt request itself ends when the agent answers it.
	waits     context.Context
	stopWaits context.CancelFunc
	stopTurn  context.CancelFunc // ends the prompt request, on Close
	tools     map[sdk.ToolCallId]*ToolCall
	closed    bool
}

// startTimeout bounds starting the agent and opening its session; an agent
// run through npx may first download itself.
var startTimeout = 2 * time.Minute

// Start runs the agent and opens a session.
func Start(ctx context.Context, o Options) (*Session, error) {
	bin, err := exec.LookPath(o.Command)
	if err != nil {
		return nil, fmt.Errorf("agent %s: %w", o.Command, err)
	}
	s := &Session{opts: o, events: make(chan Event, 256), done: make(chan struct{}), stderr: &tail{max: 4096}, tools: map[sdk.ToolCallId]*ToolCall{}}
	s.cmd = exec.Command(bin, o.Args...)
	s.cmd.Dir = o.Cwd
	s.cmd.Env = os.Environ()
	for k, v := range o.Env {
		s.cmd.Env = append(s.cmd.Env, k+"="+v)
	}
	s.cmd.Stderr = s.stderr
	stdin, err := s.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := s.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := s.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", o.Command, err)
	}
	s.conn = sdk.NewClientSideConnection(client{s}, stdin, stdout)
	// The SDK logs to the default logger, which would draw over the
	// interface; its lines go with the agent's own error output.
	s.conn.SetLogger(slog.New(slog.NewTextHandler(s.stderr, nil)))
	go func() {
		err := s.cmd.Wait()
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if !closed {
			s.send(Exited{s.withStderr(fmt.Errorf("the agent exited: %v", err))})
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	init, err := s.conn.Initialize(ctx, sdk.InitializeRequest{
		ProtocolVersion: sdk.ProtocolVersionNumber,
		ClientCapabilities: sdk.ClientCapabilities{
			Fs: sdk.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		},
		ClientInfo: &sdk.Implementation{Name: o.ClientName, Version: o.ClientVersion},
	})
	if err != nil {
		s.Close()
		return nil, s.withStderr(fmt.Errorf("initialize %s: %w", o.Command, err))
	}
	if init.ProtocolVersion != sdk.ProtocolVersionNumber {
		s.Close()
		return nil, fmt.Errorf("%s speaks ACP version %d, gwiki version %d", o.Command, init.ProtocolVersion, sdk.ProtocolVersionNumber)
	}
	if init.AgentInfo != nil {
		s.Agent = strings.TrimSpace(init.AgentInfo.Name + " " + init.AgentInfo.Version)
	}
	mcp := sdk.McpServer{Stdio: &sdk.McpServerStdio{Name: o.MCP.Name, Command: o.MCP.Command, Args: o.MCP.Args, Env: []sdk.EnvVariable{}}}
	sess, err := s.conn.NewSession(ctx, sdk.NewSessionRequest{Cwd: o.Cwd, McpServers: []sdk.McpServer{mcp}})
	if err != nil {
		s.Close()
		var names []string
		for _, m := range init.AuthMethods {
			names = append(names, methodName(m))
		}
		if len(names) > 0 {
			return nil, s.withStderr(fmt.Errorf("open a session with %s: %w; it may need you to log in first (%s)", o.Command, err, strings.Join(names, ", ")))
		}
		return nil, s.withStderr(fmt.Errorf("open a session with %s: %w", o.Command, err))
	}
	s.id = sess.SessionId
	return s, nil
}

// Events delivers what the agent sends. It is never closed; Done is, by Close.
func (s *Session) Events() <-chan Event { return s.events }

// Done is closed when the session is closed.
func (s *Session) Done() <-chan struct{} { return s.done }

// Busy reports a prompt running.
func (s *Session) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Prompt sends text, with file links to give as context, and returns at
// once; TurnEnded follows.
func (s *Session) Prompt(text string, files ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("the agent has stopped")
	}
	if s.running {
		return errors.New("the agent is still answering; esc cancels")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.running, s.stopTurn = true, cancel
	s.waits, s.stopWaits = context.WithCancel(context.Background())
	blocks := []sdk.ContentBlock{sdk.TextBlock(text)}
	for _, f := range files {
		blocks = append(blocks, sdk.ResourceLinkBlock(filepath.Base(f), "file://"+filepath.ToSlash(f)))
	}
	go func() {
		res, err := s.conn.Prompt(ctx, sdk.PromptRequest{SessionId: s.id, Prompt: blocks})
		s.mu.Lock()
		s.running = false
		s.stopWaits()
		s.mu.Unlock()
		cancel()
		if err != nil {
			// The agent's error carries its own message; its last line of
			// error output is often unrelated, such as a startup note.
			s.send(TurnEnded{Err: err})
			return
		}
		s.send(TurnEnded{StopReason: string(res.StopReason)})
	}()
	return nil
}

// Cancel asks the agent to stop the prompt running; it ends with TurnEnded
// cancelled. A permission still pending is answered cancelled.
func (s *Session) Cancel() {
	s.mu.Lock()
	running, stop := s.running, s.stopWaits
	s.mu.Unlock()
	if !running {
		return
	}
	_ = s.conn.Cancel(context.Background(), sdk.CancelNotification{SessionId: s.id})
	stop()
}

// Close stops the agent.
func (s *Session) Close() {
	s.mu.Lock()
	if !s.closed {
		close(s.done)
	}
	s.closed = true
	if s.running {
		s.stopWaits()
		s.stopTurn()
	}
	s.mu.Unlock()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func (s *Session) send(e Event) {
	select {
	case s.events <- e:
	case <-s.done:
	}
}

func (s *Session) withStderr(err error) error {
	if msg := strings.TrimSpace(s.stderr.String()); msg != "" {
		lines := strings.Split(msg, "\n")
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(lines[len(lines)-1]))
	}
	return err
}

func methodName(m sdk.AuthMethod) string {
	switch {
	case m.Agent != nil:
		return m.Agent.Name
	case m.Terminal != nil:
		return m.Terminal.Name
	}
	return "an unknown method"
}

// within reports whether an absolute path is under one of roots.
func within(p string, roots []string) bool {
	p = filepath.Clean(p)
	return slices.ContainsFunc(roots, func(r string) bool {
		rel, err := filepath.Rel(r, p)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	})
}

// tail keeps the last max bytes written, for the agent's error output.
type tail struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(bytes.ToValidUTF8(t.buf, nil))
}

var _ io.Writer = (*tail)(nil)
