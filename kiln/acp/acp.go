// Package acp adapts Kiln's tool surface to the Agent Client Protocol
// via core/acp. The ACP agent runs prompt turns through an in-process
// kiln/agent.Provider when one is attached: text streams as
// agent_message_chunk updates, tool invocations surface as tool_call /
// tool_call_update frames, and approve_plan is gated on the human at
// the client through session/request_permission before it dispatches.
// Without a provider (the kiln CLI today), prompts are journaled and
// the turn refuses with a pointer at the transports that do drive
// tools.
//
// The kiln world is process-local, so session IDs are process-lifetime
// too: session/load replays a session minted by this process and
// reports unknown IDs as resource-not-found.
package acp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	acpcore "github.com/DonaldMurillo/gofastr/core/acp"
	"github.com/DonaldMurillo/gofastr/kiln/agent"
	"github.com/DonaldMurillo/gofastr/kiln/internal/kid"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// noProviderNote is streamed back when a prompt arrives and no model
// provider is attached. Refusing loudly beats accepting a prompt that
// can never be answered.
const noProviderNote = "kiln acp: no model provider is attached to this agent, so this prompt was only journaled. " +
	"Drive Kiln's tools over `kiln mcp`, the HTTP panel, or attach a provider with kiln/acp.WithProvider."

// Agent implements core/acp.Agent and core/acp.SessionLoader over
// kiln/protocol.Tools. Build one with New and hand it to
// acpcore.NewServer, or use NewServer.
type Agent struct {
	tools    *protocol.Tools
	provider agent.Provider
	maxTurns int

	mu       sync.Mutex
	sessions map[string]*session
}

// Option customizes an Agent.
type Option func(*Agent)

// WithProvider attaches the in-process model that runs prompt turns.
// Without one, prompts are journaled and the turn stops with the
// refusal reason.
func WithProvider(p agent.Provider) Option {
	return func(a *Agent) { a.provider = p }
}

// WithMaxTurns caps provider turns in one prompt; 0 means 16, matching
// agent.Loop.
func WithMaxTurns(n int) Option {
	return func(a *Agent) { a.maxTurns = n }
}

// New builds an Agent over a Tools surface. tools must be non-nil.
func New(tools *protocol.Tools, opts ...Option) *Agent {
	a := &Agent{tools: tools, sessions: map[string]*session{}}
	for _, o := range opts {
		o(a)
	}
	return a
}

// NewServer builds a core/acp server with all Kiln tools behind it,
// mirroring kiln/agent/mcp.NewServer.
func NewServer(tools *protocol.Tools, opts ...Option) *acpcore.Server {
	return acpcore.NewServer(New(tools, opts...), nil)
}

// Info identifies the agent in the initialize handshake.
func (a *Agent) Info() acpcore.Implementation {
	return acpcore.Implementation{Name: "kiln", Title: "Kiln", Version: "0.1.0"}
}

// NewSession mints a process-lifetime conversation.
func (a *Agent) NewSession(ctx context.Context, cwd string) (acpcore.Session, error) {
	if a.tools == nil {
		return nil, errors.New("kiln/acp: nil tools")
	}
	id := "kiln-" + kid.Hex(16)
	s := &session{agent: a, id: id}
	a.mu.Lock()
	a.sessions[id] = s
	a.mu.Unlock()
	return s, nil
}

// LoadSession restores a session minted by this process and replays
// the journaled conversation (user and assistant turns) as message
// chunks before returning. Kiln sessions do not outlive the process;
// an ID from an earlier run reports resource-not-found.
func (a *Agent) LoadSession(ctx context.Context, sessionID, cwd string, out *acpcore.Client) (acpcore.Session, error) {
	a.mu.Lock()
	s, ok := a.sessions[sessionID]
	a.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("session %q: %w", sessionID, acpcore.ErrSessionNotFound)
	}
	a.tools.Live().ReadSession(func(sess *journal.Session) {
		for _, ev := range sess.Chat {
			var u acpcore.Update
			switch ev.Kind {
			case journal.KindChatUser:
				u = acpcore.UserMessageChunk(ev.EntryID, ev.Message.Text)
			case journal.KindChatAssistant:
				u = acpcore.AgentMessageChunk(ev.EntryID, ev.Message.Text)
			default:
				continue // tool calls and plans replay as tool frames on demand
			}
			if err := out.Update(u); err != nil {
				return
			}
		}
	})
	return s, nil
}

