// Package mcptools serves the framework's embedded documentation over
// MCP: framework_docs_list, framework_docs_get and framework_docs_search,
// so an agent connected to a running app can answer "how do I use
// hooks" without leaving the session.
//
// The package is a host opt-in beside framework.WithMCPIntrospection,
// handed to the framework through the generic registrar option:
//
//	framework.NewApp(
//		framework.WithMCP(),
//		framework.WithMCPIntrospection(),
//		framework.WithMCPTools(mcptools.Register),
//	)
//
// It lives under framework/docs rather than inside framework so that
// framework itself never imports the embedded markdown corpus, and it
// imports core/mcp rather than framework so the facade stays above it
// (framework/layering_test.go pins that direction). The
// repo's affected-package walk marks an embedded asset as a change to
// its owning package, so with the import in framework every docs edit
// widened the test set to the whole tree; with the edge here a docs
// edit affects framework/docs, this package and the hosts that import
// it. The tools read framework/docs at call time, so they always
// describe the framework version the binary was built against.
package mcptools

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework/docs"
)

// maxDocsSearchHits is the hard ceiling the framework_docs_search schema
// advertises. docs.SearchWithLimit honours any positive value and only
// substitutes its own default for limit <= 0, so a caller asking for
// 1e12 got every matching line in the embedded corpus -- ten thousand
// hits against a term as ordinary as "the". The tool exists to be called
// by agents with narrow contexts; a response that large is the failure
// the cap is named for.
const maxDocsSearchHits = 200

// Register adds the three framework_docs_* MCP tools to srv. The tools
// are read-only and reveal nothing about the app itself, only the
// framework's own documentation, so they are safe beside a public /mcp.
// Hand it to framework.WithMCPTools; a name collision with a
// host-registered tool fails the boot the way every other registrar
// error does.
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
			description: "Search across every framework doc topic for a substring (case-insensitive, min 3 chars). Returns matching lines with nearest-heading context, capped at `limit` hits (default 50). Use when you don't know which topic owns the answer.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"term":  map[string]any{"type": "string", "description": "Search term (min 3 chars)"},
					"limit": map[string]any{"type": "integer", "description": "Max hits to return (default 50, hard cap 200 to protect narrow-context clients)", "maximum": maxDocsSearchHits},
				},
				"required": []string{"term"},
			},
			handler: toolDocsSearch,
		},
	}
	for _, t := range tools {
		if err := srv.RegisterTool(t.name, t.description, t.schema, t.handler); err != nil {
			return fmt.Errorf("mcptools: register MCP tool %q: %w", t.name, err)
		}
	}
	return nil
}

// toolDocsList enumerates every embedded framework doc topic. Each
// entry has name (use with framework_docs_get), title (first H1 in the
// file), summary (first non-heading paragraph), and bytes (raw size).
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

// toolDocsGet returns the full markdown body for a named topic.
func toolDocsGet(_ context.Context, params map[string]any) (any, error) {
	topic, _ := params["topic"].(string)
	body, err := docs.Get(topic)
	if err != nil {
		// The caller's mistake, said to the caller: a plain error would be
		// answered as a generic internal tool error and logged at Error
		// level, which a bad topic name must not do.
		return callerError(err), nil
	}
	return map[string]any{
		"name":     topic,
		"markdown": string(body),
		"bytes":    len(body),
	}, nil
}

// toolDocsSearch greps every topic for a substring. The response shape
// mirrors the SearchHit type, topic, line, heading, excerpt, but
// keeps the payload size bounded by capping each excerpt at 240 chars.
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
	// Clamp rather than reject: a caller asking for more than the tool
	// will give is not making an error. A limit of 0 falls through
	// unchanged, since that is how SearchWithLimit spells "default".
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

// callerError answers a docs lookup failure to the caller as a tool
// error result. Every failure in this package is the caller's input (an
// unknown topic, a term under three characters): nothing here reads
// anything but the embedded corpus, so there is no server fault to hide.
func callerError(err error) mcp.ToolResult {
	return mcp.ToolResult{IsError: true, Content: []mcp.Content{mcp.TextContent(err.Error())}}
}
