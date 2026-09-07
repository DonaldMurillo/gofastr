//go:build red

package framework

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: every mutation a typed lifecycle hook makes to *T is reflected into the
// pending body (Before hooks → INSERT/UPDATE body) or the live response body (After hooks).
// Surfaces: framework/typed_hooks.go::OnBeforeCreate, OnBeforeUpdate (persisted body) and
// OnAfterCreate, OnAfterUpdate (response body), via mergeStructIntoMap.
// Finding: each wrapper snapshots with `before := v`, a struct copy whose map/slice/pointer
// fields still ALIAS v's. An in-place mutation (delete v.Meta[...], v.Tags[0]=...,
// v.Profile.Secret="") leaves before and v DeepEqual-equal at mergeStructIntoMap (same
// backing pointers), so the field is treated as untouched and skipped: the CLIENT's original
// nested value survives into the persisted row / the HTTP response while the host believes
// its hook redacted it.
// Fix direction: deep-copy the snapshot (or deep-compare) before running the callback so
// aliased in-place mutations register as changed fields and merge back.

// redHookProfile is the pointer-nested shape: mutations through the pointer
// must reach the body just like top-level field writes do.
type redHookProfile struct {
	Secret string `json:"secret,omitempty"`
}

// redHookPost carries the three reference-typed shapes a generated model
// realistically has: map, slice, pointer-to-struct.
type redHookPost struct {
	Title   string          `json:"title,omitempty"`
	Meta    map[string]any  `json:"meta,omitempty"`
	Tags    []string        `json:"tags,omitempty"`
	Profile *redHookProfile `json:"profile,omitempty"`
}

// metaHas reports whether the body's meta map still carries a key that is
// `want` modulo snake/camel casing (the unmarshal path camelCases inner map
// keys, the write-back path re-snakes field names, so the assertion must
// accept either spelling and only reject the VALUE surviving).
func metaHas(m map[string]any, want string) bool {
	for k := range m {
		if strings.EqualFold(strings.ReplaceAll(k, "_", ""), want) {
			return true
		}
	}
	return false
}

// TestTypedHookRedRefAliasMutationDrop: a BeforeCreate hook that mutates *T
// in place (map delete, slice element write, write through a pointer field)
// must have every one of those mutations land in the pending body — that body
// is what the crud layer INSERTs.
func TestTypedHookRedRefAliasMutationDrop(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[redHookPost](app, "posts", func(_ context.Context, v *redHookPost) error {
		delete(v.Meta, "isAdmin")
		v.Tags[0] = "redacted"
		if v.Profile != nil {
			v.Profile.Secret = ""
		}
		return nil
	})
	body := map[string]any{
		"id":      "1",
		"title":   "t",
		"meta":    map[string]any{"isAdmin": true},
		"tags":    []any{"a", "b"},
		"profile": map[string]any{"secret": "s3cr3t"},
	}
	if err := app.HookRegistry("posts").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatalf("setup broken: ExecuteHooks: %v", err)
	}

	if meta, ok := body["meta"].(map[string]any); ok && metaHas(meta, "isAdmin") {
		t.Errorf("SECURITY: [typedhook-refalias-before] in-place delete of v.Meta[\"isAdmin\"] was dropped by the merge-back; the client's privileged meta (%v) survives into the INSERT body", meta)
	}
	if tags, ok := body["tags"].([]any); ok && len(tags) > 0 && tags[0] != "redacted" {
		t.Errorf("SECURITY: [typedhook-refalias-before] in-place v.Tags[0]=\"%s\" was dropped by the merge-back; body still carries %v — a slice rewrite a hook believes it performed never reaches the row", "redacted", tags)
	}
	if prof, ok := body["profile"].(map[string]any); ok && prof["secret"] != "" {
		t.Errorf("SECURITY: [typedhook-refalias-before] in-place v.Profile.Secret=\"\" was dropped by the merge-back; the client's nested secret %q survives into the INSERT body", prof["secret"])
	}
}

