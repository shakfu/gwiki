package acp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	sdk "github.com/coder/acp-go-sdk"
)

// client is the editor's side of the connection, which the agent calls.
type client struct{ s *Session }

var _ sdk.Client = client{}

// RequestPermission asks the person, except for an edit outside the write
// roots, which is rejected unasked.
func (c client) RequestPermission(ctx context.Context, p sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	s := c.s
	tc := s.toolCallFrom(p.ToolCall)
	// What the request shows, such as the diff, belongs to the call too, for
	// the person to see while deciding.
	s.mu.Lock()
	stored := tc
	s.tools[p.ToolCall.ToolCallId] = &stored
	s.mu.Unlock()
	s.send(copyTool(&stored))
	options := make([]Option, len(p.Options))
	for i, o := range p.Options {
		options[i] = Option{ID: string(o.OptionId), Name: o.Name, Kind: string(o.Kind)}
	}
	if out := s.outside(tc); out != "" {
		s.send(Notice{fmt.Sprintf("rejected %q: %s is outside %s", tc.Title, out, strings.Join(s.opts.WriteRoots, ", "))})
		for _, o := range p.Options {
			if o.Kind == sdk.PermissionOptionKindRejectOnce || o.Kind == sdk.PermissionOptionKindRejectAlways {
				return selected(o.OptionId), nil
			}
		}
		return cancelled(), nil
	}

	// Cancelling the prompt answers a pending permission cancelled.
	s.mu.Lock()
	waits := s.waits
	s.mu.Unlock()
	if waits == nil {
		waits = context.Background()
	}
	reply := make(chan string, 1)
	s.send(Permission{ToolCall: tc, Options: options, reply: reply})
	select {
	case id := <-reply:
		if id == "" {
			return cancelled(), nil
		}
		return selected(sdk.PermissionOptionId(id)), nil
	case <-waits.Done():
		return cancelled(), nil
	case <-ctx.Done():
		return cancelled(), nil
	}
}

func selected(id sdk.PermissionOptionId) sdk.RequestPermissionResponse {
	return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{Selected: &sdk.RequestPermissionOutcomeSelected{OptionId: id}}}
}

func cancelled() sdk.RequestPermissionResponse {
	return sdk.RequestPermissionResponse{Outcome: sdk.RequestPermissionOutcome{Cancelled: &sdk.RequestPermissionOutcomeCancelled{}}}
}

// outside is the first path a changing tool call touches outside the write
// roots, or "".
func (s *Session) outside(tc ToolCall) string {
	if !slices.Contains([]string{"edit", "delete", "move"}, tc.Kind) {
		return ""
	}
	paths := slices.Clone(tc.Paths)
	for _, d := range tc.Diffs {
		paths = append(paths, d.Path)
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) || !within(p, s.opts.WriteRoots) {
			return p
		}
	}
	return ""
}

// ReadTextFile returns the editor's unsaved text for an open page, else the
// file, within the read roots.
func (c client) ReadTextFile(ctx context.Context, p sdk.ReadTextFileRequest) (sdk.ReadTextFileResponse, error) {
	s := c.s
	if !filepath.IsAbs(p.Path) || !within(p.Path, s.opts.ReadRoots) {
		return sdk.ReadTextFileResponse{}, fmt.Errorf("%s is outside %s", p.Path, strings.Join(s.opts.ReadRoots, ", "))
	}
	reply := make(chan readReply, 1)
	s.send(Read{Path: filepath.Clean(p.Path), reply: reply})
	var text string
	select {
	case r := <-reply:
		if r.open {
			text = r.text
			break
		}
		data, err := readWithin(p.Path, s.opts.ReadRoots)
		if err != nil {
			return sdk.ReadTextFileResponse{}, err
		}
		text = string(data)
	case <-ctx.Done():
		return sdk.ReadTextFileResponse{}, ctx.Err()
	}
	return sdk.ReadTextFileResponse{Content: slice(text, p.Line, p.Limit)}, nil
}

