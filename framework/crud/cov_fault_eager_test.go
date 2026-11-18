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

func TestEagerHasMany_NextErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	rel := entity.HasMany("comments", "comments", "post_id")
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM comments " })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{rel}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("hasMany rows.Err = %v, want injected", err)
	}
}

func TestEagerBelongsTo_SrcNextErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	rel := entity.BelongsTo("author", "users", "author_id")
	// Source query selects from posts; fail iteration of it.
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM posts " })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{rel}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("belongsTo src rows.Err = %v, want injected", err)
	}
}

func TestEagerBelongsTo_TgtQueryErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	rel := entity.BelongsTo("author", "users", "author_id")
	// Source query (posts) runs; target query (users) fails.
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM users " })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{rel}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("belongsTo tgt query = %v, want injected", err)
	}
}

func TestEagerBelongsTo_TgtNextErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	rel := entity.BelongsTo("author", "users", "author_id")
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM users " })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{rel}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("belongsTo tgt rows.Err = %v, want injected", err)
	}
}

func TestEagerManyToMany_NextErr(t *testing.T) {
	ch, db, reg := covFaultRelWorld(t)
	m2m := entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")
	m2m.ForeignKey = []string{"tag_id"}
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM tags " })
	_, err := EagerLoad(context.Background(), db, ch.Entity, []entity.Relation{m2m}, []string{"p1"}, reg)
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("m2m rows.Err = %v, want injected", err)
	}
}

// --- eager_filtered.go DB-error branches (via List include path) ---

func TestFilteredHasMany_NextErr(t *testing.T) {
	ch, db, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM comments " })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"comments"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered hasMany rows.Err = %v, want injected", err)
	}
	_ = db
}

func belongsToAuthorNode() *IncludeNode {
	return &IncludeNode{Relation: entity.BelongsTo("author", "users", "author_id"), Target: guardTarget("users")}
}

func TestFilteredBelongsTo_SrcQueryErr(t *testing.T) {
	_, db, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM posts " })
	err := loadIncludeNode(context.Background(), db, "posts", []string{"id"}, belongsToAuthorNode(), []string{"p1"}, newResult("p1"), newIncludeBudget())
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered belongsTo src query = %v, want injected", err)
	}
}

func TestFilteredBelongsTo_SrcNextErr(t *testing.T) {
	_, db, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM posts " })
	err := loadIncludeNode(context.Background(), db, "posts", []string{"id"}, belongsToAuthorNode(), []string{"p1"}, newResult("p1"), newIncludeBudget())
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered belongsTo src rows.Err = %v, want injected", err)
	}
}

func TestFilteredBelongsTo_TgtQueryErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM users " })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"author"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered belongsTo tgt query = %v, want injected", err)
	}
}

func TestFilteredBelongsTo_TgtNextErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "FROM users " })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"author"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered belongsTo tgt rows.Err = %v, want injected", err)
	}
}

func TestFilteredManyToMany_QueryErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "__parent_id" })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"tags"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered m2m query = %v, want injected", err)
	}
}

func TestFilteredManyToMany_NextErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "__parent_id" })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"tags"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered m2m rows.Err = %v, want injected", err)
	}
}

func TestFilteredHasMany_QueryErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "FROM comments " })
	_, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"comments"}})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("filtered hasMany query = %v, want injected", err)
	}
}
