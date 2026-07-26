package framework

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Two entities with DIFFERENT names but the SAME Table also share one DB
// table, so an incompatible column between them has exactly the same
// consequence as between two versions of one name: migrate emits DDL for the
// table twice and the surviving schema depends on map iteration order.
//
// This is the "distinct names, shared table" shape — the workaround people
// reached for before the registry became version-aware — so it stays reachable
// and has to be caught by the same rule.
func TestSharedTableConflict_DifferentNames(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic: two entities share table \"posts\" with incompatible 'title' columns")
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, "title") {
			t.Errorf("panic should name the conflicting column 'title':\n%s", msg)
		}
		if !strings.Contains(msg, "posts") {
			t.Errorf("panic should name the shared table 'posts':\n%s", msg)
		}
	}()

	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	app.Entity("postsLegacy", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.Int}}, // incompatible
	}.WithTimestamps(false))
}

// The benign side of the same rule: distinct names on distinct tables that
// happen to share a column name are unrelated and must register cleanly.
func TestSharedTableConflict_DistinctTablesUnaffected(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	app.Entity("notes", entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.Int}},
	}.WithTimestamps(false))
}
