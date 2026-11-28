// Package acptest is a stand-in ACP agent for tests. A test binary runs it in
// place of the tests when MaybeRun finds its environment variable set, so a
// test can start "the agent" as its own executable.
//
// Each prompt's text scripts the turn:
//
//	hello              two message chunks, a thought, a plan and commands
//	read <path>        reads the file through the client, and says its text
//	write <path> <t>   writes t through the client, and says ok or the error
//	edit <path>        asks permission for an edit of path, and says the choice
//	wait               waits until cancelled
package acptest

import (
	"context"
	"fmt"
	"os"
	"strings"

	sdk "github.com/coder/acp-go-sdk"
)

// Env is the variable that makes a test binary the stand-in agent.
const Env = "GWIKI_ACPTEST_AGENT"

// MaybeRun runs the stand-in agent and exits when Env is set; call it first in
// TestMain.
func MaybeRun() {
	if os.Getenv(Env) == "" {
		return
	}
	a := &agent{}
	a.conn = sdk.NewAgentSideConnection(a, os.Stdout, os.Stdin)
	<-a.conn.Done()
	os.Exit(0)
}

type agent struct {
	conn *sdk.AgentSideConnection
	mcp  []sdk.McpServer
}

func (a *agent) Initialize(ctx context.Context, p sdk.InitializeRequest) (sdk.InitializeResponse, error) {
	return sdk.InitializeResponse{
		ProtocolVersion: sdk.ProtocolVersionNumber,
		AgentInfo:       &sdk.Implementation{Name: "stand-in", Version: "1"},
		AuthMethods:     []sdk.AuthMethod{},
	}, nil
}

func (a *agent) NewSession(ctx context.Context, p sdk.NewSessionRequest) (sdk.NewSessionResponse, error) {
	a.mcp = p.McpServers
	return sdk.NewSessionResponse{SessionId: "s1"}, nil
}

func (a *agent) say(ctx context.Context, text string) {
	_ = a.conn.SessionUpdate(ctx, sdk.SessionNotification{SessionId: "s1", Update: sdk.UpdateAgentMessageText(text)})
}

func (a *agent) Prompt(ctx context.Context, p sdk.PromptRequest) (sdk.PromptResponse, error) {
	var text []string
	var links []string
	for _, b := range p.Prompt {
		switch {
		case b.Text != nil:
			text = append(text, b.Text.Text)
		case b.ResourceLink != nil:
			links = append(links, b.ResourceLink.Uri)
		}
	}
	words := strings.Fields(strings.Join(text, " "))
	if len(words) == 0 {
		return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
	}
	switch words[0] {
	case "hello":
		_ = a.conn.SessionUpdate(ctx, sdk.SessionNotification{SessionId: "s1", Update: sdk.UpdateAgentThoughtText("pondering")})
		a.say(ctx, "Hi ")
		a.say(ctx, "there; links: "+strings.Join(links, ",")+"; mcp: "+mcpLine(a.mcp))
		_ = a.conn.SessionUpdate(ctx, sdk.SessionNotification{SessionId: "s1", Update: sdk.UpdatePlan(
			sdk.PlanEntry{Content: "look", Status: sdk.PlanEntryStatusCompleted, Priority: sdk.PlanEntryPriorityMedium},
			sdk.PlanEntry{Content: "write", Status: sdk.PlanEntryStatusPending, Priority: sdk.PlanEntryPriorityMedium},
		)})
		_ = a.conn.SessionUpdate(ctx, sdk.SessionNotification{SessionId: "s1", Update: sdk.SessionUpdate{AvailableCommandsUpdate: &sdk.SessionAvailableCommandsUpdate{
			AvailableCommands: []sdk.AvailableCommand{{Name: "review", Description: "review the page"}},
		}}})
	case "read":
		r, err := a.conn.ReadTextFile(ctx, sdk.ReadTextFileRequest{SessionId: "s1", Path: arg(words, 1)})
		if err != nil {
			a.say(ctx, "error: "+err.Error())
		} else {
			a.say(ctx, "read: "+r.Content)
		}
	case "write":
		_, err := a.conn.WriteTextFile(ctx, sdk.WriteTextFileRequest{SessionId: "s1", Path: arg(words, 1), Content: strings.Join(words[min(2, len(words)):], " ") + "\n"})
		if err != nil {
			a.say(ctx, "error: "+err.Error())
		} else {
			a.say(ctx, "ok")
		}
	case "edit":
		path := arg(words, 1)
		_ = a.conn.SessionUpdate(ctx, sdk.SessionNotification{SessionId: "s1", Update: sdk.StartToolCall("t1", "Edit "+path,
			sdk.WithStartKind(sdk.ToolKindEdit), sdk.WithStartLocations([]sdk.ToolCallLocation{{Path: path}}))})
		old := "a\n"
		r, err := a.conn.RequestPermission(ctx, sdk.RequestPermissionRequest{
			SessionId: "s1",
			ToolCall: sdk.ToolCallUpdate{ToolCallId: "t1", Content: sdk.PatchSet([]sdk.ToolCallContent{
				{Diff: &sdk.ToolCallContentDiff{Path: path, OldText: &old, NewText: "b\n"}},
			})},
			Options: []sdk.PermissionOption{
				{OptionId: "yes", Name: "Allow", Kind: sdk.PermissionOptionKindAllowOnce},
				{OptionId: "no", Name: "Reject", Kind: sdk.PermissionOptionKindRejectOnce},
			},
		})
		switch {
		case ctx.Err() != nil:
			return sdk.PromptResponse{StopReason: sdk.StopReasonCancelled}, nil
		case err != nil:
			a.say(ctx, "error: "+err.Error())
		case r.Outcome.Selected != nil:
			a.say(ctx, "chose "+string(r.Outcome.Selected.OptionId))
		default:
			a.say(ctx, "cancelled")
		}
		_ = a.conn.SessionUpdate(ctx, sdk.SessionNotification{SessionId: "s1", Update: sdk.UpdateToolCall("t1", sdk.WithUpdateStatus(sdk.ToolCallStatusCompleted))})
	case "wait":
		<-ctx.Done()
		return sdk.PromptResponse{StopReason: sdk.StopReasonCancelled}, nil
	}
	return sdk.PromptResponse{StopReason: sdk.StopReasonEndTurn}, nil
}

