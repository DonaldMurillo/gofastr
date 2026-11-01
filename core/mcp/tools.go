package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
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

	// UseNumber so a JSON integer argument keeps every digit: a plain
	// Unmarshal decodes 9007199254740993 to the float64 9007199254740992
	// (precision lost above 2^53), and an entity filter built from it
	// then addresses the wrong row. json.Number carries the exact
	// decimal through toolParamValues and validateToolArgs, which both
	// handle the type. In-process callers (CallTool) still pass Go
	// natives; only the wire decode is affected.
	dec := json.NewDecoder(bytes.NewReader(req.Params))
	dec.UseNumber()
	var params toolsCallParams
	if err := dec.Decode(&params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+handler.JSONErrorDetail(err).Error())
	}
	// Keep the exact decimal only where float64 would lose it: an integer
	// beyond 2^53 stays a json.Number, everything else becomes the float64
	// handlers have always received, so existing tools that assert
	// params["n"].(float64) keep working.
	params.Arguments, _ = normalizeArgNumbers(params.Arguments).(map[string]any)

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
		// Unreachable while callTool only answers *RPCError, but the
		// echo it replaces was the leak: a plain error's text is
		// internal detail and never crosses the transport.
		return newErrorResponse(req.ID, ErrInternalError, "internal tool error")
	}

	// Normalize the handler's return into MCP content. A plain value keeps
	// the legacy JSON-marshaled text shape; a mcp.ToolResult / mcp.ImageResult /
	// mcp.Content / []mcp.Content emits rich blocks + structuredContent.
	out, err := normalizeToolResult(result)
	if err != nil {
		// The encode error names Go types and values; it stays here.
		return newErrorResponse(req.ID, ErrInternalError, "internal tool error")
	}
	return newSuccessResponse(req.ID, out)
}

// normalizeArgNumbers walks a UseNumber-decoded arguments tree and
// converts each json.Number to the float64 tool handlers historically
// received, EXCEPT an integer whose magnitude exceeds float64's exact
// range (2^53): that one stays a json.Number so its digits survive into
// a filter or query slot. json.Number for a huge integer is not a
// regression — a float64 could never have held it exactly either.
func normalizeArgNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		s := x.String()
		if !strings.ContainsAny(s, ".eE") {
			if i, err := x.Int64(); err == nil {
				if i > 1<<53 || i < -(1<<53) {
					return x
				}
				return float64(i)
			}
			return x // beyond int64 — keep every digit
		}
		// An integral value past 2^53 spelled with a point or an exponent
		// (9007199254740993e0) has already lost digits in a float64, and
		// a float64 that size marshals back as a plain integer literal, so
		// a handler forwarding it would pass the rounded number off as
		// exact. Keep the caller's spelling instead. The same goes for a
		// fraction that rounded to an integral float64
		// (1.0000000000000001, 1e-999999999): as a float64 it would pass
		// an integer schema it does not satisfy.
		if f, err := x.Float64(); err == nil && (f != math.Trunc(f) || math.Abs(f) < 1<<53 && integralNumber(s)) {
			return f
		}
		return x
	case map[string]any:
		for k, e := range x {
			x[k] = normalizeArgNumbers(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalizeArgNumbers(e)
		}
		return x
	}
	return v
}
