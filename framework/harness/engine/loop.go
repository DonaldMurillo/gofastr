package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Engine is the agent loop bound to one EngineRun. It owns:
//
//   - the per-session event bus
//   - the tool dispatcher (which owns the registry reference)
//   - the request middleware chain wrapping Provider.Chat
//   - the conversation history (in-memory; persisted by session.Store
//     via an event subscriber)
//
// One Engine runs one session. Multiple engines coexist in one
// harness process; the multiplexer routes commands to the right one.
type Engine struct {
	Session  ids.SessionID
	Bus      *Bus
	Provider provider.Provider
	Model    string

	Dispatcher *Dispatcher
	Middleware []RequestMiddleware

	// History is the canonical-form message list. The loop appends
	// to this list as turns complete; middleware can read it via
	// Request.Messages.
	History []provider.Message

	// Tools snapshot for the next Request. Refreshed when the
	// registry changes.
	Tools []provider.ToolSchema

	// Tree owns cancellation for the active turn. Child turns and
	// child engines branch off this.
	Tree *CancelTree
}

// NewEngine constructs an Engine. Caller is responsible for wiring
// Provider, Model, Dispatcher, and Middleware before calling Run.
func NewEngine(session ids.SessionID, bus *Bus, p provider.Provider, model string, d *Dispatcher) *Engine {
	return &Engine{
		Session:    session,
		Bus:        bus,
		Provider:   p,
		Model:      model,
		Dispatcher: d,
		Tree:       NewCancelTree(context.Background()),
	}
}

