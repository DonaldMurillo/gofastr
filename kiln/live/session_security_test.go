package live_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property: the ephemeral DB is derived state of the journal. Every
// truncation surface — reset_session, undo, an approved delete_entity —
// rewrites the world from the log, but nothing ever drops the tables or
// rows the old world created: kiln/db's Migrate is additive-only
// (alignColumns adds columns, nothing drops tables) and applySideEffects
// never runs in reverse. cmd/kiln scopes one ephemeral SQLite "to the
// session" and the ResetSession contract says "the DB schema is rebuilt
// from scratch", but a reset or deleted entity leaves every row in
// place, so re-adding the entity resurrects the previous world's data
// through the public CRUD surface. The journal is also supposed to be
// sufficient to rebuild: live.New replays add_seed entries into world IR
// but never re-runs the seed side effects, so a restart (fresh ephemeral
// DB, same journal) serves an empty table for a world the log says is
// seeded.
func TestResetLeavesNoStaleRowsInDB(t *testing.T) {
	marker := "stale-row-from-old-world"

	// boot builds a live runtime over a journal at path, adds the posts
	// entity and one seeded row, and returns the harness pieces.
	boot := func(t *testing.T, j journal.Journal, d *dbHandle) (*live.Live, *protocol.Tools) {
		t.Helper()
		l, err := live.New(j, d.factory)
		if err != nil {
			t.Fatalf("live.New: %v", err)
		}
		tools := protocol.New(l)
		if res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{
			Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
		}); !res.OK {
			t.Fatalf("add_entity: %+v", res)
		}
		if res := tools.AddSeed(t.Context(), protocol.AddSeedArgs{
			Seed: &world.Seed{Entity: "posts", Rows: []map[string]any{{"title": marker}}},
		}); !res.OK {
			t.Fatalf("add_seed: %+v", res)
		}
		return l, tools
	}

	rowVisible := func(t *testing.T, l *live.Live) bool {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/posts status = %d body=%s", rec.Code, rec.Body.String())
		}
		return strings.Contains(rec.Body.String(), marker)
	}

	t.Run("reset_session then re-add serves empty table", func(t *testing.T) {
		d := newDBHandle(t)
		l, tools := boot(t, journal.NewMemory(), d)
		if !rowVisible(t, l) {
			t.Fatal("fixture: seeded row not visible before reset")
		}
		if res := tools.ResetSession(t.Context(), protocol.ResetSessionArgs{}); !res.OK {
			t.Fatalf("reset_session: %+v", res)
		}
		if res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{
			Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
		}); !res.OK {
			t.Fatalf("re-add posts: %+v", res)
		}
		if rowVisible(t, l) {
			t.Errorf("SECURITY: the new session serves the previous session's rows.\n" +
				"reset_session truncated the journal and rebuilt an empty world, but the ephemeral DB kept\n" +
				"the old table and rows; re-adding the entity resurrects data the reset was supposed to\n" +
				"destroy (cmd/kiln scopes the DB \"to the session\", ResetSession claims the schema is\n" +
				"rebuilt from scratch).")
		}
	})

	t.Run("undo of add_seed leaves no rows", func(t *testing.T) {
		d := newDBHandle(t)
		l, tools := boot(t, journal.NewMemory(), d)
		if !rowVisible(t, l) {
			t.Fatal("fixture: seeded row not visible before undo")
		}
		if res := tools.Undo(t.Context(), protocol.UndoArgs{}); !res.OK {
			t.Fatalf("undo: %+v", res)
		}
		l.ReadSession(func(sess *journal.Session) {
			if len(sess.World.Seeds) != 0 {
				t.Fatalf("fixture: undo left %d seeds in the world", len(sess.World.Seeds))
			}
		})
		if rowVisible(t, l) {
			t.Errorf("SECURITY: undo removed the add_seed entry from the journal, but its rows are\n" +
				"still served: the durable DB now shows state no journal entry authorizes.")
		}
	})

	t.Run("approved delete_entity then re-add serves empty table", func(t *testing.T) {
		d := newDBHandle(t)
		l, tools := boot(t, journal.NewMemory(), d)
		if !rowVisible(t, l) {
			t.Fatal("fixture: seeded row not visible before delete")
		}
		if res := tools.ProposePlan(t.Context(), protocol.ProposePlanArgs{
			PlanID: "p-drop", Steps: []string{"drop posts"},
			Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
		}); !res.OK {
			t.Fatalf("propose_plan: %+v", res)
		}
		if res := tools.ApprovePlan(t.Context(), protocol.ApprovePlanArgs{PlanID: "p-drop"}); !res.OK {
			t.Fatalf("approve_plan: %+v", res)
		}
		if res := tools.DeleteEntity(t.Context(), protocol.DeleteEntityArgs{Name: "posts", PlanID: "p-drop"}); !res.OK {
			t.Fatalf("delete_entity: %+v", res)
		}
		if res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{
			Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
		}); !res.OK {
			t.Fatalf("re-add posts: %+v", res)
		}
		if rowVisible(t, l) {
			t.Errorf("SECURITY: an approved, journaled delete_entity is reversible by re-adding the\n" +
				"entity: the plan-gated destruction left the rows in the ephemeral DB and they resurrect\n" +
				"through the public CRUD surface (kiln/db's doc: a disposable DB \"so destructive entity\n" +
				"edits are safe\").")
		}
	})

	t.Run("reboot from journal rebuilds seeded rows", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "session.jsonl")
		j, err := journal.OpenJSONL(path)
		if err != nil {
			t.Fatal(err)
		}
		defer j.Close()
		fresh := newDBHandle(t)
		boot(t, j, fresh)

		// A restart gets a fresh ephemeral DB (cmd/kiln's model) but the
		// SAME journal: replay must reconstruct the seeded rows.
		rebootDB := newDBHandle(t)
		l2, err := live.New(j, rebootDB.factory)
		if err != nil {
			t.Fatalf("reboot live.New: %v", err)
		}
		if !rowVisible(t, l2) {
			t.Errorf("SECURITY: reboot replayed the add_seed entry into world IR but never re-ran the\n" +
				"seed side effect, so the journal's own record says the world is seeded while the DB\n" +
				"serves an empty table. The log is supposed to be the complete state derivation\n" +
				"(live.New replays everything else); seeds exist only in the process that wrote them.")
		}
	})
}

