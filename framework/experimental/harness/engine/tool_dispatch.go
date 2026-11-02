package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Dispatcher routes ToolCalls through the tool middleware chain to
// the registered tool. It also bridges the EventSink into the
// per-session event bus so tool progress events get broadcast.
//
// The dispatcher is the only thing in the engine that knows about
// concrete tools, middleware sees the abstract Tool interface and
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
// originator is the Client.ID() that triggered this turn, surfaces in
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

// bashSummary spells a Bash call as the shell command it amounts to:
// `cd <cwd> && env K=V … <cmd>`, env keys sorted so the summary is
// deterministic.
func bashSummary(cmd, cwd string, env map[string]string) string {
	var b strings.Builder
	if cwd != "" {
		b.WriteString("cd " + shellQuote(cwd) + " && ")
	}
	if len(env) > 0 {
		b.WriteString("env ")
		for _, k := range slices.Sorted(maps.Keys(env)) {
			b.WriteString(shellQuote(k+"="+env[k]) + " ")
		}
	}
	b.WriteString(cmd)
	return b.String()
}

// shellQuote single-quotes s unless every byte is one a shell word
// keeps as is.
func shellQuote(s string) string {
	if strings.Trim(s, shellSafe) == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const shellSafe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-./:=@%+,"

// searchSummary spells a search as its pattern, then each non-empty
// (flag, value) pair of flagValues, every word quoted by summaryWord.
// No word holds an unquoted space, so the spelling splits back into
// the words it came from: a pattern-only call cannot spell the summary
// of a call that names a directory.
func searchSummary(pattern string, flagValues ...string) string {
	var b strings.Builder
	b.WriteString(summaryWord(pattern))
	for i := 0; i+1 < len(flagValues); i += 2 {
		if v := flagValues[i+1]; v != "" {
			b.WriteString(" " + flagValues[i] + " " + summaryWord(v))
		}
	}
	return b.String()
}

// summaryWord single-quotes s, the way shellQuote does, when it holds
// whitespace or a quote; any other s is its own word, so a plain
// pattern keeps the summary an earlier approval was saved under.
func summaryWord(s string) string {
	if !strings.ContainsAny(s, " \t\n\r\v\f'") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// FormatArgvSummary computes a short argv-style summary for a tool
// call, used by permission middleware to populate the
// PermissionRequested event and to match argv-glob rules. The form is
// best-effort and tool-specific:
//
//   - For Bash, the shell command, behind the call's cwd and env.
//   - For Read/Write/Edit/Ls, "<Tool>:<path>".
//   - For Grep and Glob, "<Tool>:<pattern>" plus the directory and
//     filename glob the search reads (searchSummary).
//   - Otherwise the bare tool name.
func FormatArgvSummary(toolName string, args []byte) string {
	switch toolName {
	case "Bash":
		// The permission engine decides from this summary alone (quiet
		// mode, argv-glob rules), so it carries everything that changes
		// what runs: a call's env (GIT_EXTERNAL_DIFF, PATH, LD_PRELOAD)
		// and cwd (whose .git/config git reads) turn a read-only command
		// into one that runs anything. They are spelled the way a shell
		// would run them, so neither an allow-list prefix nor a rule
		// written for the bare command matches. The fields and types are
		// the Bash tool's own: input it cannot decode falls back to the
		// bare tool name, which nothing auto-allows.
		var bashLike struct {
			Cmd string            `json:"cmd"`
			Cwd string            `json:"cwd"`
			Env map[string]string `json:"env"`
		}
		if jsonTryUnmarshal(args, &bashLike) && bashLike.Cmd != "" {
			return bashSummary(bashLike.Cmd, bashLike.Cwd, bashLike.Env)
		}
	case "Read", "Write", "Edit", "Ls":
		var pathLike struct {
			Path string `json:"path"`
		}
		if jsonTryUnmarshal(args, &pathLike) && pathLike.Path != "" {
			return fmt.Sprintf("%s:%s", toolName, pathLike.Path)
		}
	case "Glob":
		// Where a search reads is part of what it does: the root
		// (Glob) and the path and glob (Grep) are in the summary, so an
		// approval of a pattern over the working directory never
		// covers the same pattern over ~/.ssh.
		var glob struct {
			Pattern string `json:"pattern"`
			Root    string `json:"root"`
		}
		if jsonTryUnmarshal(args, &glob) && glob.Pattern != "" {
			return "Glob:" + searchSummary(glob.Pattern, "--root", glob.Root)
		}
	case "Grep":
		var grep struct {
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
			Glob    string `json:"glob"`
		}
		if jsonTryUnmarshal(args, &grep) && grep.Pattern != "" {
			return "Grep:" + searchSummary(grep.Pattern, "--path", grep.Path, "--glob", grep.Glob)
		}
	case "WebFetch":
		var w struct {
			URL string `json:"url"`
		}
		if jsonTryUnmarshal(args, &w) && w.URL != "" {
			return fmt.Sprintf("WebFetch:%s", w.URL)
		}
	}
	return toolName
}
