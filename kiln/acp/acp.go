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
	s := &session{agent: a, id: id, turn: make(chan struct{}, 1)}
	a.mu.Lock()
	a.sessions[id] = s
	a.mu.Unlock()
	return s, nil
}

// LoadSession restores a session minted by this process and replays
// that session's journaled conversation (its user and assistant turns)
// as message chunks before returning. The journal's chat timeline is
// shared by every ACP session and the panel, so only messages tagged
// with this session replay. Kiln sessions do not outlive the process;
// an ID from an earlier run reports resource-not-found.
func (a *Agent) LoadSession(ctx context.Context, sessionID, cwd string, out *acpcore.Client) (acpcore.Session, error) {
	a.mu.Lock()
	s, ok := a.sessions[sessionID]
	a.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("session %q: %w", sessionID, acpcore.ErrSessionNotFound)
	}
	// Build the replay under Live's read lock and write it after: a
	// client slow to read would otherwise hold the lock, and every
	// Apply in the process with it.
	var replay []acpcore.Update
	a.tools.Live().ReadSession(func(sess *journal.Session) {
		for _, ev := range sess.Chat {
			if ev.Message == nil || ev.Message.ACPSession != sessionID {
				continue
			}
			switch ev.Kind {
			case journal.KindChatUser:
				replay = append(replay, acpcore.UserMessageChunk(ev.EntryID, ev.Message.Text))
			case journal.KindChatAssistant:
				replay = append(replay, acpcore.AgentMessageChunk(ev.EntryID, ev.Message.Text))
			}
			// Tool calls and plans replay as tool frames on demand.
		}
	})
	for _, u := range replay {
		if err := out.Update(u); err != nil {
			break
		}
	}
	return s, nil
}

