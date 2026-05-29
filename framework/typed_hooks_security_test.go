package framework

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// hookSecPost mirrors a real codegen model: every non-id field carries
// `,omitempty` on its json tag (cmd/gofastr/generate.go emits this). A
// defensive Before-hook that forces a field to its zero value (e.g.
// IsAdmin=false, Balance=0, Role="") must still override the
// attacker-supplied value already in the pending body. If the merge-back
// relies on json.Marshal's omitempty, the forced-zero field is dropped
// and the attacker's value survives — a privilege-escalation primitive.
type hookSecPost struct {
	ID      string `json:"id,omitempty"`
	Title   string `json:"title,omitempty"`
	IsAdmin bool   `json:"isAdmin,omitempty"`
	Balance int    `json:"balance,omitempty"`
	Role    string `json:"role,omitempty"`
}

// TestTypedHook_ForcedZeroOverrides asserts a Before-hook that forces a
// field to a falsy value persists that override into the row, even when
// the client supplied a truthy/non-zero value and the generated struct
// tags the field `omitempty`.
func TestTypedHook_ForcedZeroOverrides(t *testing.T) {
	cases := []struct {
		name    string
		client  map[string]any
		force   func(*hookSecPost)
		wantKey string
		want    any
	}{
		{
			name:    "bool true forced false",
			client:  map[string]any{"title": "t", "is_admin": true},
			force:   func(p *hookSecPost) { p.IsAdmin = false },
			wantKey: "is_admin",
			want:    false,
		},
		{
			name:    "int forced zero",
			client:  map[string]any{"title": "t", "balance": 999},
			force:   func(p *hookSecPost) { p.Balance = 0 },
			wantKey: "balance",
			want:    0,
		},
		{
			name:    "string forced empty",
			client:  map[string]any{"title": "t", "role": "admin"},
			force:   func(p *hookSecPost) { p.Role = "" },
			wantKey: "role",
			want:    "",
		},
		{
			name:    "non-zero mutation still flows back",
			client:  map[string]any{"title": "t"},
			force:   func(p *hookSecPost) { p.Title = "changed" },
			wantKey: "title",
			want:    "changed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := make(map[string]any, len(tc.client))
			for k, v := range tc.client {
				body[k] = v
			}
			var v hookSecPost
			if err := unmarshalHookPayload(body, &v); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			before := v
			tc.force(&v)
			if err := mergeStructIntoMap(&before, &v, body); err != nil {
				t.Fatalf("merge: %v", err)
			}
			got, ok := body[tc.wantKey]
			if !ok {
				t.Fatalf("forced field %q dropped from body; defensive hook is a no-op", tc.wantKey)
			}
			// json numbers round-trip as float64; normalise for int compare.
			if f, isFloat := got.(float64); isFloat {
				got = int(f)
			}
			if got != tc.want {
				t.Fatalf("body[%q] = %v (%T), want %v", tc.wantKey, got, got, tc.want)
			}
		})
	}
}

// TestTypedHook_UpdateNoForcedFieldUnchanged guards the diff approach
// against over-writing: a sparse BeforeUpdate body must not gain fields
// the hook never touched (forcing balance=0 on every update would wipe
// balances). Only fields the hook actually changed get merged back.
func TestTypedHook_UpdateNoForcedFieldUnchanged(t *testing.T) {
	body := map[string]any{"title": "updated"}
	var v hookSecPost
	if err := unmarshalHookPayload(body, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	before := v
	// hook makes no change
	if err := mergeStructIntoMap(&before, &v, body); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if _, ok := body["balance"]; ok {
		t.Fatalf("untouched field leaked into sparse update body: %v", body)
	}
	if _, ok := body["is_admin"]; ok {
		t.Fatalf("untouched field leaked into sparse update body: %v", body)
	}
	if body["title"] != "updated" {
		t.Fatalf("title corrupted: %v", body["title"])
	}
}

// A panic inside a third-party or buggy hook must NOT propagate out of
// HookRegistry.ExecuteHooks. Without recovery, the http stack unwinds
// unpredictably and the surrounding tx leaks. The framework's
// lifecycle (tx rollback, error chain) handles errors deterministically;
// recovery converts panics into errors so the same code path applies.

var panicShapes = []struct {
	name string
	fire func()
}{
	{"string", func() { panic("boom") }},
	{"error", func() { panic(errors.New("boom")) }},
	{"struct", func() { panic(struct{ Code int }{500}) }},
	{"nil-deref", func() { var p *int; _ = *p }},
	{"index-out-of-range", func() { var s []string; _ = s[1] }},
	{"nil-map-write", func() { var m map[string]string; m["x"] = "y" }},
}

func TestHooks_RecoverPanicsAsErrors(t *testing.T) {
	for _, shape := range panicShapes {
		t.Run(shape.name, func(t *testing.T) {
			app := NewApp(WithoutDefaultMiddleware())
			OnBeforeCreate(app, "posts", func(_ context.Context, _ *hookTestPost) error {
				shape.fire()
				return nil
			})

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic escaped ExecuteHooks: %v", r)
				}
			}()
			err := app.HookRegistry("posts").ExecuteHooks(context.Background(), hook.BeforeCreate, map[string]any{"title": "hi"})
			if err == nil {
				t.Fatalf("expected error from hook panic")
			}
		})
	}
}

// TestHooks_AfterRecoveryStillCallsLater ensures recovery doesn't break
// the existing first-error-stops loop semantic — recovered panics
// behave like any other error.
func TestHooks_AfterRecoveryStillCallsLater(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	calls := 0
	OnBeforeCreate(app, "posts", func(_ context.Context, _ *hookTestPost) error {
		calls++
		panic("boom")
	})
	OnBeforeCreate(app, "posts", func(_ context.Context, _ *hookTestPost) error {
		calls++
		return nil
	})

	err := app.HookRegistry("posts").ExecuteHooks(context.Background(), hook.BeforeCreate, map[string]any{"title": "hi"})
	if err == nil {
		t.Fatalf("expected error from panicking hook")
	}
	if calls != 1 {
		t.Fatalf("expected loop to stop at first failing hook; got %d calls", calls)
	}
}
