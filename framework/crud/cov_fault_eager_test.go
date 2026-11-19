package crud

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// covFaultRelWorld builds the same posts/users/comments/tags world as
// covRelWorld but over a fault-injectable DB so the SECOND query and
// rows.Err iteration branches in the eager loaders can be exercised (the
// missing-table world only fails the FIRST query).
func covFaultRelWorld(t *testing.T) (*CrudHandler, *sql.DB, stubRegistry) {
	t.Helper()
	return covRelWorldOver(t, covSetupFaultDB)
}

// --- eager.go (unfiltered EagerLoad) DB-error branches ---

// TestEagerLoad_DBFaults walks each relation shape over the fault-injectable
// world: the flagged query (source or target) fails, or its rows iteration
// does, and EagerLoad must surface the injected error. The BelongsTo
// source-query case is eager.go's source scan, whose error path none of the
// other fault worlds reach.
func TestEagerLoad_DBFaults(t *testing.T) {
	m2m := entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")
	m2m.ForeignKey = []string{"tag_id"}
	for _, tc := range []struct {
		name  string
		rel   entity.Relation
		fault func(*covFaults)
	}{
		{"HasMany_NextErr", entity.HasMany("comments", "comments", "post_id"),
			func(c *covFaults) { c.nextErrOn = "FROM comments " }},
		{"BelongsTo_SrcQueryErr", entity.BelongsTo("author", "users", "author_id"),
			func(c *covFaults) { c.queryErrOn = "FROM posts " }},
		{"BelongsTo_SrcNextErr", entity.BelongsTo("author", "users", "author_id"),
			func(c *covFaults) { c.nextErrOn = "FROM posts " }},
		{"BelongsTo_TgtQueryErr", entity.BelongsTo("author", "users", "author_id"),
			func(c *covFaults) { c.queryErrOn = "FROM users " }},
		{"BelongsTo_TgtNextErr", entity.BelongsTo("author", "users", "author_id"),
			func(c *covFaults) { c.nextErrOn = "FROM users " }},
		{"ManyToMany_NextErr", m2m,
			func(c *covFaults) { c.nextErrOn = "FROM tags " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch, db, reg := covFaultRelWorld(t)
			covFault.set(tc.fault)
			_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{tc.rel}, []string{"p1"}, reg)
			if !errors.Is(err, errCovInjected) {
				t.Fatalf("%s = %v, want injected", tc.name, err)
			}
		})
	}
}

// --- eager_filtered.go DB-error branches (via List include path) ---

// TestFilteredBelongsToSrcFaults drives the BelongsTo source scan through
// loadIncludeNode directly: the source query and its rows iteration each
// fail, and the injected error must surface.
func TestFilteredBelongsToSrcFaults(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fault func(*covFaults)
		want  string
	}{
		{"SrcQueryErr", func(c *covFaults) { c.queryErrOn = "FROM posts " }, "filtered belongsTo src query"},
		{"SrcNextErr", func(c *covFaults) { c.nextErrOn = "FROM posts " }, "filtered belongsTo src rows.Err"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, db, _ := covFaultRelWorld(t)
			covFault.set(tc.fault)
			err := loadIncludeNode(context.Background(), db, "posts", []string{"id"}, belongsToAuthorNode(), []string{"p1"}, newResult("p1"), newIncludeBudget())
			if !errors.Is(err, errCovInjected) {
				t.Fatalf("%s = %v, want injected", tc.want, err)
			}
		})
	}
}

func belongsToAuthorNode() *IncludeNode {
	return &IncludeNode{Relation: entity.BelongsTo("author", "users", "author_id"), Target: guardTarget("users")}
}

// TestFilteredEagerFaults walks the public List include path over the
// fault-injectable world: the flagged query (source or target) fails, or
// its rows iteration does, and the filtered eager loader must surface
// the injected error.
func TestFilteredEagerFaults(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fault func(*covFaults)
		inc   string
		want  string
	}{
		{"HasMany_NextErr", func(c *covFaults) { c.nextErrOn = "FROM comments " }, "comments", "filtered hasMany rows.Err"},
		{"BelongsTo_TgtQueryErr", func(c *covFaults) { c.queryErrOn = "FROM users " }, "author", "filtered belongsTo tgt query"},
		{"BelongsTo_TgtNextErr", func(c *covFaults) { c.nextErrOn = "FROM users " }, "author", "filtered belongsTo tgt rows.Err"},
		{"ManyToMany_QueryErr", func(c *covFaults) { c.queryErrOn = "__parent_id" }, "tags", "filtered m2m query"},
		{"ManyToMany_NextErr", func(c *covFaults) { c.nextErrOn = "__parent_id" }, "tags", "filtered m2m rows.Err"},
		{"HasMany_QueryErr", func(c *covFaults) { c.queryErrOn = "FROM comments " }, "comments", "filtered hasMany query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch, _, _ := covFaultRelWorld(t)
			covFault.set(tc.fault)
			_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{tc.inc}})
			if !errors.Is(err, errCovInjected) {
				t.Fatalf("%s = %v, want injected", tc.want, err)
			}
		})
	}
}
