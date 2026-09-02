package effect

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property: a request-time-evaluated response field can never put the
// process into a panic.
//
// respond_json's `status` is an expression string evaluated per request
// against the route scope (path, method), so its value is attacker-
// influenced even when the IR itself is benign. net/http's WriteHeader
// panics for any code outside 100..999, and kiln's applyRoutes installs
// raw http.HandlerFuncs, panic recovery in a kiln-rendered app is an
// opt-in catalog entry the world has to declare, so an out-of-range
// status is an uncontained panic on the serving goroutine.

// TestOutOfRangeStatusRejected pins that a status outside the HTTP range
// is refused at resolve time rather than handed to WriteHeader.
func TestOutOfRangeStatusRejected(t *testing.T) {
	for name, status := range map[string]any{
		"zero":         float64(0),
		"below 100":    float64(42),
		"above 999":    float64(1000),
		"negative":     float64(-1),
		"huge float":   1e30,
		"path length":  "len(ctx.path)", // benign IR + attacker-chosen path
		"non-integral": 200.5,
	} {
		a := world.Action{
			Kind:   world.ActionRespondJSON,
			Params: map[string]any{"status": status},
		}
		scope := Scope{Ctx: map[string]any{"path": "/ab", "method": "GET"}}
		resp, err := Resolve(context.Background(), a, scope)
		if err != nil {
			continue // rejected at resolve, the preferred outcome
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s (status=%v) panicked in WriteTo: %v", name, status, r)
				}
			}()
			if wErr := resp.WriteTo(httptest.NewRecorder()); wErr != nil {
				t.Logf("%s: write error %v", name, wErr)
			}
		}()
	}
}

// TestInRangeStatusStillHonored guards against an over-strict clamp.
func TestInRangeStatusStillHonored(t *testing.T) {
	for _, want := range []int{200, 201, 302, 404, 418, 503} {
		a := world.Action{
			Kind:   world.ActionRespondJSON,
			Params: map[string]any{"status": float64(want), "body": map[string]any{"ok": true}},
		}
		resp, err := Resolve(context.Background(), a, Scope{})
		if err != nil {
			t.Fatalf("status %d rejected: %v", want, err)
		}
		rec := httptest.NewRecorder()
		if err := resp.WriteTo(rec); err != nil {
			t.Fatalf("status %d: %v", want, err)
		}
		if rec.Code != want {
			t.Errorf("status = %d, want %d", rec.Code, want)
		}
	}
}

// Property: the write boundary refuses an out-of-range status too.
//
// Resolve's rejection is pinned above, but Response is an exported type
// a caller can construct directly, and the failure mode of handing a bad
// status to net/http is a process-level panic. The belt-and-braces check
// in WriteTo must return an error before WriteHeader is ever called.
func TestWriteToRejectsOutOfRangeStatus(t *testing.T) {
	for _, status := range []int{-1, 42, 99, 1000, 100000} {
		resp := Response{Status: status, Body: map[string]any{"ok": true}}
		rec := httptest.NewRecorder()
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("status %d panicked in WriteTo: %v", status, r)
				}
			}()
			err = resp.WriteTo(rec)
		}()
		if err == nil {
			t.Errorf("directly-constructed Response{Status: %d} was written without complaint", status)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("status %d: body written before validation: %q", status, rec.Body.String())
		}
	}
	// Status 0 stays the documented "no explicit status" default.
	rec := httptest.NewRecorder()
	if err := (Response{}).WriteTo(rec); err != nil {
		t.Fatalf("zero status: %v", err)
	}
	if rec.Code != 200 {
		t.Errorf("zero status normalized to %d, want 200", rec.Code)
	}
}

// Property: a request-evaluated body cannot inject response headers —
// the body is JSON-encoded and the only header a respond_json response
// sets is the framework-fixed Content-Type.
//
// The body expression's result is attacker-influenced (scope values come
// from the request), so a CRLF-laden value must stay inside the JSON
// string, never reach w.Header().
func TestRespondJSONBodyCannotSetHeaders(t *testing.T) {
	for name, body := range map[string]any{
		"crlf via expression":  `"a` + "\r\n" + `Set-Cookie: evil=1` + "\r\n" + `X-Evil: yes"`,
		"crlf via literal map": map[string]any{"v": "b\r\nSet-Cookie: evil=1"},
		"lf via literal map":   map[string]any{"v": "c\nSet-Cookie: evil=1"},
	} {
		a := world.Action{
			Kind:   world.ActionRespondJSON,
			Params: map[string]any{"status": float64(200), "body": body},
		}
		resp, err := Resolve(context.Background(), a, Scope{Ctx: map[string]any{"path": "/x"}})
		if err != nil {
			t.Fatalf("%s: resolve: %v", name, err)
		}
		rec := httptest.NewRecorder()
		if err := resp.WriteTo(rec); err != nil {
			t.Fatalf("%s: write: %v", name, err)
		}
		for _, h := range []string{"Set-Cookie", "X-Evil"} {
			if rec.Header().Get(h) != "" {
				t.Errorf("%s: %s header injected: %v", name, h, rec.Header())
			}
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: Content-Type = %q, want application/json", name, ct)
		}
		// json.Encoding escapes control bytes; a raw CR or LF in the body
		// (Encode's trailing newline aside) means quoting was bypassed.
		trimmed := strings.TrimSuffix(rec.Body.String(), "\n")
		if strings.Contains(trimmed, "\r") || strings.Contains(trimmed, "\n") {
			t.Errorf("%s: raw control byte reached the body outside JSON quoting: %q", name, trimmed)
		}
	}
}

// Property: a body expression that fails to evaluate surfaces as a
// Resolve error (the route handler 500s), never a panic and never a
// silent empty 200.
func TestRespondJSONBodyExprErrorIsLoud(t *testing.T) {
	for _, body := range []string{
		"entity.nope", // member access on a missing field
		"1/0",         // division by zero
		"entity + 1",  // arith on a map operand
		`"a" + 1`,     // string/int arith
	} {
		a := world.Action{
			Kind:   world.ActionRespondJSON,
			Params: map[string]any{"body": body},
		}
		scope := Scope{Entity: map[string]any{"n": int64(1)}}
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("body %q panicked: %v", body, r)
				}
			}()
			_, err = Resolve(context.Background(), a, scope)
		}()
		if err == nil {
			t.Errorf("body %q evaluated without error; a failing expression must not become a 200", body)
		}
	}
}
