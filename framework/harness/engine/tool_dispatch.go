package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Dispatcher routes ToolCalls through the tool middleware chain to
// the registered tool. It also bridges the EventSink into the
// per-session event bus so tool progress events get broadcast.
//
// The dispatcher is the only thing in the engine that knows about
// concrete tools — middleware sees the abstract Tool interface and
// the EventSink, never the registry.
type Dispatcher struct {
	bus      *Bus
	registry *tool.Registry
	mws      []tool.Middleware
}

// NewDispatcher builds a Dispatcher for one session.
func NewDispatcher(bus *Bus, registry *tool.Registry, mws ...tool.Middleware) *Dispatcher {
	return &Dispatcher{bus: bus, registry: registry, mws: mws}
}

// Registry returns the dispatcher's tool registry. Used by the engine
// to inject the registry into the dispatch ctx for meta tools.
func (d *Dispatcher) Registry() *tool.Registry { return d.registry }

// Dispatch executes a tool call through the middleware chain.
//
// originator is the Client.ID() that triggered this turn — surfaces in
// PermissionRequested events so the multiplexer can address answers
// correctly and so identity_class self-approval guards apply.
func (d *Dispatcher) Dispatch(ctx context.Context, originator ids.ClientID, call tool.ToolCall) (*tool.ToolResult, error) {
	t, err := d.registry.Lookup(call.Name)
	if err != nil {
		return nil, err
	}
	// Per § Persistence → tool-call intent/outcome ledger, the
	// engine emits a ToolCallStarted event before the tool runs so
	// the session log captures the intent. session.Store subscribes
	// and writes the intent row with fsync for mutating tools.
	_, _ = d.bus.Publish(control.ToolCallStarted{
		CallID:   call.ID,
		Tool:     call.Name,
		Args:     call.Input,
		Mutating: t.Mutating(),
	}, originator)

	sink := &busSink{bus: d.bus, callID: call.ID, originator: originator}
	base := func(ctx context.Context, c tool.ToolCall, s tool.EventSink) (*tool.ToolResult, error) {
		return t.Run(ctx, c, s)
	}
	handler := tool.Chain(base, d.mws...)

	// Stamp ctx with originator + is_mutating so middleware (notably
	// the permission gate) doesn't need to look the tool up again.
	ctx = WithOriginator(ctx, originator)
	ctx = WithMutatingFlag(ctx, t.Mutating())

	start := time.Now()
	res, err := handler(ctx, call, sink)
	_ = start // currently unused; engine.Loop aggregates timing in TurnTiming

	if err != nil {
		// A middleware returned an error (timeout, sandbox failure,
		// permission deny that escalated). Surface as a ToolResult
		// with IsError=true so the model can plan around it, plus
		// an Error event for clients.
		_, _ = d.bus.Publish(control.Error{
			Reason:  control.ReasonInvalidCommand,
			Message: err.Error(),
		}, originator)
		return &tool.ToolResult{
			IsError: true,
			Content: []control.ContentBlock{{Type: "text", Text: err.Error()}},
		}, nil
	}
	if res == nil {
		res = &tool.ToolResult{
			IsError: true,
			Content: []control.ContentBlock{{Type: "text", Text: "tool returned nil result"}},
		}
	}
	_, _ = d.bus.Publish(control.ToolResult{
		CallID:  call.ID,
		Content: res.Content,
		IsError: res.IsError,
	}, originator)
	return res, nil
}

// busSink bridges tool.EventSink to the per-session event bus.
type busSink struct {
	bus        *Bus
	callID     ids.CallID
	originator ids.ClientID
}

func (s *busSink) EmitProgress(partial string) {
	_, _ = s.bus.Publish(control.ToolCallProgress{
		CallID:  s.callID,
		Partial: partial,
	}, s.originator)
}

func (s *busSink) EmitEvent(e control.Event) {
	_, _ = s.bus.Publish(e, s.originator)
}

// ErrDispatcherNotReady is returned when the dispatcher is invoked
// before its registry has any sources registered.
var ErrDispatcherNotReady = errors.New("engine: dispatcher not ready (empty registry)")

// FormatArgvSummary computes a short argv-style summary for a tool
// call, used by permission middleware to populate the
// PermissionRequested event and to match argv-glob rules. The form is
// best-effort and tool-specific:
//
//   - For Bash, the leading shell command string.
//   - For Read/Write/Edit/Glob/Ls, "<Tool>:<path>" or "<Tool>:<pattern>".
//   - Otherwise the bare tool name.
func FormatArgvSummary(toolName string, args []byte) string {
	switch toolName {
	case "Bash":
		// Best-effort decode of {cmd: "..."}.
		var bashLike struct{ Cmd string `json:"cmd"` }
		if jsonTryUnmarshal(args, &bashLike) && bashLike.Cmd != "" {
			return bashLike.Cmd
		}
	case "Read", "Write", "Edit", "Ls":
		var pathLike struct{ Path string `json:"path"` }
		if jsonTryUnmarshal(args, &pathLike) && pathLike.Path != "" {
			return fmt.Sprintf("%s:%s", toolName, pathLike.Path)
		}
	case "Glob":
		var glob struct{ Pattern string `json:"pattern"` }
		if jsonTryUnmarshal(args, &glob) && glob.Pattern != "" {
			return fmt.Sprintf("Glob:%s", glob.Pattern)
		}
	case "Grep":
		var grep struct{ Pattern string `json:"pattern"` }
		if jsonTryUnmarshal(args, &grep) && grep.Pattern != "" {
			return fmt.Sprintf("Grep:%s", grep.Pattern)
		}
	case "WebFetch":
		var w struct{ URL string `json:"url"` }
		if jsonTryUnmarshal(args, &w) && w.URL != "" {
			return fmt.Sprintf("WebFetch:%s", w.URL)
		}
	}
	return toolName
}
