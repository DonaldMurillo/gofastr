package effect

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

// validStatus reports whether code is inside the range net/http's
// WriteHeader accepts. Anything else panics there, and the kiln route path
// installs raw http.HandlerFuncs (render.applyRoutes) whose panic recovery
// is an opt-in middleware the world has to declare — so the panic is
// uncontained by default. 0 is excluded here and normalized to 200 by
// WriteTo, matching net/http's own "no explicit status" behavior.
func validStatus(code int) bool { return code >= 100 && code <= 999 }

// WriteTo writes r as JSON to w.
func (r Response) WriteTo(w http.ResponseWriter) error {
	if r.Status == 0 {
		r.Status = http.StatusOK
	}
	// Belt and braces: Resolve already rejects an out-of-range status, but
	// Response is an exported type a caller can construct directly, and the
	// failure mode here is a process-level panic rather than a bad response.
	if !validStatus(r.Status) {
		return fmt.Errorf("effect: response status %d out of range (100-999)", r.Status)
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
			i, ok := toInt(n)
			if !ok {
				return Response{}, fmt.Errorf("respond_json status: %v is not an integer", n)
			}
			resp.Status = i
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
		// The status is an expression evaluated per request against the
		// route scope, so its value is attacker-influenced even for a
		// benign IR — `len(ctx.path)` against a 3-character path yields 3,
		// and WriteHeader(3) panics. Reject here, before the value can
		// reach net/http.
		if !validStatus(resp.Status) {
			return Response{}, fmt.Errorf("respond_json status: %d out of range (100-999)", resp.Status)
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

// toInt narrows a scalar to int. A bare int(f) on a float64 is undefined in
// Go when f is NaN, ±Inf, or outside int's range — 1e30 produced
// 9223372036854775807 here — so the float case checks round-trippability
// instead of trusting the conversion.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n != n || n > math.MaxInt32 || n < math.MinInt32 {
			return 0, false // NaN, ±Inf, or out of a portable int range
		}
		if float64(int(n)) != n {
			return 0, false // non-integral
		}
		return int(n), true
	}
	return 0, false
}
