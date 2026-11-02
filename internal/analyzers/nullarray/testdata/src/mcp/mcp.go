// Package mcp is core/mcp before bdb716482 and 9e207f909: tools/call
// content and prompts/get messages reached the wire as null.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

type Content struct{ Text string }

func TextContent(s string) Content { return Content{Text: s} }

type toolsCallResult struct {
	Content           []Content `json:"content"` //gofastr:required-array // want Content:"requiredArray"
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}

type ToolResult struct {
	Content    []Content
	Structured any
	IsError    bool
}

type PromptMessage struct{ Role string }

type promptsGetResult struct {
	Description string `json:"description,omitempty"`
	// Messages is required by the MCP schema.
	//gofastr:required-array
	Messages []PromptMessage `json:"messages"` // want Messages:"requiredArray"
}

// Pre-bdb716482 normalizeToolResult: the type-switch binding of a
// caller-supplied value goes straight in.
func normalizeToolResult(result any) (toolsCallResult, error) {
	switch v := result.(type) {
	case Content:
		return toolsCallResult{Content: []Content{v}}, nil
	case []Content:
		return toolsCallResult{Content: v}, nil // want `Content is a required JSON array, and this value may be nil`
	case string:
		return toolsCallResult{Content: []Content{TextContent(v)}}, nil
	}
	return toolsCallResult{}, errors.New("unsupported")
}

// Pre-bdb716482 richResult: a field of the parameter, rebound on one path only.
func richResult(r ToolResult) (toolsCallResult, error) {
	content := r.Content
	if r.Structured != nil && len(content) == 0 {
		b, _ := json.Marshal(r.Structured)
		content = []Content{TextContent(string(b))}
	}
	return toolsCallResult{Content: content, StructuredContent: r.Structured, IsError: r.IsError}, nil // want `Content is a required JSON array`
}

// The near miss a sloppy guard check takes for a fix: the nil guard sits
// under another condition, so the other path still ships nil.
func richResultNested(r ToolResult) toolsCallResult {
	content := r.Content
	if r.Structured != nil {
		if len(content) == 0 {
			content = []Content{TextContent("structured")}
		}
	}
	return toolsCallResult{Content: content} // want `Content is a required JSON array`
}

// Pre-9e207f909 prompts/get: a callback's result.
func promptsGet(ctx context.Context, h func(context.Context) ([]PromptMessage, error)) (promptsGetResult, error) {
	messages, err := h(ctx)
	if err != nil {
		return promptsGetResult{}, err
	}
	return promptsGetResult{Messages: messages}, nil // want `Messages is a required JSON array`
}

// The bdb716482 fix: a helper whose every return is non-nil.
func contentArray(c []Content) []Content { // want contentArray:"nonNilResult"
	if c == nil {
		return []Content{}
	}
	return c
}

func normalizeFixed(result any) toolsCallResult {
	if v, ok := result.([]Content); ok {
		return toolsCallResult{Content: contentArray(v)}
	}
	return toolsCallResult{Content: []Content{}}
}

// The 9e207f909 fix: a nil guard before the sink.
func promptsGetFixed(ctx context.Context, h func(context.Context) ([]PromptMessage, error)) (promptsGetResult, error) {
	messages, err := h(ctx)
	if err != nil {
		return promptsGetResult{}, err
	}
	if messages == nil {
		messages = []PromptMessage{}
	}
	return promptsGetResult{Messages: messages}, nil
}

// make, a literal, append with an element, and an assignment sink.
func built(names []string) promptsGetResult {
	out := make([]PromptMessage, 0, len(names))
	for _, n := range names {
		out = append(out, PromptMessage{Role: n})
	}
	var res promptsGetResult
	res.Messages = out
	res.Messages = append(res.Messages, PromptMessage{})
	return res
}

// A bare `var out []T` filled by a loop that may never run is nil on an
// empty input.
func loopFilled(names []string) promptsGetResult {
	var out []PromptMessage
	for _, n := range names {
		out = append(out, PromptMessage{Role: n})
	}
	return promptsGetResult{Messages: out} // want `Messages is a required JSON array`
}

// The positional spelling and a literal nil.
func positional() (toolsCallResult, toolsCallResult) {
	return toolsCallResult{nil, nil, true}, toolsCallResult{[]Content{}, nil, false} // want `Content is a required JSON array`
}

// Near miss: an unmarked slice field takes whatever it is given.
type listResult struct {
	Items []string `json:"items"`
}

func list(items []string) listResult { return listResult{Items: items} }

// A marker on a non-slice or an omitempty field is itself a mistake.
type bad struct {
	N    int      `json:"n"`              //gofastr:required-array // want `marks a field that is not a slice`
	Tags []string `json:"tags,omitempty"` //gofastr:required-array // want `field has omitempty` Tags:"requiredArray"
}

type toolsListResult struct {
	Tools []Content `json:"tools"` //gofastr:required-array // want Tools:"requiredArray"
}

// pageList hands back a slice of its argument: non-nil when it is.
func pageList[T any](items []T, offset int) []T { // want pageList:"passThrough\\(0\\)"
	if offset >= len(items) {
		return items[:0]
	}
	return items[offset:]
}

func listTools(n int) toolsListResult {
	tools := make([]Content, 0, n)
	return toolsListResult{Tools: pageList(tools, 0)}
}

// The pass-through keeps a nil argument nil.
func listGiven(given []Content) toolsListResult {
	return toolsListResult{Tools: pageList(given, 0)} // want `Tools is a required JSON array`
}

// Appending onto the field itself, and a sink inside a non-empty guard.
func extend(r *toolsCallResult, more []Content, msg *promptsGetResult, parts []PromptMessage) {
	r.Content = append(r.Content, more...)
	if len(parts) > 0 {
		msg.Messages = parts
	}
}
