package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Engine runs the agent loop for one session.
type Engine struct {
	Session  ids.SessionID
	Bus      *Bus
	Provider provider.Provider
	Model    string

	Dispatcher *Dispatcher
	Middleware []RequestMiddleware

	// History is the conversation so far, in provider message form.
	History []provider.Message

	// Tools is the tool catalog sent with every Request.
	Tools []provider.ToolSchema

	// Tree is the root cancellation node every turn's context derives from.
	Tree *CancelTree

	// OnTurnEnd, when set, runs just before TurnEnded is published.
	OnTurnEnd func()

	// MaxStreamBytesPerTurn caps one provider turn's buffered bytes; zero means DefaultMaxStreamBytesPerTurn.
	MaxStreamBytesPerTurn int
}

// NewEngine returns an Engine with a fresh root CancelTree.
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

// RunTurn sends input to the provider and dispatches tool calls until the model stops calling tools, publishing every step to the bus.
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

	e.History = append(e.History, provider.Message{Role: provider.RoleUser, Content: input})

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
				Message: fmt.Sprintf("inner loop exceeded %d provider rounds: turn aborted to avoid runaway tool calls",
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

		base := func(ctx context.Context, r *provider.Request) (<-chan provider.StreamEvent, error) {
			return e.Provider.Chat(ctx, r)
		}
		handler := ChainRequest(base, e.Middleware...)

		providerStart := time.Now()
		stream, err := handler(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				e.publishTurnEnd(turnNo, "cancelled", originator, componentTimings, toolTimings)
				return err
			}
			_, _ = e.Bus.Publish(control.Error{
				Reason:  control.ReasonInvalidCommand,
				Message: err.Error(),
			}, originator)
			e.publishTurnEnd(turnNo, "error", originator, componentTimings, toolTimings)
			return err
		}

		summary, err := CollectStreamWithCap(ctx, e.Bus, originator, stream, e.MaxStreamBytesPerTurn)
		componentTimings["provider.chat"] += time.Since(providerStart)
		if err != nil {
			reason := "error"
			if ctx.Err() != nil {
				reason = "cancelled"
			}
			e.publishTurnEnd(turnNo, reason, originator, componentTimings, toolTimings)
			return err
		}

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

		assistantContent := assistantContentFromSummary(summary)
		e.History = append(e.History, provider.Message{
			Role:    provider.RoleAssistant,
			Content: assistantContent,
		})

		if len(summary.ToolUses) == 0 {
			reason := "complete"
			if summary.FinishReason == "length" {
				reason = "length"
			} else if summary.FinishReason == "yield" {
				reason = "yield"
			}
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
		e.History = append(e.History, provider.Message{
			Role:    provider.RoleUser,
			Content: results,
		})
	}
}

func (e *Engine) publishTurnEnd(
	turn int, reason string, originator ids.ClientID,
	components map[string]time.Duration, toolTimings []control.ToolTimingEntry,
) {
	if e.OnTurnEnd != nil {
		e.OnTurnEnd()
	}
	_, _ = e.Bus.Publish(control.TurnEnded{Turn: turn, Reason: reason}, originator)
	_, _ = e.Bus.Publish(control.TurnTiming{
		Turn:       turn,
		Components: components,
		ToolCalls:  toolTimings,
	}, originator)
}

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

// Spawn runs userPrompt as a sub-agent with a fresh history on a private bus and returns its last assistant text.
func (e *Engine) Spawn(ctx context.Context, systemHint, userPrompt string) (string, error) {
	originator := ids.NewClientID()
	subBus := NewSubBus(e.Bus, string(originator))
	subDisp := NewDispatcher(subBus, e.Dispatcher.Registry(), e.Dispatcher.mws...)
	subDisp.refused = metaToolNames
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
	subCtx := tool.WithSpawnDepth(ctx, tool.SpawnDepthFromContext(ctx)+1)
	subCtx = withSubAgentMark(subCtx)
	if err := sub.RunTurn(subCtx, originator, []control.ContentBlock{
		{Type: "text", Text: userPrompt},
	}); err != nil {
		return "", err
	}
	for _, m := range slices.Backward(sub.History) {

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

type subAgentCtxKey struct{}

func withSubAgentMark(ctx context.Context) context.Context {
	return context.WithValue(ctx, subAgentCtxKey{}, true)
}

func isSubAgentCtx(ctx context.Context) bool {
	v, _ := ctx.Value(subAgentCtxKey{}).(bool)
	return v
}

var metaToolNames = map[string]bool{
	"TaskList": true,
	"Agent":    true,
}

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

const maxInnerLoopIterations = 25

// SubAgentMaxIterations caps the provider rounds of one sub-agent turn.
const SubAgentMaxIterations = 8

const subAgentFocusHint = "You are a focused sub-agent. Complete ONLY the specific task the parent asked for and return a concise summary (1-3 short paragraphs). Use at most 3-4 tool calls. Don't over-explore, don't re-plan, don't ask for clarification."

const maxToolResultBytesPerBlock = 64 << 10 // 64 KiB

// CapToolResultContent cuts every text block over 64 KiB and appends a notice saying so.
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

func countTurns(h []provider.Message) int {
	n := 0
	for _, m := range h {
		if m.Role != provider.RoleUser {
			continue
		}
		if len(m.Content) > 0 && m.Content[0].Type == "tool_result" {
			continue
		}
		n++
	}
	return n
}

func callIDFromToolUseID(s string) (ids.CallID, error) {
	if id, err := ids.ParseCall(s); err == nil {
		return id, nil
	}
	return "", errors.New("not a CallID")
}

// SimpleInput wraps text as one text content block.
func SimpleInput(text string) []control.ContentBlock {
	return []control.ContentBlock{{Type: "text", Text: text}}
}

// FormatMessages renders history one message per line, for debugging.
func FormatMessages(h []provider.Message) string {
	b := make([]byte, 0, 256)
	for _, m := range h {
		b = append(b, []byte(string(m.Role)+": ")...)
		blob, err := json.Marshal(m.Content)
		if err != nil {
			blob = []byte("<unencodable: " + err.Error() + ">")
		}
		b = append(b, blob...)
		b = append(b, '\n')
	}
	return string(b)
}

// String returns a short description of the Engine.
func (e *Engine) String() string {
	return fmt.Sprintf("Engine{session=%s, provider=%s, model=%s, turns=%d}",
		e.Session, e.Provider.Name(), e.Model, countTurns(e.History))
}
