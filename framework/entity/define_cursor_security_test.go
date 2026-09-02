package entity

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Cursor columns are a declared query surface exactly like SearchFields
// and ReadScope: they reach ORDER BY and the keyset WHERE, and the
// emitted cursor token carries the stored value verbatim. The property
// family pinned here: a column named by the cursor configuration must
// be a DECLARED, non-Hidden, non-NoQuery field, checked at Define,
// across every spelling of the configuration (CursorField, CursorFields
// composite, and the implicit primary-key default / auto tiebreak).
//
// The Hidden/NoQuery halves of this family are implemented in Define
// (entity.go, cursorCols loop) but had no test. The declared-half is
// NOT implemented: a CursorField naming an unknown column is silently
// accepted and surfaces as a per-request "no such column" 500 — the
// exact failure mode the SearchFields and ReadScope checks exist to
// prevent. Those tests live in define_validation_test.go; this file
// extends the same property to the cursor surface.

// expectCursorPanic asserts Define panics with wantSubstr, reusing the
// shape of expectDefinePanic from define_validation_test.go.
func expectCursorPanic(t *testing.T, wantSubstr string, config EntityConfig) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("Define did not panic (want %q)", wantSubstr)
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, wantSubstr) {
			t.Fatalf("panic = %v, want substring %q", r, wantSubstr)
		}
	}()
	Define("bad", config)
}

func TestCursorFieldHiddenPanics(t *testing.T) {
	expectCursorPanic(t, "cursor field \"internal_key\" is Hidden", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "internal_key", Type: schema.String, Hidden: true},
		},
		Pagination: &PaginationConfig{CursorField: "internal_key"},
	})
}

func TestCursorFieldNoQueryPanics(t *testing.T) {
	expectCursorPanic(t, "cursor field \"slug\" is NoQuery", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "slug", Type: schema.String, NoQuery: true},
		},
		Pagination: &PaginationConfig{CursorField: "slug"},
	})
}

// The implicit default keyset column (the primary key) is checked too:
// a NoQuery id used to leak its stored value through the emitted token
// while every filter and sort refused it.
func TestCursorDefaultKeysetNoQueryPanics(t *testing.T) {
	expectCursorPanic(t, "cursor field \"id\" is NoQuery", EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, NoQuery: true},
			{Name: "title", Type: schema.String},
		},
	})
}

// A composite cursor silently gains the primary key as its tiebreak;
// only validating the declared members missed a NoQuery id entirely.
func TestCursorCompositeTiebreakNoQueryPanics(t *testing.T) {
	expectCursorPanic(t, "cursor field \"id\" is NoQuery", EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, NoQuery: true},
			{Name: "title", Type: schema.String},
			{Name: "created_at", Type: schema.Timestamp},
		},
		Pagination: &PaginationConfig{CursorFields: []string{"created_at"}},
	})
}

func TestCursorCompositeMemberHiddenPanics(t *testing.T) {
	expectCursorPanic(t, "cursor field \"votes\" is Hidden", EntityConfig{
		Fields: []schema.Field{
			{Name: "votes", Type: schema.Int, Hidden: true},
			{Name: "title", Type: schema.String},
		},
		Pagination: &PaginationConfig{CursorFields: []string{"votes"}},
	})
}

// Declared-but-clean cursor columns must keep defining fine, so the
// panics above are property gates, not a blanket refusal.
func TestCursorCleanColumnsDefine(t *testing.T) {
	e := Define("posts", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "created_at", Type: schema.Timestamp},
		},
		Pagination: &PaginationConfig{CursorFields: []string{"created_at"}},
	})
	if e.Config.Pagination.CursorFields[0] != "created_at" {
		t.Fatalf("CursorFields = %v", e.Config.Pagination.CursorFields)
	}
}

// TestCursorFieldUndeclaredPanics: a CursorField naming a column the
// entity does not declare must fail Define, mirroring SearchFields
// ("is not a declared field") and ReadScope ("is not a declared
// field"). Today it is silently accepted and every keyset request
// fails at query time with "no such column" — a per-request 500 that
// nothing at definition time reported. The cursor column is
// developer-declared, but so are SearchFields entries, and Define
// already treats THOSE as fail-loud misconfiguration. The parity, not
// the threat model, is the contract being pinned.
func TestCursorFieldUndeclaredPanics(t *testing.T) {
	expectCursorPanic(t, "is not a declared field", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
		Pagination: &PaginationConfig{CursorField: "createdat"}, // typo for created_at
	})
}

// TestCursorCompositeMemberUndeclaredPanics: same property for the
// composite spelling — every member must be declared, including when
// other members are fine.
func TestCursorCompositeMemberUndeclaredPanics(t *testing.T) {
	expectCursorPanic(t, "is not a declared field", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "created_at", Type: schema.Timestamp},
		},
		Pagination: &PaginationConfig{CursorFields: []string{"created_at", "postioned_at"}},
	})
}