// session is one ACP conversation over the shared Kiln world.
type session struct {
	agent *Agent
	id    string
}

func (s *session) ID() string { return s.id }

// Prompt runs one turn: journal the user message, then either refuse
// (no provider) or drive provider turns with streamed updates until
// the model stops calling tools.
func (s *session) Prompt(ctx context.Context, prompt []acpcore.ContentBlock, out *acpcore.Client) (string, error) {
	text := strings.TrimSpace(acpcore.PromptText(prompt))
	if text == "" {
		return acpcore.StopRefusal, nil
	}
	if res := s.agent.tools.Chat(ctx, protocol.ChatArgs{Role: "user", Text: text}); !res.OK {
		return "", fmt.Errorf("journal user message: %s", res.Error)
	}

	if s.agent.provider == nil {
		_ = out.Update(acpcore.AgentMessageChunk(s.messageID(), noProviderNote))
		_ = s.agent.tools.Chat(context.Background(), protocol.ChatArgs{Role: "assistant", Text: noProviderNote})
		return acpcore.StopRefusal, nil
	}
	return s.runTurns(ctx, text, out)
}

func (s *session) messageID() string {
	return "msg_" + kid.Hex(16)
}

// runTurns mirrors agent.Loop.Run with ACP streaming: each provider
// turn streams text chunks and executes tool calls as tool_call
// frames, until a turn requests no tools.
func (s *session) runTurns(ctx context.Context, userText string, out *acpcore.Client) (string, error) {
	maxTurns := s.agent.maxTurns
	if maxTurns <= 0 {
		maxTurns = 16
	}
	messages := []agent.Message{{Role: "user", Text: userText}}
	for turn := range maxTurns {
		if ctx.Err() != nil {
			return acpcore.StopCancelled, nil
		}
		system := agent.BuildPrompt(s.agent.tools.Live().Session(), s.agent.tools.List()).String()
		msgID := s.messageID()
		streamed := false
		req := agent.Request{
			System:   system,
			Messages: messages,
			Tools:    s.agent.tools.List(),
			OnEvent: func(ev agent.StreamEvent) {
				if ev.Kind == "text" && ev.Text != "" {
					streamed = true
					_ = out.Update(acpcore.AgentMessageChunk(msgID, ev.Text))
				}
			},
		}
		t, err := s.agent.provider.Stream(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return acpcore.StopCancelled, nil
			}
			return "", fmt.Errorf("provider turn %d: %w", turn, err)
		}
		if t.Text != "" {
			if !streamed {
				_ = out.Update(acpcore.AgentMessageChunk(msgID, t.Text))
			}
			_ = s.agent.tools.Chat(ctx, protocol.ChatArgs{Role: "assistant", Text: t.Text})
		}
		messages = append(messages, agent.Message{Role: "assistant", Text: t.Text, ToolCalls: t.ToolCalls})
		if len(t.ToolCalls) == 0 {
			return acpcore.StopEndTurn, nil
		}
		for _, call := range t.ToolCalls {
			res, stop := s.runToolCall(ctx, call, out)
			if stop != "" {
				return stop, nil
			}
			messages = append(messages, agent.Message{Role: "tool_result", ToolUseID: call.CallID, Result: &res})
		}
	}
	return acpcore.StopMaxTurnRequests, nil
}

