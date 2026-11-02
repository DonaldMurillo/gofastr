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
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
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
// No two keys anywhere in params may fold to one name ("q" and "Q"), and
// a tagged param given as "" is absent unless its tag carries `,empty`,
// the rules Bind applies to a query string.
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
//
// The key rules run over the whole params object first, before it is
// split. Split first, a tagged key and its case-folded twin landed in
// different halves and each passed its own check: for `Q string
// query:"q"` (no json tag), {"q":"safe","Q":"danger"} bound "safe" from
// the tag half, then the body half's "Q" matched the Go field name and
// overwrote it, so the handler ran "danger" while params["q"] read
// "safe".
//
// An empty tagged value ("") is absence, as an empty query value is in
// Bind, unless the tag carries `,empty`.
func bindToolParams(params map[string]any, dst any) error {
	whole, err := json.Marshal(params)
	if err != nil {
		return Fail(400, ReasonInvalidParams, map[string]any{"detail": err.Error()})
	}
	if err := CheckObjectKeys(whole, FoldJSONKey); err != nil {
		return err
	}
	rest := maps.Clone(params)
	// A pointer-typed input (Handler[*T, O]) reads T's tags; settableField
	// allocates the pointer when a tagged param arrives.
	rv := reflect.ValueOf(dst).Elem()
	if st := derefType(rv.Type()); st.Kind() == reflect.Struct {
		for _, source := range []string{"query", "path"} {
			fields, err := taggedFields(st, source)
			if err != nil {
				return err
			}
			for _, tf := range fields {
				tag := tf.name
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
				if tf.absent(s) {
					continue
				}
				fv := settableField(rv, tf.index)
				if !fv.IsValid() {
					continue
				}
				if err := setTagged(fv, s); err != nil {
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
		return Fail(400, ReasonInvalidParams, map[string]any{"detail": JSONErrorDetail(err)})
	}
	// UseNumber, then Settle, as Bind does: MCP hands an integer past 2^53
	// over as a json.Number, and a float64 decode would round it.
	if err := UnmarshalStrict(body, dst, UseNumber()); err != nil {
		return err
	}
	if err := jsonexact.Settle(dst); err != nil {
		return Fail(400, ReasonInvalidParams, map[string]any{"detail": JSONErrorDetail(err)})
	}
	return nil
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
