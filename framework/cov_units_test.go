package framework

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// ============================================================================
// audit.go — pure helper branches
// ============================================================================

// defaultRedact returns the same (empty) map unchanged.
func TestCovDefaultRedactEmpty(t *testing.T) {
	in := map[string]any{}
	if got := defaultRedact(in); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
	// nil input takes the len==0 short-circuit too.
	if got := defaultRedact(nil); got != nil {
		t.Fatalf("expected nil passthrough, got %v", got)
	}
}

// auditMeta returns nil when no request is attached and when meta is empty.
func TestCovAuditMetaNil(t *testing.T) {
	if m := auditMeta(context.Background()); m != nil {
		t.Fatalf("expected nil meta with no request, got %v", m)
	}
	// A request with no RemoteAddr and no User-Agent yields empty meta → nil.
	ctx := crud.WithAuditRequest(context.Background(), &http.Request{})
	if m := auditMeta(ctx); m != nil {
		t.Fatalf("expected nil meta for empty request, got %v", m)
	}
	// A request with a User-Agent but no IP yields non-nil meta.
	r := &http.Request{Header: http.Header{}}
	r.Header.Set("User-Agent", "cov-agent")
	if m := auditMeta(crud.WithAuditRequest(context.Background(), r)); m == nil || m["user_agent"] != "cov-agent" {
		t.Fatalf("expected user_agent meta, got %v", m)
	}
}

// clientIP handles empty addr and addr with no port.
func TestCovClientIP(t *testing.T) {
	if got := clientIP(&http.Request{RemoteAddr: ""}); got != "" {
		t.Fatalf("empty addr: got %q", got)
	}
	if got := clientIP(&http.Request{RemoteAddr: "10.0.0.1:5050"}); got != "10.0.0.1" {
		t.Fatalf("with port: got %q", got)
	}
	if got := clientIP(&http.Request{RemoteAddr: "no-port"}); got != "no-port" {
		t.Fatalf("no port: got %q", got)
	}
}

// stringifyPK: nil row, missing pk with case-insensitive fallback, total miss.
func TestCovStringifyPK(t *testing.T) {
	if got := stringifyPK(nil, "id"); got != "" {
		t.Fatalf("nil row: got %q", got)
	}
	// pk present directly
	if got := stringifyPK(map[string]any{"id": 7}, "id"); got != "7" {
		t.Fatalf("direct pk: got %q", got)
	}
	// fallback: requested pk absent, but "ID" present
	if got := stringifyPK(map[string]any{"ID": "abc"}, "uid"); got != "abc" {
		t.Fatalf("fallback ID: got %q", got)
	}
	// total miss
	if got := stringifyPK(map[string]any{"name": "x"}, "uid"); got != "" {
		t.Fatalf("total miss: got %q", got)
	}
}

// buildAuditCreateDiff falls back to original when redacted can't marshal,
// and to the {} sentinel when both fail.
func TestCovBuildCreateDiffFallback(t *testing.T) {
	bad := map[string]any{"fn": func() {}} // not JSON-marshalable
	good := map[string]any{"ok": 1}
	out := buildAuditCreateDiff(bad, good, nil)
	if string(out) == "" {
		t.Fatal("expected a diff")
	}
	// both unmarshalable → sentinel
	out2 := buildAuditCreateDiff(bad, bad, nil)
	if string(out2) != `{"new":{}}` {
		t.Fatalf("expected sentinel, got %s", out2)
	}
}

// buildAuditUpdateDiff exercises the originalOld second-fallback chain.
func TestCovBuildUpdateDiffFallback(t *testing.T) {
	bad := map[string]any{"fn": func() {}}
	goodOld := map[string]any{"old": 1}
	// new poisoned, old redacted poisoned, originalOld good → recovers old.
	out := buildAuditUpdateDiff(bad, bad, goodOld, map[string]any{"n": 2}, nil)
	if string(out) == "" || string(out) == `{"new":{}}` {
		t.Fatalf("expected recovery via originalOld, got %s", out)
	}
	// everything poisoned → sentinel
	out2 := buildAuditUpdateDiff(bad, bad, bad, bad, nil)
	if string(out2) != `{"new":{}}` {
		t.Fatalf("expected sentinel, got %s", out2)
	}
}

// buildAuditDeleteDiff: nil when nothing captured; originalOld fallback; sentinel.
func TestCovBuildDeleteDiff(t *testing.T) {
	if d := buildAuditDeleteDiff(nil, nil, nil); d != nil {
		t.Fatalf("expected nil with nothing captured, got %s", d)
	}
	bad := map[string]any{"fn": func() {}}
	good := map[string]any{"id": 1}
	// redacted poisoned, original good → recovers.
	out := buildAuditDeleteDiff(bad, good, nil)
	if string(out) == "" || string(out) == `{"old":{}}` {
		t.Fatalf("expected recovery via originalOld, got %s", out)
	}
	// both poisoned, but meta present so the nil short-circuit doesn't fire.
	out2 := buildAuditDeleteDiff(bad, bad, map[string]any{"x": func() {}})
	if string(out2) != `{"old":{}}` {
		t.Fatalf("expected sentinel, got %s", out2)
	}
}

