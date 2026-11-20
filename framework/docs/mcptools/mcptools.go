// Package mcptools serves the framework's embedded docs over MCP as the
// framework_docs_list, framework_docs_get and framework_docs_search tools.
// A host opts in with framework.WithMCPTools(mcptools.Register).
package mcptools

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/docs"
)

const maxDocsSearchHits = 200

// Register adds the three read-only framework_docs_* tools to srv and
// errors when a name is already registered.
func Register(srv *mcp.Server) error {
	tools := []struct {
		name        string
		description string
		schema      map[string]any
		handler     func(ctx context.Context, params map[string]any) (any, error)
	}{
		{
			name:        "framework_docs_list",
			description: "List every framework documentation topic shipped with this binary. Returns name + title + summary for each topic. Pair with framework_docs_get to fetch the full markdown.",
			schema:      map[string]any{"type": "object"},
			handler:     toolDocsList,
		},
		{
			name:        "framework_docs_get",
			description: "Return the full markdown body of a framework doc topic by name. Pass the topic name without .md (e.g. \"entity-declarations\", \"hooks-and-transactions\"). Call framework_docs_list first to discover names.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic": map[string]any{"type": "string", "description": "Topic name (no .md suffix)"},
				},
				"required": []string{"topic"},
			},
			handler: toolDocsGet,
		},
		{
			name:        "framework_docs_search",
			description: "Search across every framework doc topic for a substring (case-insensitive, min 3 UTF-8 bytes). Returns matching lines with nearest-heading context, capped at `limit` hits (default 50). Use when you don't know which topic owns the answer.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"term":  map[string]any{"type": "string", "description": "Search term (min 3 UTF-8 bytes)"},
					"limit": map[string]any{"type": "integer", "description": "Max hits to return (default 50, hard cap 200 to protect narrow-context clients)", "maximum": maxDocsSearchHits},
				},
				"required": []string{"term"},
			},
			handler: toolDocsSearch,
		},
	}
	for _, t := range tools {
		if err := srv.RegisterTool(t.name, t.description, t.schema, t.handler, mcp.WithAnnotations(mcp.Annotations{
			ReadOnlyHint:    mcp.Bool(true),
			DestructiveHint: mcp.Bool(false),
			IdempotentHint:  mcp.Bool(true),
			OpenWorldHint:   mcp.Bool(false),
		})); err != nil {
			return fmt.Errorf("mcptools: register MCP tool %q: %w", t.name, err)
		}
	}
	return nil
}

func toolDocsList(_ context.Context, _ map[string]any) (any, error) {
	topics, err := docs.List()
	if err != nil {
		return callerError(err), nil
	}
	out := make([]map[string]any, 0, len(topics))
	for _, t := range topics {
		out = append(out, map[string]any{
			"name":    t.Name,
			"title":   t.Title,
			"summary": t.Summary,
			"bytes":   t.Bytes,
		})
	}
	return map[string]any{
		"topics": out,
		"count":  len(out),
	}, nil
}

func toolDocsGet(_ context.Context, params map[string]any) (any, error) {
	topic, _ := params["topic"].(string)
	body, err := docs.Get(topic)
	if err != nil {
		return callerError(err), nil
	}
	return map[string]any{
		"name":     topic,
		"markdown": string(body),
		"bytes":    len(body),
	}, nil
}

func toolDocsSearch(_ context.Context, params map[string]any) (any, error) {
	term, _ := params["term"].(string)
	limit := 0
	switch v := params["limit"].(type) {
	case int:
		limit = v
	case int64:
		limit = int(v)
	case float64:
		limit = int(v)
	}
	if limit > maxDocsSearchHits {
		limit = maxDocsSearchHits
	}
	hits, err := docs.SearchWithLimit(term, limit)
	if err != nil {
		return callerError(err), nil
	}
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, map[string]any{
			"topic":   h.Topic,
			"line":    h.Line,
			"heading": h.Heading,
			"excerpt": h.Excerpt,
		})
	}
	return map[string]any{
		"term":  term,
		"hits":  out,
		"count": len(out),
	}, nil
}

func callerError(err error) mcp.ToolResult {
	return mcp.ToolResult{IsError: true, Content: []mcp.Content{mcp.TextContent(err.Error())}}
}
