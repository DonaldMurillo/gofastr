package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Source adapts an MCP Client into a tool.ToolSource. Tools the
// remote server exposes are surfaced under namespace prefixes like
// "mcp:kiln.create_entity" so they don't collide with built-ins.
type Source struct {
	client    *Client
	name      string // "mcp:<server-name>" or just "<server-name>"
	discovery string // "eager" or "lazy"
	tools     []ToolDescriptor
}

// NewSource constructs a Source.
//
// discovery selects between:
//   - "eager": tools/list + schemas fetched at startup. Use for
//     low-tool-count servers (<=20 tools).
//   - "lazy":  tools/list only at startup; tool_schema fetched on
//     first invocation. Default for high-tool-count servers.
//
// Per the spec, /list already returns names + descriptions +
// inputSchema. The "lazy" mode in v0.1 just defers tool registration
// to later if no schema has been seen — but most servers return
// schemas in /list anyway, so the actual savings come from server
// implementations that elide schemas in /list.
func NewSource(name, discovery string, c *Client) *Source {
	return &Source{client: c, name: name, discovery: discovery}
}

// Name implements tool.ToolSource.
func (s *Source) Name() string { return "mcp:" + s.name }

// Tools implements tool.ToolSource by calling the MCP server's tools/list.
func (s *Source) Tools(ctx context.Context) ([]tool.Tool, error) {
	descs, err := s.client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	s.tools = descs
	out := make([]tool.Tool, 0, len(descs))
	for _, d := range descs {
		out = append(out, &mcpTool{
			source:      s,
			name:        prefixed(s.name, d.Name),
			description: d.Description,
			schema:      d.InputSchema,
		})
	}
	return out, nil
}

func prefixed(serverName, toolName string) string {
	// Avoid double-prefixing if the server already prefixes its tool names.
	if strings.HasPrefix(toolName, serverName+".") {
		return toolName
	}
	return serverName + "." + toolName
}

// mcpTool is one MCP-bridged tool.
type mcpTool struct {
	source      *Source
	name        string
	description string
	schema      json.RawMessage
}

func (t *mcpTool) Name() string        { return t.name }
func (t *mcpTool) Description() string { return t.description }
func (t *mcpTool) InputSchema() []byte { return t.schema }

// Mutating returns true (conservative) for all MCP-bridged tools.
// MCP doesn't declare is_mutating per tool; without that info we
// assume mutating so the intent/outcome ledger fsyncs are applied.
// Profile-level overrides can mark specific MCP tools as read-only.
func (t *mcpTool) Mutating() bool { return true }

// Run executes the tool via MCP tools/call.
func (t *mcpTool) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	// Strip the server prefix when calling — the server expects its
	// own canonical tool name.
	remoteName := call.Name
	prefix := t.source.name + "."
	if strings.HasPrefix(remoteName, prefix) {
		remoteName = strings.TrimPrefix(remoteName, prefix)
	}
	result, err := t.source.client.CallTool(ctx, remoteName, call.Input)
	if err != nil {
		return &tool.ToolResult{
			IsError: true,
			Content: []control.ContentBlock{{Type: "text", Text: err.Error()}},
		}, nil
	}
	// MCP tool result shape: {content: [{type: "text", text: "..."}], isError?}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return &tool.ToolResult{
			IsError: true,
			Content: []control.ContentBlock{{Type: "text", Text: fmt.Sprintf("mcp result parse error: %v", err)}},
		}, nil
	}
	res := &tool.ToolResult{IsError: parsed.IsError}
	for _, c := range parsed.Content {
		res.Content = append(res.Content, control.ContentBlock{Type: c.Type, Text: c.Text})
	}
	return res, nil
}