// Property: a seed the runtime cannot apply must leave no durable
// record. live.Apply's contract (live.go): the entry is validated and
// ONLY THEN persisted, "so a poison entry that fails the rebuild never
// reaches the durable log... On any failure the pre-entry state is
// restored". applySideEffects runs AFTER the durable Append and its
// failure returns an error without any rollback — so an add_seed whose
// entity does not exist (ApplySeeds: no such table; the ident-validation
// refusals pinned in kiln/render reach the same branch) reports failure
// to the agent while the entry is already in the journal AND the
// in-memory world. Every restart and every `kiln freeze` then carries a
// seed the runtime itself refused to apply.
func TestFailedSeedSideEffectRollsBack(t *testing.T) {
	d := newDBHandle(t)
	l, err := live.New(journal.NewMemory(), d.factory)
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}
	tools := protocol.New(l)
	if res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	}); !res.OK {
		t.Fatalf("add_entity: %+v", res)
	}

	// Surface 1: ingestion. A seed for an entity that is not in the
	// world is a validation error, not a half-applied side effect.
	res := tools.AddSeed(t.Context(), protocol.AddSeedArgs{
		Seed: &world.Seed{Entity: "ghost", Rows: []map[string]any{{"title": "x"}}},
	})
	if res.OK {
		t.Fatal("fixture: add_seed for a missing entity reported success")
	}
	if res.Kind != "validation" {
		t.Errorf("SECURITY: add_seed for a missing entity failed only at the side-effect stage "+
			"(kind=%q, error=%.120s): the entry was journaled before the failure. Ingestion must "+
			"refuse it like every other shape error.", res.Kind, res.Error)
	}

	// Surface 2: the durable record. Apply's contract says a failed
	// Apply leaves nothing behind.
	l.ReadSession(func(sess *journal.Session) {
		for _, s := range sess.World.Seeds {
			if s != nil && s.Entity == "ghost" {
				t.Errorf("SECURITY: the refused ghost seed is in the in-memory world after Apply failed.")
			}
		}
	})
	entries, err := l.Journal().Read()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Kind == journal.KindWorldEdit && e.Op == journal.OpAddSeed {
			var p journal.AddSeedPayload
			if err := e.Decode(&p); err == nil && p.Seed != nil && p.Seed.Entity == "ghost" {
				t.Errorf("SECURITY: the refused ghost seed is durable in the journal: a failed Apply " +
					"must restore the pre-entry state (live.go Apply contract), and every restart " +
					"plus `kiln freeze` now replays a seed the runtime refused to apply.")
			}
		}
	}

	// Surface 3: the runtime must still be usable afterwards.
	if res := tools.Chat(t.Context(), protocol.ChatArgs{Role: "user", Text: "still alive"}); !res.OK {
		t.Errorf("runtime unusable after a failed side effect: %+v", res)
	}
}