// TestTypedHookRedRefAliasAfterResp: the same aliasing drop on the After
// surfaces, where the payload is the live result map serialised into the HTTP
// response — an in-place redaction that never lands ships the secret to the
// caller while the host believes it is masked.
func TestTypedHookRedRefAliasAfterResp(t *testing.T) {
	run := func(t *testing.T, label string, phase hook.HookType, register func(*App)) {
		t.Helper()
		app := NewApp(WithoutDefaultMiddleware())
		register(app)
		body := map[string]any{
			"id":      "1",
			"title":   "t",
			"meta":    map[string]any{"secret": "leak"},
			"profile": map[string]any{"secret": "leak"},
		}
		if err := app.HookRegistry("posts").ExecuteHooks(context.Background(), phase, body); err != nil {
			t.Fatalf("setup broken: %s ExecuteHooks: %v", label, err)
		}
		if meta, ok := body["meta"].(map[string]any); ok && metaHas(meta, "secret") {
			t.Errorf("SECURITY: [typedhook-refalias-after] %s: in-place delete of v.Meta[\"secret\"] was dropped by the merge-back; the secret survives into the HTTP response body (%v)", label, meta)
		}
		if prof, ok := body["profile"].(map[string]any); ok && prof["secret"] != "" {
			t.Errorf("SECURITY: [typedhook-refalias-after] %s: in-place v.Profile.Secret=\"\" was dropped by the merge-back; the nested secret %q survives into the HTTP response body", label, prof["secret"])
		}
		// The hook touched only nested internals; sibling columns survive.
		if body["id"] != "1" || body["title"] != "t" {
			t.Fatalf("setup broken: %s clobbered unrelated fields: %v", label, body)
		}
	}
	t.Run("create", func(t *testing.T) {
		run(t, "AfterCreate", hook.AfterCreate, func(app *App) {
			OnAfterCreate[redHookPost](app, "posts", func(_ context.Context, v *redHookPost) error {
				delete(v.Meta, "secret")
				if v.Profile != nil {
					v.Profile.Secret = ""
				}
				return nil
			})
		})
	})
	t.Run("update", func(t *testing.T) {
		run(t, "AfterUpdate", hook.AfterUpdate, func(app *App) {
			OnAfterUpdate[redHookPost](app, "posts", func(_ context.Context, v *redHookPost) error {
				delete(v.Meta, "secret")
				if v.Profile != nil {
					v.Profile.Secret = ""
				}
				return nil
			})
		})
	})
}

// TestTypedHookRedRefAliasBeforeUpdate is the surface extension of
// typedhook-refalias onto OnBeforeUpdate ALONE: BeforeCreate and the After
// arms stay pinned above, so a fix landing on one wrapper flips only its
// own test. It mirrors the update-path seam the sibling
// TestTypedHook_UpdateNoForcedFieldUnchanged exercises.
//
// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; surface
// extension of typedhook-refalias; tests-only; no fix applied).
// Property: every mutation an OnBeforeUpdate hook makes to *T is reflected
// into the pending UPDATE body — that body is what the crud layer writes.
// Surface: framework/typed_hooks.go::OnBeforeUpdate — `before := v` is a
// struct copy whose map/slice/pointer fields still ALIAS v's, so in-place
// mutations DeepEqual-skip at mergeStructIntoMap.
// Finding: the CLIENT's original nested values survive into the UPDATE
// body while the host believes its hook redacted them. Fix direction:
// deep-copy (or deep-compare) the snapshot before running the callback.
func TestTypedHookRedRefAliasBeforeUpdate(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeUpdate[redHookPost](app, "posts", func(_ context.Context, v *redHookPost) error {
		delete(v.Meta, "isAdmin")
		v.Tags[0] = "redacted"
		if v.Profile != nil {
			v.Profile.Secret = ""
		}
		return nil
	})
	body := map[string]any{
		"id":      "1",
		"title":   "t",
		"meta":    map[string]any{"isAdmin": true},
		"tags":    []any{"a", "b"},
		"profile": map[string]any{"secret": "s3cr3t"},
	}
	if err := app.HookRegistry("posts").ExecuteHooks(context.Background(), hook.BeforeUpdate, body); err != nil {
		t.Fatalf("setup broken: ExecuteHooks: %v", err)
	}

	if meta, ok := body["meta"].(map[string]any); ok && metaHas(meta, "isAdmin") {
		t.Errorf("SECURITY: [typedhook-refalias-beforeupdate] in-place delete of v.Meta[\"isAdmin\"] was dropped by the merge-back; the client's privileged meta (%v) survives into the UPDATE body", meta)
	}
	if tags, ok := body["tags"].([]any); ok && len(tags) > 0 && tags[0] != "redacted" {
		t.Errorf("SECURITY: [typedhook-refalias-beforeupdate] in-place v.Tags[0]=\"%s\" was dropped by the merge-back; body still carries %v — a slice rewrite a hook believes it performed never reaches the row", "redacted", tags)
	}
	if prof, ok := body["profile"].(map[string]any); ok && prof["secret"] != "" {
		t.Errorf("SECURITY: [typedhook-refalias-beforeupdate] in-place v.Profile.Secret=\"\" was dropped by the merge-back; the client's nested secret %q survives into the UPDATE body", prof["secret"])
	}
}