// runToolCall executes one model tool call as a visible ACP tool call:
// report it pending, gate approve_plan on the user, run it, and report
// the outcome with its result as content.
func (s *session) runToolCall(ctx context.Context, call agent.ToolCall, out *acpcore.Client) (protocol.Result, string) {
	title, kind := toolCallPresentation(call.Name)
	_ = out.Update(acpcore.NewToolCall(acpcore.ToolCall{
		ToolCallID: call.CallID,
		Title:      title,
		Kind:       kind,
		Status:     acpcore.ToolStatusPending,
	}))

	// approve_plan is the single human gate over destructive edits:
	// the model may propose plans freely, but only the user at the ACP
	// client may approve one.
	if call.Name == "approve_plan" {
		allowed := false
		outcome, err := out.RequestPermission(ctx,
			acpcore.ToolCallUpdate{ToolCallID: call.CallID, Title: new("Approve plan")},
			[]acpcore.PermissionOption{
				{OptionID: "allow-once", Name: "Allow once", Kind: acpcore.PermissionAllowOnce},
				{OptionID: "reject-once", Name: "Reject", Kind: acpcore.PermissionRejectOnce},
			})
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return protocol.Result{}, acpcore.StopCancelled
			}
			_ = out.Update(acpcore.ToolCallUpdateFrame(acpcore.ToolCallUpdate{
				ToolCallID: call.CallID, Status: new(acpcore.ToolStatusFailed),
				Content: []acpcore.ToolCallContent{acpcore.TextToolContent("permission request failed: " + err.Error())},
			}))
			return protocol.Result{OK: false, Kind: "needs_plan", Error: "permission request failed: " + err.Error()}, ""
		case outcome.Outcome == acpcore.OutcomeSelected && outcome.OptionID == "allow-once":
			allowed = true
		}
		if !allowed {
			_ = out.Update(acpcore.ToolCallUpdateFrame(acpcore.ToolCallUpdate{
				ToolCallID: call.CallID, Status: new(acpcore.ToolStatusFailed),
				Content: []acpcore.ToolCallContent{acpcore.TextToolContent("rejected by user")},
			}))
			return protocol.Result{OK: false, Kind: "needs_plan", Error: "user rejected the plan", Hint: "propose a different plan or stop"}, ""
		}
	}

	_ = out.Update(acpcore.ToolCallUpdateFrame(acpcore.ToolCallUpdate{
		ToolCallID: call.CallID, Status: new(acpcore.ToolStatusInProgress),
	}))
	res := agent.Dispatch(ctx, s.agent.tools, call)

	status := acpcore.ToolStatusCompleted
	if !res.OK {
		status = acpcore.ToolStatusFailed
	}
	_ = out.Update(acpcore.ToolCallUpdateFrame(acpcore.ToolCallUpdate{
		ToolCallID: call.CallID,
		Status:     new(status),
		Content:    []acpcore.ToolCallContent{acpcore.TextToolContent(resultText(res))},
	}))
	return res, ""
}

// resultText renders a tool result for the tool_call_update content:
// the payload on success, error + hint + kind on failure.
func resultText(res protocol.Result) string {
	if res.OK {
		if res.Result == nil {
			return "ok"
		}
		return fmt.Sprintf("%v", res.Result)
	}
	out := "error: " + res.Error
	if res.Kind != "" {
		out += " (kind: " + res.Kind + ")"
	}
	if res.Hint != "" {
		out += "\nhint: " + res.Hint
	}
	return out
}

// toolCallPresentation maps a Kiln tool name to a human title and an
// ACP tool kind for client display.
func toolCallPresentation(name string) (title, kind string) {
	title = strings.ReplaceAll(name, "_", " ")
	switch {
	case name == "world_get":
		return title, acpcore.ToolKindRead
	case name == "propose_plan":
		return title, acpcore.ToolKindThink
	case strings.HasPrefix(name, "delete_"):
		return title, acpcore.ToolKindDelete
	case strings.HasPrefix(name, "add_"), strings.HasPrefix(name, "update_"), strings.HasPrefix(name, "set_"):
		return title, acpcore.ToolKindEdit
	default:
		return title, acpcore.ToolKindOther
	}
}
