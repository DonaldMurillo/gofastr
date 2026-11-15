package crud

import (
	"context"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// An offset page sorts on the key after the client's sort, so tied rows
// keep one order across pages; a key column the sort names is not repeated.
func TestKeyTiebreakFollowsTheSort(t *testing.T) {
	ch := &CrudHandler{Entity: &entity.Entity{Config: entity.EntityConfig{PrimaryKey: []string{"code", "scope"}}}}
	var fields []string
	for _, s := range ch.withKeyTiebreak([]filter.ParsedSort{{Field: "status"}, {Field: "scope", Desc: true}}) {
		fields = append(fields, s.Field)
	}
	if !slices.Equal(fields, []string{"status", "scope", "code"}) {
		t.Fatalf("sort columns %v, want status scope code", fields)
	}
}

// A write a hook starts inside another write's hook carries its own row's
// id on ctx, not the id of the write that started it.
func TestAfterRowSetsItsOwnID(t *testing.T) {
	ch := &CrudHandler{Entity: entity.Define("posts", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}}), Hooks: hook.NewHookRegistry()}
	var seen string
	ch.Hooks.RegisterHook(hook.AfterCreate, func(ctx context.Context, _ any) error {
		seen, _ = AuditRecordIDFor(ctx, "posts")
		return nil
	})
	parent := context.WithValue(context.Background(), auditRecordKey{}, auditRecord{entity: "posts", id: "parent"})
	if err := ch.afterRow(parent, hook.AfterCreate, map[string]any{"id": "child"}); err != nil {
		t.Fatal(err)
	}
	if seen != "child" {
		t.Fatalf("hook saw record id %q, want child", seen)
	}
}