// readWithin reads a file through the root that holds it, so a symlink cannot
// lead outside.
func readWithin(path string, roots []string) ([]byte, error) {
	for _, r := range roots {
		rel, err := filepath.Rel(r, filepath.Clean(path))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		root, err := os.OpenRoot(r)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		return root.ReadFile(rel)
	}
	return nil, fmt.Errorf("%s is outside the read roots", path)
}

// slice is lines [line, line+limit) of text, 1-based; nil takes all.
func slice(text string, line, limit *int64) string {
	if line == nil && limit == nil {
		return text
	}
	lines := strings.SplitAfter(text, "\n")
	start := 0
	if line != nil && *line > 0 {
		start = int(min(*line-1, int64(len(lines))))
	}
	end := len(lines)
	if limit != nil && *limit >= 0 {
		end = int(min(int64(start)+*limit, int64(end)))
	}
	return strings.Join(lines[start:end], "")
}

// WriteTextFile has the editor write a file under a write root.
func (c client) WriteTextFile(ctx context.Context, p sdk.WriteTextFileRequest) (sdk.WriteTextFileResponse, error) {
	s := c.s
	if !filepath.IsAbs(p.Path) || !within(p.Path, s.opts.WriteRoots) {
		return sdk.WriteTextFileResponse{}, fmt.Errorf("gwiki writes only under %s, not %s", strings.Join(s.opts.WriteRoots, ", "), p.Path)
	}
	reply := make(chan error, 1)
	s.send(Write{Path: filepath.Clean(p.Path), Content: p.Content, reply: reply})
	select {
	case err := <-reply:
		return sdk.WriteTextFileResponse{}, err
	case <-ctx.Done():
		return sdk.WriteTextFileResponse{}, ctx.Err()
	}
}

// SessionUpdate turns the agent's updates into events.
func (c client) SessionUpdate(ctx context.Context, n sdk.SessionNotification) error {
	s, u := c.s, n.Update
	switch {
	case u.AgentMessageChunk != nil:
		if t := u.AgentMessageChunk.Content.Text; t != nil {
			s.send(Text{Text: t.Text})
		}
	case u.AgentThoughtChunk != nil:
		if t := u.AgentThoughtChunk.Content.Text; t != nil {
			s.send(Text{Thought: true, Text: t.Text})
		}
	case u.ToolCall != nil:
		t := u.ToolCall
		tc := &ToolCall{ID: string(t.ToolCallId), Title: t.Title, Kind: string(t.Kind), Status: string(t.Status)}
		if tc.Status == "" {
			tc.Status = "pending"
		}
		setContent(tc, t.Content, t.Locations)
		s.mu.Lock()
		s.tools[t.ToolCallId] = tc
		snapshot := copyTool(tc)
		s.mu.Unlock()
		s.send(snapshot)
	case u.ToolCallUpdate != nil:
		t := u.ToolCallUpdate
		s.mu.Lock()
		tc := s.tools[t.ToolCallId]
		if tc == nil {
			tc = &ToolCall{ID: string(t.ToolCallId), Status: "pending"}
			s.tools[t.ToolCallId] = tc
		}
		if v, ok := t.Title.Value(); ok {
			tc.Title = v
		}
		if v, ok := t.Kind.Value(); ok {
			tc.Kind = string(v)
		}
		if v, ok := t.Status.Value(); ok {
			tc.Status = string(v)
		}
		content, okc := t.Content.Value()
		locations, okl := t.Locations.Value()
		if okc || okl {
			if !okc {
				content = nil
			}
			if !okl {
				locations = nil
			}
			keep := *tc
			setContent(tc, content, locations)
			if !okc {
				tc.Diffs, tc.Output = keep.Diffs, keep.Output
			}
			if !okl {
				tc.Paths = keep.Paths
			}
		}
		snapshot := copyTool(tc)
		s.mu.Unlock()
		s.send(snapshot)
	case u.Plan != nil:
		var p Plan
		for _, e := range u.Plan.Entries {
			p.Entries = append(p.Entries, PlanEntry{Content: e.Content, Status: string(e.Status)})
		}
		s.send(p)
	case u.AvailableCommandsUpdate != nil:
		var cs Commands
		for _, cmd := range u.AvailableCommandsUpdate.AvailableCommands {
			cs.List = append(cs.List, Command{Name: cmd.Name, Description: cmd.Description})
		}
		s.send(cs)
	}
	return nil
}

