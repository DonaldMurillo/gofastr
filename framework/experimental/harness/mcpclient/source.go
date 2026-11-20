package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Source is a tool.ToolSource exposing a server's tools as "<name>.<tool>".
type Source struct {
	client    *Client
	name      string
	discovery string
	tools     []ToolDescriptor
}

// NewSource returns a Source for the server c under name; discovery ("eager" or "lazy") is recorded but not yet acted on.
func NewSource(name, discovery string, c *Client) *Source {
	return &Source{client: c, name: name, discovery: discovery}
}

// Name returns "mcp:" plus the server name.
func (s *Source) Name() string { return "mcp:" + s.name }

// Tools lists the server's tools on every call.
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
	if strings.HasPrefix(toolName, serverName+".") {
		return toolName
	}
	return serverName + "." + toolName
}

type mcpTool struct {
	source      *Source
	name        string
	description string
	schema      json.RawMessage
}

func (t *mcpTool) Name() string        { return t.name }
func (t *mcpTool) Description() string { return t.description }
func (t *mcpTool) InputSchema() []byte { return t.schema }

func (t *mcpTool) Mutating() bool { return true }

func (t *mcpTool) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	remoteName := call.Name
	prefix := t.source.name + "."
	if after, ok := strings.CutPrefix(remoteName, prefix); ok {
		remoteName = after
	}
	result, err := t.source.client.CallTool(ctx, remoteName, call.Input)
	if err != nil {
		return &tool.ToolResult{
			IsError: true,
			Content: []control.ContentBlock{{Type: "text", Text: err.Error()}},
		}, nil
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := handler.CheckObjectKeys(result, handler.FoldJSONKey); err != nil {
		return &tool.ToolResult{
			IsError: true,
			Content: []control.ContentBlock{{Type: "text", Text: fmt.Sprintf("mcp result parse error: %v", err)}},
		}, nil
	}
	//gofastr:allow(GOFASTR1407) the CheckObjectKeys walk above already refused duplicate and case-folded keys at every depth; this Unmarshal only decodes a vetted result
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
