package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Dispatcher runs tool calls through the tool middleware chain and publishes their events.
type Dispatcher struct {
	bus      *Bus
	registry *tool.Registry
	mws      []tool.Middleware
	refused  map[string]bool
}

// NewDispatcher builds a Dispatcher for one session.
func NewDispatcher(bus *Bus, registry *tool.Registry, mws ...tool.Middleware) *Dispatcher {
	return &Dispatcher{bus: bus, registry: registry, mws: mws}
}

// Registry returns the dispatcher's tool registry.
func (d *Dispatcher) Registry() *tool.Registry { return d.registry }

// Dispatch runs call for originator; a middleware error comes back as an error result, and only an unknown tool returns an error.
func (d *Dispatcher) Dispatch(ctx context.Context, originator ids.ClientID, call tool.ToolCall) (*tool.ToolResult, error) {
	if d.refused[call.Name] {
		return &tool.ToolResult{
			IsError: true,
			Content: []control.ContentBlock{{Type: "text", Text: call.Name + " is not available here: a sub-agent cannot call it"}},
		}, nil
	}
	t, err := d.registry.Lookup(call.Name)
	if err != nil {
		return nil, err
	}
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

	ctx = WithOriginator(ctx, originator)
	ctx = WithMutatingFlag(ctx, t.Mutating())

	res, err := handler(ctx, call, sink)

	if err != nil {
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

// ErrDispatcherNotReady names a dispatch before any tool source is registered.
var ErrDispatcherNotReady = errors.New("engine: dispatcher not ready (empty registry)")

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

func shellQuote(s string) string {
	if strings.Trim(s, shellSafe) == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const shellSafe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-./:=@%+,"

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

func summaryWord(s string) string {
	if !strings.ContainsAny(s, " \t\n\r\v\f'") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// FormatArgvSummary spells a tool call as the string permission rules match and prompts show.
func FormatArgvSummary(toolName string, args []byte) string {
	switch toolName {
	case "Bash":
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
