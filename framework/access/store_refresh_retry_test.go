package access

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Refreshes that fail during an outage are retried after the worker's
// delay, so each role converges once the database is back. drainDirty no
// longer retries inside its pass (TestGrantRefreshDownDBStops), which makes
// this delayed retry the only path a missed grant or revoke has back.
//
// The revoked role keeps no grant at all, and the full reload the worker
// queues rebuilds only roles that still hold a baseline or a DB grant: the
// failed role's own re-mark is what clears it.
func TestGrantRefreshRetriesAfterOutage(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	policy := NewRolePolicy()
	s := NewGrantStore(db, policy)
	if err := s.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	stop, err := s.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	can := func(role string) bool {
		return Can(WithRoles(WithPolicy(ctx, policy), []string{role}), Permission("posts:read"))
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(reloadTimeout + 3*time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatal(what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	exec(`INSERT INTO access_grants (role, permission) VALUES ('editor', 'posts:read')`)
	s.markDirty("editor")
	waitFor("the editor grant never loaded", func() bool { return can("editor") })

	// The grants table is unreachable while both refreshes run: editor is
	// revoked to nothing, viewer is granted.
	// One wake for both, so nothing but the worker's own retry wakes it
	// again.
	exec(`ALTER TABLE access_grants RENAME TO access_grants_away`)
	s.dirtyMu.Lock()
	s.dirty["editor"] = true
	s.dirtyMu.Unlock()
	s.markDirty("viewer")
	time.Sleep(200 * time.Millisecond)
	exec(`DELETE FROM access_grants_away WHERE role = 'editor'`)
	exec(`INSERT INTO access_grants_away (role, permission) VALUES ('viewer', 'posts:read')`)
	exec(`ALTER TABLE access_grants_away RENAME TO access_grants`)

	waitFor("the viewer grant never converged after the outage", func() bool { return can("viewer") })
	waitFor("the editor revoke never converged after the outage", func() bool { return !can("editor") })
}
