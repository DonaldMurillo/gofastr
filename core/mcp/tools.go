package mcp

import (
	"context"
	"encoding/json"
)

// toolsListResult is the result shape for tools/list per MCP spec. The
// nextCursor key is absent on the final page, and on every page when the
// whole listing fits (the pre-pagination wire shape).
type toolsListResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// toolsCallParams represents the parameters for a tools/call request,
// per the MCP spec: a tool name and an `arguments` object.
type toolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// toolsCallResult wraps a tool execution result per MCP spec.
type toolsCallResult struct {
	Content           []Content `json:"content"`
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}

// handleToolsList returns one page of the tools visible to the caller,
// in name order. Pagination slices the POST-GATE listing built by
// listTools (call gate + per-tool caller gates already applied), so a
// gated tool is invisible to the paging arithmetic itself: no short
// pages, no cursor offsets that count it.
func (s *Server) handleToolsList(ctx context.Context, req Request) Response {
	offset, err := s.listOffset(req, "tools/list")
	if err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	page, next := pageList(s, "tools/list", s.listTools(ctx), offset)
	return newSuccessResponse(req.ID, toolsListResult{Tools: page, NextCursor: next})
}

// handleToolsCall executes a tool by name with the provided parameters.
func (s *Server) handleToolsCall(ctx context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}

	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
	}

	if params.Name == "" {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing tool name")
	}

	result, err := s.callTool(ctx, params.Name, params.Arguments)
	if err != nil {
		rpcErr, ok := err.(*RPCError)
		if ok {
			return Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   rpcErr,
			}
		}
		return newErrorResponse(req.ID, ErrInternalError, err.Error())
	}

	// Normalize the handler's return into MCP content. A plain value keeps
	// the legacy JSON-marshaled text shape; a mcp.ToolResult / mcp.ImageResult /
	// mcp.Content / []mcp.Content emits rich blocks + structuredContent.
	return newSuccessResponse(req.ID, normalizeToolResult(result))
}
