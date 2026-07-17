package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// statusHandler builds a handler over a table with a multi-word column
// (status_id) using the framework's DEFAULT JSONCase (CaseCamel) — the
// configuration every host gets unless it opts into WithJSONCase(CaseSnake).
// This is the shape issue #69 was filed against: hook-adjacent code expects
// snake_case keys, but the default handler speaks camelCase.
func statusHandlerCamel(t *testing.T) *CrudHandler {
	t.Helper()
	db := setupDB(t, `CREATE TABLE tickets (id TEXT PRIMARY KEY, status_id TEXT, version INTEGER)`)
	ent := entity.Define("tickets", entity.EntityConfig{
		Name: "tickets", Table: "tickets",
		Fields: []schema.Field{
			{Name: "status_id", Type: schema.String},
			{Name: "version", Type: schema.Int},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db) // default JSONCase == CaseCamel
}

// runUpdateCapturingPre creates one row, updates it, and returns the
// pre-image an AfterUpdate hook observed via AuditPreImageFromContext.
// Shared setup for all the casing-contract tests below.
func runUpdateCapturingPre(t *testing.T) map[string]any {
	t.Helper()
	ch := statusHandlerCamel(t)
	// CreateOne/UpdateOne take snake_cased bodies (the in-process contract —
	// see their doc comments); JSONCase only governs the HTTP request/
	// response boundary and, per issue #69, the pre-image map.
	created, err := ch.CreateOne(context.Background(), map[string]any{"status_id": "open", "version": float64(1)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var capturedPre map[string]any
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterUpdate, func(ctx context.Context, data any) error {
		capturedPre = AuditPreImageFromContext(ctx)
		return nil
	})

	id := created["id"].(string)
	if _, err := ch.UpdateOne(context.Background(), id, map[string]any{"status_id": "closed"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if capturedPre == nil {
		t.Fatal("expected a captured pre-image")
	}
	return capturedPre
}

// TestPreImage_RawMapIsCamelCased documents (and locks in) the issue #69
// casing contract on the raw accessor: under the default handler JSONCase,
// AuditPreImageFromContext keys the pre-image by camelCase ("statusId"),
// NOT the DB column name ("status_id"). A hook doing pre["status_id"]
// against a default handler silently gets nothing back — this is the bug
// reproduction: before the fix in this change, nothing surfaced that
// contract, so it was easy to hit and hard to notice (casing-identical
// keys like "version" happen to work either way).
func TestPreImage_RawMapIsCamelCased(t *testing.T) {
	pre := runUpdateCapturingPre(t)
	if _, ok := pre["statusId"]; !ok {
		t.Errorf("expected camelCase key \"statusId\"; got keys %v", keysOf(pre))
	}
	if _, ok := pre["status_id"]; ok {
		t.Error("raw pre-image unexpectedly has a snake_case key; casing contract changed?")
	}
}

// TestPreImage_SnakeAccessorFixesNaiveRead proves the fix: a hook that
// switches to AuditPreImageSnakeFromContext (instead of the raw
// accessor) sees the pre-update value under the DB column name, no matter
// the handler's configured JSONCase.
func TestPreImage_SnakeAccessorFixesNaiveRead(t *testing.T) {
	pre := runUpdateCapturingPre(t)
	snake := AuditPreImageSnakeFromContext(contextWithPreImage(pre))
	if got, ok := snake["status_id"]; !ok || got != "open" {
		t.Errorf("snake accessor status_id = %v, ok=%v; want \"open\", true", got, ok)
	}
}

// TestPreImage_TypedAccessorDecodesField proves AuditPreImageAs[T] decodes
// the pre-update row into a struct using the same camelCase-tag convention
// typed hooks already use, so a hook can read the old value by field
// instead of guessing at map-key casing.
func TestPreImage_TypedAccessorDecodesField(t *testing.T) {
	pre := runUpdateCapturingPre(t)

	type ticket struct {
		StatusID string `json:"statusId"`
		Version  int    `json:"version"`
	}
	got, ok := AuditPreImageAs[ticket](contextWithPreImage(pre))
	if !ok {
		t.Fatal("AuditPreImageAs returned ok=false")
	}
	if got.StatusID != "open" {
		t.Errorf("StatusID = %q, want %q", got.StatusID, "open")
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
}

// TestPreImage_TypedAccessorNoPreImage proves the zero-value/false case:
// no pre-image on the context (e.g. a hook fired outside doUpdate/
// doDelete) must not panic and must report ok=false.
func TestPreImage_TypedAccessorNoPreImage(t *testing.T) {
	type ticket struct {
		StatusID string `json:"statusId"`
	}
	got, ok := AuditPreImageAs[ticket](context.Background())
	if ok {
		t.Error("expected ok=false with no pre-image on context")
	}
	if got.StatusID != "" {
		t.Errorf("expected zero value, got %+v", got)
	}
}

// contextWithPreImage re-wraps an already-captured pre-image map onto a
// fresh context, so the accessor tests can run outside the hook callback
// (WithAuditPreImage is the only writer of the context key).
func contextWithPreImage(pre map[string]any) context.Context {
	return WithAuditPreImage(context.Background(), pre)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
