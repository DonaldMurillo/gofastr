package mcp

import (
	"context"
	"fmt"

	mcpcore "github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/kiln/agent"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// Register registers every Kiln tool against an MCP server.
// The same Tools surface that backs the native agent loop and the
// chat panel handles dispatch — the MCP layer is purely a transport.
func Register(srv *mcpcore.Server, tools *protocol.Tools) error {
	if srv == nil {
		return fmt.Errorf("kiln/agent/mcp: nil server")
	}
	if tools == nil {
		return fmt.Errorf("kiln/agent/mcp: nil tools")
	}
	for _, d := range tools.List() {
		desc := d
		err := srv.RegisterTool(desc.Name, desc.Description, desc.Schema, func(ctx context.Context, params map[string]any) (any, error) {
			res := agent.Dispatch(ctx, tools, agent.ToolCall{
				Name: desc.Name,
				Args: params,
			})
			// Both ok and !ok results return as the structured Result so
			// the agent (and panel) can branch on Kind/Hint. needs_plan
			// surfaces destructive-op blockers; the agent should call
			// propose_plan and retry.
			return res, nil
		})
		if err != nil {
			return fmt.Errorf("kiln/agent/mcp: register %q: %w", desc.Name, err)
		}
	}
	return nil
}

// NewServer builds a fresh MCP server with all Kiln tools registered.
// Convenience wrapper for callers that don't need to share the server
// with other registrations.
func NewServer(tools *protocol.Tools) (*mcpcore.Server, error) {
	srv := mcpcore.NewServer()
	if err := Register(srv, tools); err != nil {
		return nil, err
	}
	return srv, nil
}
