package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const maxRequestMetaBytes = 64 << 10

type requestMetaKey struct{}

// RequestMeta returns a copy of the `_meta` object sent with the tools/call being handled, or nil when it sent none.
// Every value in it is caller-supplied and unauthenticated: never treat one as an identity.
func RequestMeta(ctx context.Context) map[string]any {
	m, _ := ctx.Value(requestMetaKey{}).(map[string]any)
	if m == nil {
		return nil
	}
	return cloneMetaValue(m).(map[string]any)
}

// ContextWithRequestMeta returns ctx carrying a copy of meta as the request `_meta`, for calling a tool in process or testing a handler.
func ContextWithRequestMeta(ctx context.Context, meta map[string]any) context.Context {
	if meta == nil {
		return context.WithValue(ctx, requestMetaKey{}, map[string]any(nil))
	}
	return context.WithValue(ctx, requestMetaKey{}, cloneMetaValue(meta))
}

func decodeRequestMeta(raw json.RawMessage) (map[string]any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if len(raw) > maxRequestMetaBytes {
		return nil, fmt.Errorf("_meta exceeds %d bytes", maxRequestMetaBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var meta map[string]any
	if err := dec.Decode(&meta); err != nil {
		return nil, errors.New("_meta must be an object")
	}
	meta, _ = normalizeArgNumbers(meta).(map[string]any)
	return meta, nil
}

func cloneMetaValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = cloneMetaValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = cloneMetaValue(e)
		}
		return out
	}
	return v
}