// RunTurn processes a single turn:
//
//  1. Append the input as a user message.
//  2. Build a Request and pass through the request middleware chain.
//  3. Send to Provider.Chat; collect the stream into a summary.
//  4. If the model emitted tool_use blocks, dispatch each through
//     the tool middleware chain and append the results as a
//     tool_result-bearing user message. Loop back to step 2.
//  5. Otherwise yield with TurnEnded.
//
// originator is the Client that sent the SendInput.
//
// Cancellation: ctx (typically derived from e.Tree.Context()) is
// honored at every await point; partial state is emitted as best
// effort.
func (e *Engine) RunTurn(ctx context.Context, originator ids.ClientID, input []control.ContentBlock) error {
	turnNo := countTurns(e.History) + 1
	if _, err := e.Bus.Publish(control.TurnStarted{
		Turn:       turnNo,
		Originator: originator,
		Content:    input,
	}, originator); err != nil {
		return err
	}
	componentTimings := map[string]time.Duration{}
	var toolTimings []control.ToolTimingEntry

	// Append the user input.
	e.History = append(e.History, provider.Message{Role: provider.RoleUser, Content: input})

	// Inner loop: keep cycling provider → tools → provider until the
	// model emits no tool_use. Hard-capped at maxInnerLoopIterations
	// so a model that gets stuck ping-ponging tool calls (the
	// runaway-Agent pattern from sess_01KSDZK5) can't burn the whole
	// session. Hit the cap → publish Error + close the turn.
	innerIter := 0
	cap := maxInnerLoopIterations
	if isSubAgentCtx(ctx) {
		cap = SubAgentMaxIterations
	}
	for {
		innerIter++
		if innerIter > cap {
			_, _ = e.Bus.Publish(control.Error{
				Reason: "IterationLimit",
				Message: fmt.Sprintf("inner loop exceeded %d provider rounds — turn aborted to avoid runaway tool calls",
					cap),
			}, originator)
			e.publishTurnEnd(turnNo, "iteration_limit", originator, componentTimings, toolTimings)
			return nil
		}
		if err := ctx.Err(); err != nil {
			e.publishTurnEnd(turnNo, "cancelled", originator, componentTimings, toolTimings)
			return err
		}

		req := &provider.Request{
			Model:    e.Model,
			Messages: append([]provider.Message{}, e.History...),
			Tools:    e.Tools,
		}

		// Build the request middleware chain.
		base := func(ctx context.Context, r *provider.Request) (<-chan provider.StreamEvent, error) {
			return e.Provider.Chat(ctx, r)
		}
		handler := ChainRequest(base, e.Middleware...)

		providerStart := time.Now()
		stream, err := handler(ctx, req)
		if err != nil {
			_, _ = e.Bus.Publish(control.Error{
				Reason:  control.ReasonInvalidCommand,
				Message: err.Error(),
			}, originator)
			e.publishTurnEnd(turnNo, "error", originator, componentTimings, toolTimings)
			return err
		}

		summary, err := CollectStream(ctx, e.Bus, originator, stream)
		componentTimings["provider.chat"] += time.Since(providerStart)
		if err != nil {
			e.publishTurnEnd(turnNo, "error", originator, componentTimings, toolTimings)
			return err
		}

		// Cost accounting: emit CostIncremented if the provider
		// reported usage. USD is computed from the model's static
		// pricing table when available, else 0 (unknown provider).
		if summary.Usage.InputTokens > 0 || summary.Usage.OutputTokens > 0 {
			usd := 0.0
			if p, ok := provider.PricingForModel(e.Provider.Name(), e.Model); ok {
				usd = provider.USDForUsage(p, summary.Usage)
			}
			_, _ = e.Bus.Publish(control.CostIncremented{
				Provider:     e.Provider.Name(),
				Model:        e.Model,
				InputTokens:  summary.Usage.InputTokens,
				OutputTokens: summary.Usage.OutputTokens,
				CacheTokens:  summary.Usage.CacheReadTokens,
				USD:          usd,
			}, originator)
		}

		// Build the assistant message from the summary and append.
		assistantContent := assistantContentFromSummary(summary)
		e.History = append(e.History, provider.Message{
			Role:    provider.RoleAssistant,
			Content: assistantContent,
		})

		// If no tool_uses, the turn is done.
		if len(summary.ToolUses) == 0 {
			reason := "complete"
			if summary.FinishReason == "length" {
				reason = "length"
			} else if summary.FinishReason == "yield" {
				reason = "yield"
			}
			// Empty-response detection: a model that returns with no
			// text, no thinking, and no tool calls looks identical to
			// "done" in the wire stream, but the user has no idea
			// anything ran. Surface it as an Error so clients can
			// show a real signal instead of a frozen UI.
			if summary.Text == "" && len(summary.Thinking) == 0 {
				_, _ = e.Bus.Publish(control.Error{
					Reason:  "EmptyResponse",
					Message: "model returned no content (finish_reason=" + summary.FinishReason + ")",
				}, originator)
				reason = "empty"
			}
			e.publishTurnEnd(turnNo, reason, originator, componentTimings, toolTimings)
			return nil
		}

		// Dispatch tool calls. When the model returns N tool_uses in
		// a single response, fan them out concurrently — the user
		// expected sub-agents to run in parallel (sess_01KSE5GG…
		// showed 3 sequential 60-second Agent calls instead of one
		// 60-second parallel batch). Order is preserved via the
		// pre-sized results slice.
		results := make([]control.ContentBlock, len(summary.ToolUses))
		timings := make([]control.ToolTimingEntry, len(summary.ToolUses))
		callCtx := tool.WithSession(ctx, e.Session)
		callCtx = tool.WithSpawner(callCtx, e)
		callCtx = tool.WithRegistry(callCtx, e.Dispatcher.Registry())
		var wg sync.WaitGroup
		for i, tu := range summary.ToolUses {
			wg.Add(1)
			go func(i int, tu control.ToolUse) {
				defer wg.Done()
				callID, err := callIDFromToolUseID(tu.ID)
				if err != nil {
					callID = ids.NewCallID()
				}
				tStart := time.Now()
				tr, dErr := e.Dispatcher.Dispatch(callCtx, originator, tool.ToolCall{
					ID:    callID,
					Name:  tu.Name,
					Input: tu.Input,
				})
				timings[i] = control.ToolTimingEntry{
					CallID:   callID,
					Tool:     tu.Name,
					Duration: time.Since(tStart),
				}
				if dErr != nil {
					_, _ = e.Bus.Publish(control.Error{
						Reason:  control.ReasonInvalidCommand,
						Message: dErr.Error(),
					}, originator)
					results[i] = control.ContentBlock{
						Type: "tool_result",
						ToolResult: &control.ToolResultBlk{
							ToolUseID: tu.ID,
							Content:   []control.ContentBlock{{Type: "text", Text: dErr.Error()}},
							IsError:   true,
						},
					}
					return
				}
				results[i] = control.ContentBlock{
					Type: "tool_result",
					ToolResult: &control.ToolResultBlk{
						ToolUseID: tu.ID,
						Content:   capToolResultContent(tr.Content),
						IsError:   tr.IsError,
					},
				}
			}(i, tu)
		}
		wg.Wait()
		toolTimings = append(toolTimings, timings...)
		// Tool results come back as a user-role message (Anthropic-shape).
		e.History = append(e.History, provider.Message{
			Role:    provider.RoleUser,
			Content: results,
		})
		// Loop back to send to the provider again.
	}
}

func (e *Engine) publishTurnEnd(
	turn int, reason string, originator ids.ClientID,
	components map[string]time.Duration, toolTimings []control.ToolTimingEntry,
) {
	_, _ = e.Bus.Publish(control.TurnEnded{Turn: turn, Reason: reason}, originator)
	_, _ = e.Bus.Publish(control.TurnTiming{
		Turn:       turn,
		Components: components,
		ToolCalls:  toolTimings,
	}, originator)
}