// ============================================================================
// typed_hooks.go — helper branches
// ============================================================================

// unmarshalHookPayload handles a non-map payload (string id) through JSON.
func TestCovUnmarshalHookPayloadString(t *testing.T) {
	var s string
	if err := unmarshalHookPayload("rec_123", &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s != "rec_123" {
		t.Fatalf("got %q", s)
	}
}

// mergeStructIntoMap falls through to the additive marshal merge for a
// non-struct payload.
func TestCovMergeNonStructFallsBack(t *testing.T) {
	dest := map[string]any{}
	before := map[string]any{"a": 1}
	after := map[string]any{"a": 2, "b": 3}
	if err := mergeStructIntoMap(before, after, dest); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if dest["a"] == nil || dest["b"] == nil {
		t.Fatalf("expected additive merge, got %v", dest)
	}
}

// mergeStructIntoMap skips unexported and json:"-" fields.
func TestCovMergeSkipsHiddenFields(t *testing.T) {
	type s struct {
		Visible string `json:"visible"`
		Skipped string `json:"-"`
		hidden  string //nolint:unused
	}
	dest := map[string]any{}
	before := &s{Visible: "a", Skipped: "x", hidden: "h"}
	after := &s{Visible: "b", Skipped: "y", hidden: "z"}
	if err := mergeStructIntoMap(before, after, dest); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if dest["visible"] != "b" {
		t.Fatalf("visible not merged: %v", dest)
	}
	if _, ok := dest["skipped"]; ok {
		t.Fatalf("json:\"-\" field leaked into dest: %v", dest)
	}
}

// jsonFieldName covers the no-tag and skip branches.
func TestCovJSONFieldName(t *testing.T) {
	type s struct {
		Plain   string
		Skipped string `json:"-"`
	}
	tp := reflect.TypeOf(s{})
	plain, skip := jsonFieldName(tp.Field(0))
	if skip || plain != "Plain" {
		t.Fatalf("plain field: name=%q skip=%v", plain, skip)
	}
	_, skip = jsonFieldName(tp.Field(1))
	if !skip {
		t.Fatal("expected json:\"-\" to skip")
	}
}

// marshalMergeIntoMap copies snake-cased keys into dest.
func TestCovMarshalMergeIntoMap(t *testing.T) {
	dest := map[string]any{}
	type s struct {
		FirstName string `json:"firstName"`
	}
	if err := marshalMergeIntoMap(&s{FirstName: "x"}, dest); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if dest["first_name"] != "x" {
		t.Fatalf("expected snake key, got %v", dest)
	}
}

// marshalMergeIntoMap surfaces a marshal error for an unmarshalable src.
func TestCovMarshalMergeError(t *testing.T) {
	dest := map[string]any{}
	if err := marshalMergeIntoMap(func() {}, dest); err == nil {
		t.Fatal("expected marshal error")
	}
}

// ============================================================================
// typed_hooks.go — typed-hook payload-type error branches (List/Get)
// ============================================================================

// The typed List/Get hooks return a contract-drift error when the payload
// isn't the expected pointer type. Trigger by firing the registered untyped
// wrapper with a wrong-typed payload.
func TestCovTypedListGetWrongPayload(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())

	OnBeforeList(app, "x", func(_ context.Context, _ *hook.ListPayload) error { return nil })
	OnAfterList(app, "x", func(_ context.Context, _ *hook.ListPayload) error { return nil })
	OnBeforeGet(app, "x", func(_ context.Context, _ *hook.GetPayload) error { return nil })
	OnAfterGet(app, "x", func(_ context.Context, _ *hook.GetPayload) error { return nil })

	hr := app.HookRegistry("x")
	ctx := context.Background()
	for _, ht := range []hook.HookType{hook.BeforeList, hook.AfterList, hook.BeforeGet, hook.AfterGet} {
		if err := hr.ExecuteHooks(ctx, ht, "not-the-right-type"); err == nil {
			t.Fatalf("expected contract-drift error for %v", ht)
		}
	}
	// nil payload also trips the guard.
	if err := hr.ExecuteHooks(ctx, hook.BeforeList, (*hook.ListPayload)(nil)); err == nil {
		t.Fatal("expected error for nil ListPayload")
	}
}

// ============================================================================
// i18n.go — Translator accessor
// ============================================================================

func TestCovTranslatorAccessor(t *testing.T) {
	if (*App)(nil).Translator() != nil {
		t.Fatal("nil app should return nil translator")
	}
	app := NewApp(WithoutDefaultMiddleware())
	if app.Translator() != nil {
		t.Fatal("app without WithI18n should have nil translator")
	}
	tr := i18n.NewTranslator(i18n.NewMapCatalog(), "en")
	app2 := NewApp(WithI18n(tr))
	if app2.Translator() != tr {
		t.Fatal("WithI18n translator not returned")
	}
}
