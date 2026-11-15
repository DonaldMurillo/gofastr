package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// The Before-hook visibility check fails closed: a key that does not fit the
// key columns, an unsafe table, and a query that errors all read as hidden.
func TestBeforeScopeCheckFailsClosed(t *testing.T) {
	db := setupDB(t, `CREATE TABLE fc (id TEXT PRIMARY KEY, team_id TEXT)`)
	if _, err := db.Exec(`INSERT INTO fc (id, team_id) VALUES ('r1', 'a')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ok := []hook.WhereClause{{SQL: "team_id = $1", Args: []any{"a"}}}
	if beforeScopeHidesKey(ctx, db, "fc", []string{"id"}, []any{"r1"}, ok) {
		t.Fatal("a visible row read as hidden")
	}
	if !beforeScopeHidesKey(ctx, db, "fc", []string{"id"}, nil, ok) {
		t.Fatal("a row with no key read as visible")
	}
	if !beforeScopeHidesKey(ctx, db, "fc; drop", []string{"id"}, []any{"r1"}, ok) {
		t.Fatal("an unsafe table read as visible")
	}
	broken := []hook.WhereClause{{SQL: "no_such_column = $1", Args: []any{"a"}}}
	if !beforeScopeHidesKey(ctx, db, "fc", []string{"id"}, []any{"r1"}, broken) {
		t.Fatal("a failed query read as visible")
	}
}

// Under WithServerWrites the BeforeGet clauses are skipped, but a guarded
// Before hook still never sees an id outside the write's owner, tenant and
// soft-delete scope.
func TestServerWritesStillProbeGuardedTarget(t *testing.T) {
	installOwnerExtractor(t)
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		ch, calls := beforeOrderWorld(t, db)
		ctx := WithServerWrites(tenant.SetTenantID(signedIn("alice"), "t1"))
		for _, id := range []string{"n-b", "n-t", "n-x", "n-missing"} {
			_, _ = ch.UpdateOne(ctx, id, map[string]any{"title": "x"})
			_ = ch.DeleteOne(ctx, id)
			if got := calls.take(); len(got) != 0 {
				t.Errorf("Before hooks ran under WithServerWrites for untouchable id %s: %v", id, got)
			}
		}
	})
}

// An upsert onto an existing row whose BeforeGet hook errors is refused and
// writes nothing: a hook error reads as hidden.
func TestUpsertHookErrorRefuses(t *testing.T) {
	notes := hookScopedNotes(t, func(_ context.Context, data any) error {
		if _, ok := data.(*hook.GetPayload); ok {
			return errors.New("scope unavailable")
		}
		return nil
	})
	if _, err := notes.UpsertOne(WithReadHooks(context.Background()), map[string]any{"id": "n2", "body": "OVERWRITTEN"}); err == nil {
		t.Fatal("upsert under an erroring BeforeGet hook succeeded")
	}
	if got := noteBody(t, notes.DB, "n2"); got != "B-NOTE-SECRET" {
		t.Fatalf("upsert under an erroring BeforeGet hook wrote the row: body %q", got)
	}
}

// A later WithElevation replaces the set of entities rather than adding to
// it, and WithoutElevation removes it.
func TestElevationReplacesItsSet(t *testing.T) {
	ctx := WithElevation(WithElevation(context.Background(), "a"), "b")
	if elevated(ctx, "a") || !elevated(ctx, "b") {
		t.Fatal("a later WithElevation added to the set instead of replacing it")
	}
	if elevated(WithoutElevation(ctx), "b") {
		t.Fatal("WithoutElevation kept the lift")
	}
}

// A cascade child whose own Before hooks error comes back as its id only:
// a child hook error reads as hidden.
func TestCascadeChildHookErrorHidesReadBack(t *testing.T) {
	ch, _ := hookScopedNotesHandler(t)
	failing := hook.NewHookRegistry()
	for _, ht := range []hook.HookType{hook.BeforeList, hook.BeforeGet} {
		failing.RegisterHook(ht, func(context.Context, any) error { return errors.New("scope unavailable") })
	}
	ch.ChildHooks = func(name string) *hook.HookRegistry {
		if name == "notes" {
			return failing
		}
		return nil
	}
	rec := sendPosts(ch, http.MethodPost, "/posts", "", `{"title":"t","notes":[{"status":"published","body":"SECRET"}]}`)
	if rec.Code != http.StatusCreated {
		return
	}
	if strings.Contains(rec.Body.String(), "SECRET") {
		t.Fatalf("cascade child under erroring Before hooks was echoed whole: %s", rec.Body.String())
	}
}

// WithServerWrites is trusted server code: the caller's BeforeGet hooks do
// not hide a record write's target from it.
func TestServerWritesSkipBeforeGetScope(t *testing.T) {
	notes := hookScopedNotes(t, teamScope("a"))
	ctx := WithServerWrites(WithReadHooks(context.Background()))
	if _, err := notes.UpdateOne(ctx, "n2", map[string]any{"body": "SERVER"}); err != nil {
		t.Fatalf("server write on a hook-hidden row: %v", err)
	}
	if got := noteBody(t, notes.DB, "n2"); got != "SERVER" {
		t.Fatalf("server write did not land: body %q", got)
	}
}
