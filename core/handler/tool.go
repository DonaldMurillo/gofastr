package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"runtime/debug"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ToolAdapter bridges a typed Handler into an MCP tool handler, the
// signature core/mcp's ToolHandler names, so one Handler serves both an
// HTTP route (HandlerAdapter) and its MCP twin.
//
// The tool's params use the names the route binds. A field tagged
// `query:"q"` or `path:"id"` reads params["q"] or params["id"], and the
// remaining params decode strictly, the way Bind decodes a JSON body:
// an unknown or duplicate key is an error. As in Bind, the body applies
// last. A path value no route could match (a "/", a ".." segment, a
// control byte) is refused, where the router would have cut it short.
//
// The handler's context carries no *http.Request: RequestFromContext
// returns nil on this path. Header-tagged fields stay zero.
func ToolAdapter[I, O any](h Handler[I, O]) func(ctx context.Context, params map[string]any) (any, error) {
	return func(ctx context.Context, params map[string]any) (result any, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Default().Error("panic recovered in tool handler",
					"error", textsafe.Recovered(rec),
					"stack", textsafe.Truncate(string(debug.Stack()), maxStackLogLen),
				)
				result, err = nil, Fail(500, i18n.StatusPhrase(500))
			}
		}()
		var in I
		if err := bindToolParams(params, &in); err != nil {
			return nil, err
		}
		return h(ctx, in)
	}
}

// bindToolParams fills dst from MCP tool params: tagged query and path
// fields first, then the rest as a strict JSON body.
func bindToolParams(params map[string]any, dst any) error {
	rest := maps.Clone(params)
	rv := reflect.ValueOf(dst).Elem()
	if rv.Kind() == reflect.Struct {
		for _, source := range []string{"query", "path"} {
			for _, tf := range taggedFields(rv.Type(), source) {
				tag := tf.tag
				raw, ok := rest[tag]
				if !ok {
					continue
				}
				delete(rest, tag)
				s, err := toolParamString(raw)
				if err != nil {
					return Fail(400, ReasonInvalidParameter, map[string]any{"source": source, "name": strconv.Quote(tag), "detail": err.Error()})
				}
				clean := sanitizeQueryParam(s)
				if source == "path" {
					clean = router.SanitizePathParam(s, false)
				}
				if clean != s {
					return Fail(400, ReasonParameterControlByte, map[string]any{"source": source, "name": strconv.Quote(tag)})
				}
				fv := settableField(rv, tf.index)
				if !fv.IsValid() {
					continue
				}
				if err := setField(fv, s); err != nil {
					return Fail(400, ReasonInvalidParameter, map[string]any{"source": source, "name": strconv.Quote(tag), "detail": err.Error()})
				}
			}
		}
	}
	if len(rest) == 0 {
		return nil
	}
	body, err := json.Marshal(rest)
	if err != nil {
		return Fail(400, ReasonInvalidParams, map[string]any{"detail": err.Error()})
	}
	return UnmarshalStrict(body, dst)
}

// toolParamString spells a scalar param the way it would arrive in a URL.
func toolParamString(v any) (string, error) {
	switch v := v.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case json.Number:
		return v.String(), nil
	default:
		return "", fmt.Errorf("must be a string, number or boolean, not %T", v)
	}
}
