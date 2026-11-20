package handler

import (
	"context"
	"encoding/json"
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

// ToolAdapter serves h as an MCP tool. Params named by query and path tags
// bind as Bind binds them, refusing a control byte or path escape; the
// rest decode strictly as a body. No two keys anywhere may fold together.
// The context carries no request, so header tags stay zero.
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
		if err := bindToolParams(ctx, params, &in); err != nil {
			return nil, err
		}
		return h(ctx, in)
	}
}

func bindToolParams(ctx context.Context, params map[string]any, dst any) error {
	whole, err := json.Marshal(params)
	if err != nil {
		return Fail(400, ReasonInvalidParams, map[string]any{"detail": JSONErrorDetail(err)})
	}
	if err := CheckObjectKeys(whole, FoldJSONKey); err != nil {
		return err
	}
	pattern, _ := ctx.Value(routePatternKey{}).(string)
	rest := maps.Clone(params)
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
					clean = router.SanitizePathParam(s, pattern != "" && router.IsCatchAllParam(pattern, tag))
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
	if err := UnmarshalStrict(body, dst, UseNumber()); err != nil {
		return err
	}
	if err := jsonexact.Settle(dst); err != nil {
		return Fail(400, ReasonInvalidParams, map[string]any{"detail": JSONErrorDetail(err)})
	}
	return nil
}

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
		value := reflect.ValueOf(v)
		switch value.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return strconv.FormatInt(value.Int(), 10), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return strconv.FormatUint(value.Uint(), 10), nil
		case reflect.Float32, reflect.Float64:
			return strconv.FormatFloat(value.Float(), 'f', -1, value.Type().Bits()), nil
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			if value.IsNil() {
				value = reflect.Value{}
			}
		}
		got := jsonTypeNull
		if value.IsValid() {
			kind, ok := jsonTypeOf(value.Type())
			if !ok {
				return "", i18n.NewError(jsonInvalid)
			}
			got = kind
		}
		return "", i18n.NewError(jsonUnexpectedTypeRoot, map[string]any{"got": i18n.NewError(got)})
	}
}

type routePatternKey struct{}

// WithRoutePattern marks ctx with the HTTP route a tool twins, so its path
// params are sanitized with that route's catch-all flags.
func WithRoutePattern(ctx context.Context, pattern string) context.Context {
	return context.WithValue(ctx, routePatternKey{}, pattern)
}