// assistantContentFromSummary reconstructs the assistant message
// content blocks from a StreamSummary, preserving text + tool_uses +
// thinking in their wire-format shapes.
func assistantContentFromSummary(s StreamSummary) []control.ContentBlock {
	var out []control.ContentBlock
	if s.Text != "" {
		out = append(out, control.ContentBlock{Type: "text", Text: s.Text})
	}
	for _, tu := range s.ToolUses {
		c := tu // copy
		out = append(out, control.ContentBlock{Type: "tool_use", ToolUse: &c})
	}
	for _, th := range s.Thinking {
		out = append(out, control.ContentBlock{Type: "thinking", Thinking: th})
	}
	return out
}

// Spawn runs a sub-agent inside this engine's environment: same
// Provider, Model, Tools, and Dispatcher, but a FRESH conversation
// history. Used by the Agent tool. systemHint is prepended as an
// extra system message on the sub-conversation only; userPrompt is
// the initial user turn. Returns the final assistant text after the
// sub-loop terminates (or empty string on error).
//
// Sub-agent events flow on the SAME bus so all attached clients can
// observe what the sub-agent does in real time. The parent and child
// turn numbers share the same monotonic counter; this is acceptable
// because a sub-agent is conceptually one large tool call.
func (e *Engine) Spawn(ctx context.Context, systemHint, userPrompt string) (string, error) {
	// Sub-engine gets a PRIVATE event bus so its intermediate events
	// (thinking, tool calls, text deltas) don't bleed into the
	// parent chat. The Agent tool surfaces just the final summary
	// via its ToolResult. This matches CC's Task tool behavior:
	// user sees the Agent card + its single-line result, NOT the
	// sub-agent's whole stream-of-consciousness.
	subBus := NewBus(e.Session)
	// New dispatcher pointed at the private bus so tool calls
	// dispatched by the sub-agent also stay private.
	subDisp := NewDispatcher(subBus, e.Dispatcher.Registry(), e.Dispatcher.mws...)
	sub := &Engine{
		Session:    e.Session,
		Bus:        subBus,
		Provider:   e.Provider,
		Model:      e.Model,
		Dispatcher: subDisp,
		Middleware: e.Middleware,
		Tools:      stripMetaTools(e.Tools),
		Tree:       NewCancelTree(ctx),
		History:    nil, // fresh
	}
	defer subBus.Close()
	// Always prepend the focus hint, then any caller-supplied hint.
	// The hint constrains the sub-agent to focused, bounded work —
	// without it sub-agents tend to fire 20+ tool calls trying to
	// be helpful (sess_01KSE3JKR…).
	sub.History = append(sub.History, provider.Message{
		Role:    provider.RoleSystem,
		Content: []control.ContentBlock{{Type: "text", Text: subAgentFocusHint}},
	})
	if systemHint != "" {
		sub.History = append(sub.History, provider.Message{
			Role:    provider.RoleSystem,
			Content: []control.ContentBlock{{Type: "text", Text: systemHint}},
		})
	}
	originator := ids.NewClientID()
	// Increment spawn depth so the Agent tool, when reached from
	// inside this sub-loop, can refuse further recursion past
	// tool.MaxSpawnDepth. Also tag the ctx so RunTurn knows to apply
	// the tighter sub-agent iteration cap.
	subCtx := tool.WithSpawnDepth(ctx, tool.SpawnDepthFromContext(ctx)+1)
	subCtx = withSubAgentMark(subCtx)
	if err := sub.RunTurn(subCtx, originator, []control.ContentBlock{
		{Type: "text", Text: userPrompt},
	}); err != nil {
		return "", err
	}
	// Walk back through history to find the last assistant text.
	for i := len(sub.History) - 1; i >= 0; i-- {
		m := sub.History[i]
		if m.Role != provider.RoleAssistant {
			continue
		}
		var b strings.Builder
		for _, blk := range m.Content {
			if blk.Type == "text" {
				b.WriteString(blk.Text)
			}
		}
		if b.Len() > 0 {
			return b.String(), nil
		}
	}
	return "", nil
}

// subAgentCtxKey marks a context as belonging to a sub-agent turn
// so RunTurn can apply the tighter SubAgentMaxIterations cap.
type subAgentCtxKey struct{}

func withSubAgentMark(ctx context.Context) context.Context {
	return context.WithValue(ctx, subAgentCtxKey{}, true)
}

func isSubAgentCtx(ctx context.Context) bool {
	v, _ := ctx.Value(subAgentCtxKey{}).(bool)
	return v
}

// metaToolNames are tools that orchestrate the agent loop itself.
// We hide them from sub-agents so a sub-agent can't re-plan the
// parent's TaskList or recursively chain more Agent spawns. The
// registry still has them — but the model never sees the schema
// in its sub-agent context, so it won't call them.
var metaToolNames = map[string]bool{
	"TaskList": true,
	"Agent":    true,
}

