package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

// registerTools adds the directory tools. Both are read-only: they
// tell an agent what is running and how to connect to it; the agent
// then talks to that app's own MCP server, not through this one.
func registerTools(s *mcp.Server) error {
	if err := s.RegisterTool("runs_list",
		"List the gofastr apps running on this machine, newest first. Each run carries `mcp`: the mcpServers entry an agent adds to reach that app's own MCP server (null when it serves none).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "only runs whose name contains this text"},
			},
			"additionalProperties": false,
		},
		toolRunsList,
	); err != nil {
		return err
	}
	return s.RegisterTool("run_get",
		"One running gofastr app by id (from runs_list), with its directory, executable and `mcp` connection entry.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "the run id"},
			},
			"required":             []any{"id"},
			"additionalProperties": false,
		},
		toolRunGet,
	)
}

func toolRunsList(_ context.Context, params map[string]any) (any, error) {
	filter := ""
	if v, present := params["name"]; present {
		s, ok := v.(string)
		if !ok {
			return nil, &mcp.RPCError{Code: mcp.ErrInvalidParams, Message: "name must be a string"}
		}
		filter = strings.ToLower(s)
	}
	runs, err := listRuns()
	if err != nil {
		return nil, err
	}
	out := runs[:0]
	for _, r := range runs {
		if strings.Contains(strings.ToLower(r.Name), filter) {
			out = append(out, r)
		}
	}
	return map[string]any{"runs": out}, nil
}

func toolRunGet(_ context.Context, params map[string]any) (any, error) {
	id, ok := params["id"].(string)
	if !ok {
		return nil, &mcp.RPCError{Code: mcp.ErrInvalidParams, Message: "id must be a string"}
	}
	r, found, err := findRun(id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &mcp.RPCError{Code: mcp.ErrInvalidParams, Message: fmt.Sprintf("no running app with id %q", id)}
	}
	return r, nil
}