func mcpLine(servers []sdk.McpServer) string {
	var out []string
	for _, s := range servers {
		if s.Stdio != nil {
			out = append(out, s.Stdio.Name+"="+s.Stdio.Command+" "+strings.Join(s.Stdio.Args, " "))
		}
	}
	return strings.Join(out, ",")
}

func arg(words []string, i int) string {
	if i < len(words) {
		return words[i]
	}
	return ""
}

func (a *agent) Cancel(ctx context.Context, p sdk.CancelNotification) error { return nil }

func (a *agent) Authenticate(ctx context.Context, p sdk.AuthenticateRequest) (sdk.AuthenticateResponse, error) {
	return sdk.AuthenticateResponse{}, nil
}

func (a *agent) Logout(ctx context.Context, p sdk.LogoutRequest) (sdk.LogoutResponse, error) {
	return sdk.LogoutResponse{}, nil
}

func (a *agent) CloseSession(ctx context.Context, p sdk.CloseSessionRequest) (sdk.CloseSessionResponse, error) {
	return sdk.CloseSessionResponse{}, nil
}

func (a *agent) DeleteSession(ctx context.Context, p sdk.DeleteSessionRequest) (sdk.DeleteSessionResponse, error) {
	return sdk.DeleteSessionResponse{}, nil
}

func (a *agent) ListSessions(ctx context.Context, p sdk.ListSessionsRequest) (sdk.ListSessionsResponse, error) {
	return sdk.ListSessionsResponse{}, nil
}

func (a *agent) ResumeSession(ctx context.Context, p sdk.ResumeSessionRequest) (sdk.ResumeSessionResponse, error) {
	return sdk.ResumeSessionResponse{}, fmt.Errorf("not supported")
}

func (a *agent) SetSessionConfigOption(ctx context.Context, p sdk.SetSessionConfigOptionRequest) (sdk.SetSessionConfigOptionResponse, error) {
	return sdk.SetSessionConfigOptionResponse{}, nil
}

func (a *agent) SetSessionMode(ctx context.Context, p sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error) {
	return sdk.SetSessionModeResponse{}, nil
}