// stripMetaTools returns a copy of `in` minus any tool whose Name
// is in metaToolNames. Pure function so tests can pin the contract.
func stripMetaTools(in []provider.ToolSchema) []provider.ToolSchema {
	out := make([]provider.ToolSchema, 0, len(in))
	for _, t := range in {
		if metaToolNames[t.Name] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// maxInnerLoopIterations bounds the number of provider rounds inside
// a single RunTurn. Each iteration is one Provider.Chat call followed
// by optional tool dispatches. 25 is generous for normal multi-tool
// turns but stops runaway recursion (esp. via the Agent tool) before
// it burns tokens uncontrollably.
const maxInnerLoopIterations = 25

// SubAgentMaxIterations is a tighter cap for sub-agent turns
// (Engine.Spawn). Sub-agents are "do one focused thing" workers —
// they should NOT do 20 provider rounds the way a top-level
// coordinator turn might. 8 leaves room for: plan → 3-4 tool rounds
// → wrap-up. Anything more is the model getting lost.
const SubAgentMaxIterations = 8

// subAgentFocusHint is auto-prepended to every sub-agent's history.
// Keeps the sub-agent on-task without the user having to remember to
// pass `systemHint`. Discovered necessary after sess_01KSE3JKR…
// where sub-agents made 19+ WebFetches each because the system
// prompt didn't tell them to stay focused.
const subAgentFocusHint = "You are a focused sub-agent. Complete ONLY the specific task the parent asked for and return a concise summary (1-3 short paragraphs). Use at most 3-4 tool calls. Don't over-explore, don't re-plan, don't ask for clarification."

// maxToolResultBytesPerBlock is the engine-level defensive cap on
// any single text block inside a tool result. Tools SHOULD truncate
// themselves to a sane size, but this guards against any tool that
// forgets — a single 500KB tool result can silently exceed the
// model's context window and cause an empty next-turn response.
const maxToolResultBytesPerBlock = 64 << 10 // 64 KiB

// CapToolResultContent (exported for tests) enforces
// maxToolResultBytesPerBlock on each text block. Larger blocks are
// truncated with a clear suffix so the model knows content was elided.
func CapToolResultContent(blocks []control.ContentBlock) []control.ContentBlock {
	return capToolResultContent(blocks)
}

func capToolResultContent(blocks []control.ContentBlock) []control.ContentBlock {
	out := make([]control.ContentBlock, len(blocks))
	for i, b := range blocks {
		out[i] = b
		if b.Type == "text" && len(b.Text) > maxToolResultBytesPerBlock {
			out[i].Text = b.Text[:maxToolResultBytesPerBlock] +
				fmt.Sprintf("\n\n[... engine-capped at %d bytes; tool returned %d total ...]",
					maxToolResultBytesPerBlock, len(b.Text))
		}
	}
	return out
}

// countTurns is the number of completed user turns visible in
// history. A "turn" begins with a user input message; subsequent
// tool_result messages (also role=user in Anthropic's shape) are
// part of the same turn and don't bump the counter. This fixes the
// turn-number leap (e.g. 4 → 7) that happened when a turn made
// multiple LLM rounds — previously every assistant round counted as
// a turn, so countTurns + 1 ran ahead of reality.
func countTurns(h []provider.Message) int {
	n := 0
	for _, m := range h {
		if m.Role != provider.RoleUser {
			continue
		}
		// tool_result responses are user-role envelopes but not
		// genuine user input.
		if len(m.Content) > 0 && m.Content[0].Type == "tool_result" {
			continue
		}
		n++
	}
	return n
}

// callIDFromToolUseID maps a provider-supplied tool_use id to a CallID.
// If the provider's id is already a typed ULID, use it; otherwise
// callers fall back to a fresh CallID.
func callIDFromToolUseID(s string) (ids.CallID, error) {
	if id, err := ids.ParseCall(s); err == nil {
		return id, nil
	}
	return "", errors.New("not a CallID")
}

// SimpleInput converts a plain text string into a SendInput's content.
// Convenience for callers that don't need multi-block input.
func SimpleInput(text string) []control.ContentBlock {
	return []control.ContentBlock{{Type: "text", Text: text}}
}

// FormatMessages is a developer helper for printing history during
// debugging. Not used at runtime.
func FormatMessages(h []provider.Message) string {
	b := make([]byte, 0, 256)
	for _, m := range h {
		b = append(b, []byte(string(m.Role)+": ")...)
		blob, _ := json.Marshal(m.Content)
		b = append(b, blob...)
		b = append(b, '\n')
	}
	return string(b)
}

// String returns a developer-friendly representation of an Engine.
func (e *Engine) String() string {
	return fmt.Sprintf("Engine{session=%s, provider=%s, model=%s, turns=%d}",
		e.Session, e.Provider.Name(), e.Model, countTurns(e.History))
}
