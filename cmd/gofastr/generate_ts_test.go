package main

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/framework"
)

// ============================================================================
// TS codegen: every field type maps to the expected TS primitive
// ============================================================================

func TestRenderTypeScript_FieldTypeMapping(t *testing.T) {
	cases := []struct {
		fieldType string
		wantTS    string
	}{
		{"string", "string"},
		{"text", "string"},
		{"int", "number"},
		{"integer", "number"},
		{"float", "number"},
		{"decimal", "number"},
		{"bool", "boolean"},
		{"boolean", "boolean"},
		{"uuid", "string"},
		{"timestamp", "string"},
		{"date", "string"},
		{"json", "unknown"},
		{"image", "string"},
		{"file", "string"},
		{"unknown_type_falls_back", "string"},
	}
	for _, c := range cases {
		if got := tsTypeForField(c.fieldType); got != c.wantTS {
			t.Errorf("tsTypeForField(%q) = %q, want %q", c.fieldType, got, c.wantTS)
		}
	}
}

// ============================================================================
// TS codegen emits interface fields with the correct shape
// ============================================================================

func TestRenderTypeScript_FieldShape(t *testing.T) {
	decls := []framework.EntityDeclaration{
		{
			Name:  "posts",
			Table: "posts",
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true},
				{Name: "views", Type: "int"},
				{Name: "published", Type: "bool"},
			},
		},
	}
	out := renderTypeScript(decls)

	// ID is unconditional + non-optional.
	if !strings.Contains(out, "\tid: string;") {
		t.Errorf("expected required id field, got:\n%s", out)
	}
	// Other fields are optional + camelCased.
	for _, want := range []string{
		"\ttitle?: string;",
		"\tviews?: number;",
		"\tpublished?: boolean;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// ============================================================================
// TS codegen emits relation fields with the right shape (pointer / array)
// ============================================================================

func TestRenderTypeScript_RelationFields(t *testing.T) {
	decls := []framework.EntityDeclaration{
		{
			Name:  "users",
			Table: "users",
			Fields: []framework.FieldDeclaration{
				{Name: "name", Type: "string", Required: true},
			},
		},
		{
			Name:  "posts",
			Table: "posts",
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true},
				{Name: "author_id", Type: "string"},
			},
			Relations: []framework.Relation{
				framework.BelongsTo("author", "users", "author_id"),
				framework.HasMany("comments", "comments", "post_id"),
			},
		},
	}
	out := renderTypeScript(decls)

	if !strings.Contains(out, "\tauthor?: Users;") {
		t.Errorf("expected singular author?: Users, got:\n%s", out)
	}
	if !strings.Contains(out, "\tcomments?: Comments[];") {
		t.Errorf("expected plural comments?: Comments[], got:\n%s", out)
	}
}

// ============================================================================
// TS codegen always emits the envelope generics
// ============================================================================

func TestRenderTypeScript_EmitsEnvelopes(t *testing.T) {
	decls := []framework.EntityDeclaration{
		{
			Name:   "posts",
			Table:  "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		},
	}
	out := renderTypeScript(decls)
	for _, want := range []string{
		"export interface ListResponse<T>",
		"export interface CursorPage<T>",
		"export interface BatchResult<T>",
		"export interface BatchResponse<T>",
		"export interface ApiError",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing envelope %q in:\n%s", want, out)
		}
	}
}

// ============================================================================
// TS codegen output is sorted by entity name for deterministic diffs
// ============================================================================

func TestRenderTypeScript_DeterministicOrder(t *testing.T) {
	// Declared zebra-then-aardvark; expect the rendered file to have
	// Aardvarks before Zebras.
	decls := []framework.EntityDeclaration{
		{Name: "zebras", Fields: []framework.FieldDeclaration{{Name: "stripes", Type: "int"}}},
		{Name: "aardvarks", Fields: []framework.FieldDeclaration{{Name: "claws", Type: "int"}}},
	}
	out := renderTypeScript(decls)
	aIdx := strings.Index(out, "export interface Aardvarks")
	zIdx := strings.Index(out, "export interface Zebras")
	if aIdx < 0 || zIdx < 0 {
		t.Fatalf("expected both interfaces, got:\n%s", out)
	}
	if aIdx >= zIdx {
		t.Fatalf("expected Aardvarks before Zebras in output")
	}
}