// session is one ACP conversation over the shared Kiln world.
type session struct {
	agent *Agent
	id    string

	// turn admits one prompt at a time. The ACP server serializes a
	// session's prompts on one connection, but a second connection can
	// load the same session and prompt beside the first.
	turn chan struct{}
	// history is the conversation the provider has seen across this
	// session's prompts, tool calls and results included. Read and
	// written only while holding turn. It always ends on a complete
	// exchange: every tool call in it has a result.
	history []agent.Message
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
	select {
	case s.turn <- struct{}{}:
	case <-ctx.Done():
		return acpcore.StopCancelled, nil
	}
	defer func() { <-s.turn }()
	if res := s.agent.tools.Chat(ctx, protocol.ChatArgs{Role: "user", Text: text, ACPSession: s.id}); !res.OK {
		return "", fmt.Errorf("journal user message: %s", res.Error)
	}

	if s.agent.provider == nil {
		_ = out.Update(acpcore.AgentMessageChunk(s.messageID(), noProviderNote))
		_ = s.agent.tools.Chat(context.Background(), protocol.ChatArgs{Role: "assistant", Text: noProviderNote, ACPSession: s.id})
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
	messages := append(s.history, agent.Message{Role: "user", Text: userText})
	defer func() { s.history = messages }()
	for turn := range maxTurns {
		if ctx.Err() != nil {
			return acpcore.StopCancelled, nil
		}
		system := agent.BuildPromptLive(s.agent.tools.Live(), s.agent.tools.List()).String()
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
		// A canceled prompt answers cancelled, so nothing may change
		// behind that answer: a provider that finished its request
		// after the cancel gets neither its text journaled nor its
		// tool calls run (AddEntity and friends ignore ctx).
		if ctx.Err() != nil {
			return acpcore.StopCancelled, nil
		}
		if err != nil {
			return "", fmt.Errorf("provider turn %d: %w", turn, err)
		}
		if t.Text != "" {
			if !streamed {
				_ = out.Update(acpcore.AgentMessageChunk(msgID, t.Text))
			}
			_ = s.agent.tools.Chat(ctx, protocol.ChatArgs{Role: "assistant", Text: t.Text, ACPSession: s.id})
		}
		messages = append(messages, agent.Message{Role: "assistant", Text: t.Text, ToolCalls: t.ToolCalls})
		if len(t.ToolCalls) == 0 {
			return acpcore.StopEndTurn, nil
		}
		for i, call := range t.ToolCalls {
			var res protocol.Result
			stop := ""
			if ctx.Err() != nil {
				stop = acpcore.StopCancelled
			} else {
				res, stop = s.runToolCall(ctx, call, out)
			}
			if stop != "" {
				// The calls that did not run still get results, so the
				// next prompt never sends a tool call without one.
				messages = append(messages, notRun(t.ToolCalls[i:])...)
				return stop, nil
			}
			messages = append(messages, agent.Message{Role: "tool_result", ToolUseID: call.CallID, Result: &res})
		}
	}
	return acpcore.StopMaxTurnRequests, nil
}

// notRun answers tool calls a stopped prompt never ran.
func notRun(calls []agent.ToolCall) []agent.Message {
	out := make([]agent.Message, 0, len(calls))
	for _, call := range calls {
		res := protocol.Result{OK: false, Kind: "cancelled", Error: "not run: the prompt was cancelled"}
		out = append(out, agent.Message{Role: "tool_result", ToolUseID: call.CallID, Result: &res})
	}
	return out
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

	// Every destructive-marked tool obtains human consent through
	// session/request_permission before it dispatches. approve_plan is
	// the obvious one, but the gate reads the descriptor's Destructive
	// flag, not a name list: reset_session carries the flag and used to
	// dispatch ungated, so a prompt-injected turn truncated the journal
	// — the only durable state — with no frame the user could veto,
	// while a one-field delete_field needed both a plan and this
	// round-trip. A rejection is fed back as a failed tool result so
	// the model sees the refusal.
	descriptor, known := s.agent.tools.Describe(call.Name)
	destructive := known && descriptor.Destructive
	if call.Name == "approve_plan" || destructive {
		allowed, stop := s.requestToolPermission(ctx, call, out)
		if stop != "" {
			return protocol.Result{}, stop
		}
		if !allowed {
			if call.Name == "approve_plan" {
				return protocol.Result{OK: false, Kind: "needs_plan", Error: "user rejected the plan", Hint: "propose a different plan or stop"}, ""
			}
			return protocol.Result{OK: false, Kind: "permission", Error: "user rejected " + call.Name, Hint: "ask the user to run " + call.Name + " from the operator panel"}, ""
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

// requestToolPermission runs the allow/reject round-trip for a gated
// tool call. It reports whether the user allowed it; the second return
// is a stop reason (only StopCancelled, when the request dies with the
// prompt's context).
func (s *session) requestToolPermission(ctx context.Context, call agent.ToolCall, out *acpcore.Client) (bool, string) {
	outcome, err := out.RequestPermission(ctx,
		acpcore.ToolCallUpdate{ToolCallID: call.CallID, Title: new("Approve " + strings.ReplaceAll(call.Name, "_", " "))},
		[]acpcore.PermissionOption{
			{OptionID: "allow-once", Name: "Allow once", Kind: acpcore.PermissionAllowOnce},
			{OptionID: "reject-once", Name: "Reject", Kind: acpcore.PermissionRejectOnce},
		})
	switch {
	case err != nil:
		if ctx.Err() != nil {
			return false, acpcore.StopCancelled
		}
		_ = out.Update(acpcore.ToolCallUpdateFrame(acpcore.ToolCallUpdate{
			ToolCallID: call.CallID, Status: new(acpcore.ToolStatusFailed),
			Content: []acpcore.ToolCallContent{acpcore.TextToolContent("permission request failed: " + err.Error())},
		}))
		return false, ""
	case outcome.Outcome == acpcore.OutcomeSelected && outcome.OptionID == "allow-once":
		return true, ""
	}
	_ = out.Update(acpcore.ToolCallUpdateFrame(acpcore.ToolCallUpdate{
		ToolCallID: call.CallID, Status: new(acpcore.ToolStatusFailed),
		Content: []acpcore.ToolCallContent{acpcore.TextToolContent("rejected by user")},
	}))
	return false, ""
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
