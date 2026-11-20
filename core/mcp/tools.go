package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

type toolsListResult struct {
	Tools      []Tool `json:"tools"` //gofastr:required-array
	NextCursor string `json:"nextCursor,omitempty"`
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments map[string]any  `json:"arguments,omitempty"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

type toolsCallResult struct {
	Content           []Content `json:"content"` //gofastr:required-array
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}

func (s *Server) handleToolsList(ctx context.Context, req Request) Response {
	offset, err := s.listOffset(req, "tools/list")
	if err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	page, next := pageList(s, "tools/list", s.listTools(ctx), offset)
	return newSuccessResponse(req.ID, toolsListResult{Tools: page, NextCursor: next})
}

func (s *Server) handleToolsCall(ctx context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}

	dec := json.NewDecoder(bytes.NewReader(req.Params))
	dec.UseNumber()
	var params toolsCallParams
	if err := dec.Decode(&params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+handler.JSONErrorDetail(err).Error())
	}
	params.Arguments, _ = normalizeArgNumbers(params.Arguments).(map[string]any)

	if params.Name == "" {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing tool name")
	}
	meta, err := decodeRequestMeta(params.Meta)
	if err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
	}
	ctx = context.WithValue(ctx, requestMetaKey{}, meta)

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
		return newErrorResponse(req.ID, ErrInternalError, "internal tool error")
	}

	out, err := normalizeToolResult(result)
	if err != nil {
		return newErrorResponse(req.ID, ErrInternalError, "internal tool error")
	}
	return newSuccessResponse(req.ID, out)
}

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
			return x
		}
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
