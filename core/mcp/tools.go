package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

type toolsCallResult struct {
	Content           []Content `json:"content"` //gofastr:required-array
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`
}

// decodeToolArguments decodes tools/call arguments: omitted is a nil map,
// and anything present must be a JSON object (null included).
func decodeToolArguments(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("arguments must be an object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var args map[string]any
	if err := dec.Decode(&args); err != nil {
		return nil, handler.JSONErrorDetail(err)
	}
	args, _ = normalizeArgNumbers(args).(map[string]any)
	return args, nil
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
	args, err := decodeToolArguments(params.Arguments)
	if err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
	}

	if params.Name == "" {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing tool name")
	}
	meta, err := decodeRequestMeta(params.Meta)
	if err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
	}
	ctx = context.WithValue(ctx, requestMetaKey{}, meta)

	result, err := s.callTool(ctx, params.Name, args)
	if err != nil {
		rpcErr, ok := err.(*RPCError)
		if ok && rpcErr.argsInvalid {
			return newSuccessResponse(req.ID, toolsCallResult{
				Content: []Content{TextContent(rpcErr.Message)},
				IsError: true,
			})
		}
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