func setContent(tc *ToolCall, content []sdk.ToolCallContent, locations []sdk.ToolCallLocation) {
	tc.Diffs, tc.Output, tc.Paths = nil, "", nil
	var out []string
	for _, c := range content {
		switch {
		case c.Diff != nil:
			d := Diff{Path: c.Diff.Path, New: c.Diff.NewText, Created: c.Diff.OldText == nil}
			if c.Diff.OldText != nil {
				d.Old = *c.Diff.OldText
			}
			tc.Diffs = append(tc.Diffs, d)
		case c.Content != nil && c.Content.Content.Text != nil:
			out = append(out, c.Content.Content.Text.Text)
		}
	}
	tc.Output = strings.Join(out, "\n")
	for _, l := range locations {
		tc.Paths = append(tc.Paths, l.Path)
	}
}

func copyTool(tc *ToolCall) ToolCall {
	c := *tc
	c.Paths, c.Diffs = slices.Clone(tc.Paths), slices.Clone(tc.Diffs)
	return c
}

// toolCallFrom is the tool call a permission request names, merged into the
// one already known.
func (s *Session) toolCallFrom(t sdk.ToolCallUpdate) ToolCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	tc := ToolCall{ID: string(t.ToolCallId)}
	if known := s.tools[t.ToolCallId]; known != nil {
		tc = copyTool(known)
	}
	if v, ok := t.Title.Value(); ok {
		tc.Title = v
	}
	if v, ok := t.Kind.Value(); ok {
		tc.Kind = string(v)
	}
	content, okc := t.Content.Value()
	locations, okl := t.Locations.Value()
	if okc || okl {
		keep := tc
		setContent(&tc, content, locations)
		if !okc {
			tc.Diffs, tc.Output = keep.Diffs, keep.Output
		}
		if !okl {
			tc.Paths = keep.Paths
		}
	}
	return tc
}

func (client) CreateTerminal(context.Context, sdk.CreateTerminalRequest) (sdk.CreateTerminalResponse, error) {
	return sdk.CreateTerminalResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalCreate)
}

func (client) KillTerminal(context.Context, sdk.KillTerminalRequest) (sdk.KillTerminalResponse, error) {
	return sdk.KillTerminalResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalKill)
}

func (client) ReleaseTerminal(context.Context, sdk.ReleaseTerminalRequest) (sdk.ReleaseTerminalResponse, error) {
	return sdk.ReleaseTerminalResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalRelease)
}

func (client) TerminalOutput(context.Context, sdk.TerminalOutputRequest) (sdk.TerminalOutputResponse, error) {
	return sdk.TerminalOutputResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalOutput)
}

func (client) WaitForTerminalExit(context.Context, sdk.WaitForTerminalExitRequest) (sdk.WaitForTerminalExitResponse, error) {
	return sdk.WaitForTerminalExitResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodTerminalWaitForExit)
}

func (client) CreateElicitation(context.Context, sdk.CreateElicitationRequest) (sdk.CreateElicitationResponse, error) {
	return sdk.CreateElicitationResponse{}, sdk.NewMethodNotFound(sdk.ClientMethodElicitationCreate)
}

func (client) CompleteElicitation(context.Context, sdk.CompleteElicitationNotification) error {
	return nil
}
