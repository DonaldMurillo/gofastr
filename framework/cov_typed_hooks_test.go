package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// covTyped has an int field so a string payload value forces a JSON
// unmarshal error inside unmarshalHookPayload.
type covTyped struct {
	Count int `json:"count,omitempty"`
}

// All four create/update typed hooks return the unmarshal error when the
// payload can't decode into *T. Fire the registered untyped wrapper with a
// type-mismatched map.
func TestCovTypedHooksUnmarshalError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	called := false
	cb := func(context.Context, *covTyped) error { called = true; return nil }

	OnBeforeCreate(app, "e", cb)
	OnAfterCreate(app, "e", cb)
	OnBeforeUpdate(app, "e", cb)
	OnAfterUpdate(app, "e", cb)

	hr := app.HookRegistry("e")
	bad := map[string]any{"count": "not-an-int"} // string → int unmarshal fails
	ctx := context.Background()
	for _, ht := range []hook.HookType{hook.BeforeCreate, hook.AfterCreate, hook.BeforeUpdate, hook.AfterUpdate} {
		if err := hr.ExecuteHooks(ctx, ht, bad); err == nil {
			t.Fatalf("expected unmarshal error for %v", ht)
		}
	}
	if called {
		t.Fatal("callback must not run when the payload fails to decode")
	}
}

// BeforeCreate/BeforeUpdate skip the merge-back when the payload isn't a
// map (e.g. a string), exercising the `m, ok := data.(map...)` false branch.
func TestCovTypedHooksNonMapPayloadSkipsMerge(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	ran := 0
	OnBeforeCreate(app, "e2", func(_ context.Context, v *covTyped) error {
		ran++
		v.Count = 5 // mutation has nowhere to merge back (payload is a string)
		return nil
	})
	OnBeforeUpdate(app, "e2", func(_ context.Context, v *covTyped) error {
		ran++
		v.Count = 9
		return nil
	})

	hr := app.HookRegistry("e2")
	ctx := context.Background()
	// A nil payload marshals to "null" and unmarshals into the zero covTyped,
	// then takes the non-map branch so the merge-back is skipped without error.
	if err := hr.ExecuteHooks(ctx, hook.BeforeCreate, nil); err != nil {
		t.Fatalf("BeforeCreate: %v", err)
	}
	if err := hr.ExecuteHooks(ctx, hook.BeforeUpdate, nil); err != nil {
		t.Fatalf("BeforeUpdate: %v", err)
	}
	if ran != 2 {
		t.Fatalf("expected both hooks to run, got %d", ran)
	}
}

// mergeStructIntoMap returns the json.Marshal error when a mutated field
// can't be marshalled. Build a *T whose changed field holds an
// unmarshalable value via a custom MarshalJSON that errors.
func TestCovMergeMarshalError(t *testing.T) {
	type s struct {
		Bad covBadMarshal `json:"bad"`
	}
	before := &s{}
	after := &s{Bad: covBadMarshal{differ: true}}
	dest := map[string]any{}
	if err := mergeStructIntoMap(before, after, dest); err == nil {
		t.Fatal("expected marshal error from mergeStructIntoMap")
	}
}

// covBadMarshal differs by the `differ` field (so DeepEqual sees a change)
// and fails json.Marshal.
type covBadMarshal struct{ differ bool }

func (covBadMarshal) MarshalJSON() ([]byte, error) {
	return nil, errStored
}
