package effect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Response describes the effect of a handler-style action (e.g. respond_json).
// It's separate from the side-effect Run path so routes have a clean
// "produce a response" surface independent of validate/set/audit.
type Response struct {
	Status int
	Body   any
}

// Resolve runs an action that's expected to produce a Response. Currently
// supported kinds: ActionRespondJSON. ActionNoop returns 204.
func Resolve(ctx context.Context, a world.Action, scope Scope) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	switch a.Kind {
	case "", world.ActionNoop:
		return Response{Status: http.StatusNoContent}, nil
	case world.ActionRespondJSON:
		return resolveJSON(a, scope)
	default:
		return Response{}, fmt.Errorf("effect: action %q does not produce a response", a.Kind)
	}
}

// WriteTo writes r as JSON to w.
func (r Response) WriteTo(w http.ResponseWriter) error {
	if r.Status == 0 {
		r.Status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.Status)
	if r.Body == nil {
		return nil
	}
	return json.NewEncoder(w).Encode(r.Body)
}

func resolveJSON(a world.Action, s Scope) (Response, error) {
	resp := Response{Status: http.StatusOK}
	if v, ok := a.Params["status"]; ok && v != nil {
		switch n := v.(type) {
		case int:
			resp.Status = n
		case int64:
			resp.Status = int(n)
		case float64:
			resp.Status = int(n)
		case string:
			out, err := evalExpr(n, s)
			if err != nil {
				return Response{}, fmt.Errorf("respond_json status: %w", err)
			}
			i, ok := toInt(out)
			if !ok {
				return Response{}, fmt.Errorf("respond_json status: expected int, got %T", out)
			}
			resp.Status = i
		}
	}
	if v, ok := a.Params["body"]; ok {
		switch b := v.(type) {
		case string:
			out, err := evalExpr(b, s)
			if err != nil {
				return Response{}, fmt.Errorf("respond_json body: %w", err)
			}
			resp.Body = out
		default:
			resp.Body = v
		}
	}
	return resp, nil
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