// Control for the two reds above: two concurrent worlds over two
// EphemeralSQLite handles share nothing — the isolation primitive
// itself is sound; the divergence is lifecycle, not file sharing.
func TestConcurrentWorldsShareNothing(t *testing.T) {
	a, b := newDBHandle(t), newDBHandle(t)
	if a.path == b.path {
		t.Fatalf("two EphemeralSQLite handles share one file: %s", a.path)
	}
	la, err := live.New(journal.NewMemory(), a.factory)
	if err != nil {
		t.Fatal(err)
	}
	toolsA := protocol.New(la)
	if res := toolsA.AddEntity(t.Context(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	}); !res.OK {
		t.Fatalf("add_entity: %+v", res)
	}
	if res := toolsA.AddSeed(t.Context(), protocol.AddSeedArgs{
		Seed: &world.Seed{Entity: "posts", Rows: []map[string]any{{"title": "world-a-row"}}},
	}); !res.OK {
		t.Fatalf("add_seed: %+v", res)
	}
	lb, err := live.New(journal.NewMemory(), b.factory)
	if err != nil {
		t.Fatal(err)
	}
	toolsB := protocol.New(lb)
	if res := toolsB.AddEntity(t.Context(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	}); !res.OK {
		t.Fatalf("add_entity B: %+v", res)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	rec := httptest.NewRecorder()
	lb.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/posts on B: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "world-a-row") {
		t.Error("SECURITY: world A's rows are visible in world B's CRUD surface.")
	}
}

// dbHandle wraps one EphemeralSQLite lifetime plus an app factory over
// it, so tests can boot several Live runtimes against the same file
// (reboot) or distinct files (isolation).
type dbHandle struct {
	path    string
	factory func() *framework.App
}

func newDBHandle(t *testing.T) *dbHandle {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-sess")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	return &dbHandle{
		path: db.PathFor(d),
		factory: func() *framework.App {
			return framework.NewApp(framework.WithDB(d))
		},
	}
}

// Session-isolation primitive pin: EphemeralSQLite's cleanup removes
// the database file, so a disposed world leaves no data behind on disk
// (cmd/kiln wires this on exit unless --keep-db opts out).
func TestEphemeralCleanupRemovesWorld(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-cleanup")
	if err != nil {
		t.Fatal(err)
	}
	path := db.PathFor(d)
	if path == "" {
		t.Fatal("PathFor returned empty for an EphemeralSQLite handle")
	}
	if _, err := d.Exec(`CREATE TABLE residue (secret TEXT); INSERT INTO residue VALUES ('world-data')`); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("SECURITY: ephemeral DB %s survived its cleanup (stat err=%v): a disposed world's data stays on disk.", path, err)
	}
	// Idempotent: a second cleanup must not panic or fail.
	cleanup()
}
