package live_test

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property: no journal entry may panic past live.Apply's rollback, and
// no replayable journal may panic inside live.New. A world edit whose
// rebuild panics must leave the runtime exactly as Apply's contract
// promises: "On any failure the pre-entry state is restored by
// replaying the (still-unchanged) journal" (kiln/live/live.go:91-95).
//
// add_route is the reachable panic source: protocol and replay validate
// only non-empty/duplicate (kiln/protocol/protocol.go:612-621,
// kiln/journal/replay.go:332-347), and render.applyRoutes feeds the
// attacker strings to Router.Handle -> http.ServeMux parsePattern,
// which panics on a path without a leading '/' or on a conflict with
// kiln's own GET /openapi.json registration. The panic unwinds past
// live.Apply without running restoreFromJournal (live.go:105-107), but
// journal.Apply at live.go:99 has already put the route in the
// in-memory world: every subsequent Apply re-panics until ResetSession
// or restart, and a hand-authored .kiln.session.jsonl containing such
// an entry panics inside live.New's rebuild, so kiln cannot boot from
// that repo until the journal is hand-edited.

// TestApplyPoisonRouteDoesNotWedge pins the Apply seam: a poison
// add_route must either be rejected as an error (with the pre-entry
// state restored) or otherwise never leave the world unable to accept
// the next edit.
func TestApplyPoisonRouteDoesNotWedge(t *testing.T) {
	l, _ := newTestLive(t, journal.NewMemory())

	base := newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}})
	if err := l.Apply(base); err != nil {
		t.Fatalf("baseline Apply: %v", err)
	}

	poison := newEntry(t, "2", time.Now().Add(time.Second), journal.KindWorldEdit, journal.OpAddRoute,
		journal.AddRoutePayload{Route: &world.Route{Method: "GET", Path: "posts"}})
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SECURITY: Apply of an add_route entry (GET posts) panicked: %v.\n"+
					"live.Apply's documented contract is that ANY failure restores the "+
					"pre-entry state (kiln/live/live.go:91-95); the panic skips "+
					"restoreFromJournal, and because journal.Apply already mutated the "+
					"in-memory world (live.go:99), the poison route stays in memory.",
					r)
			}
		}()
		if err := l.Apply(poison); err != nil {
			// A clean rejection is an acceptable fix shape.
			t.Logf("Apply rejected the poison route: %v", err)
		}
	}()

	// The pre-entry state must be restored: the poison route must not be
	// in the in-memory world, whether Apply rejected it or panicked.
	if got := len(l.Session().World.Routes); got != 0 {
		t.Errorf("poison route survived in the in-memory world after a failed Apply: %d route(s)", got)
	}
	// The poison entry must not be durable.
	if n, _ := l.Journal().Len(); n != 1 {
		t.Errorf("journal len = %d after a failed Apply, want 1 (baseline only)", n)
	}

	// The runtime must still accept edits: one bad entry must not wedge
	// every subsequent Apply.
	follow := newEntry(t, "3", time.Now().Add(2*time.Second), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "comments", Fields: []world.Field{{Name: "body", Type: "string"}}}})
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: world wedged: the Apply AFTER a failed add_route panicked: %v.\n"+
					"The poison route stayed in the rebuilt world, so every later Apply "+
					"re-panics at route registration until ResetSession or restart.", r)
			}
		}()
		if err := l.Apply(follow); err != nil {
			t.Fatalf("follow-up Apply after failed add_route: %v", err)
		}
	}()
	if _, ok := l.Session().World.Entities["comments"]; !ok {
		t.Error("comments entity missing after successful follow-up Apply")
	}
}

// TestNewSurvivesPoisonRouteJournal pins the boot seam: replaying a
// journal that contains a poison add_route (the hand-authored
// .kiln.session.jsonl threat model pinned by kiln/journal's
// replay_security_test) must surface as an error from live.New, never
// as a panic.
func TestNewSurvivesPoisonRouteJournal(t *testing.T) {
	j := journal.NewMemory()
	poison := newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddRoute,
		journal.AddRoutePayload{Route: &world.Route{Method: "GET", Path: "posts"}})
	if _, err := j.Append(poison); err != nil {
		t.Fatalf("append poison entry: %v", err)
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(db)) }

	var (
		l    *live.Live
		nErr error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: live.New panicked replaying a journal containing an "+
					"add_route entry (GET posts): %v.\nA hand-authored .kiln.session.jsonl "+
					"with this single entry makes every subsequent kiln boot die inside "+
					"rebuild until the journal is hand-edited: a repo-local boot DoS.", r)
			}
		}()
		l, nErr = live.New(j, factory)
	}()
	if nErr != nil {
		// A clean error naming the entry is the acceptable fix shape.
		t.Logf("live.New rejected the poison journal: %v", nErr)
		return
	}
	// Boot must produce a usable runtime: the next edit still applies.
	follow := newEntry(t, "2", time.Now().Add(time.Second), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}})
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: first Apply after booting a poison journal panicked: %v", r)
			}
		}()
		if err := l.Apply(follow); err != nil {
			t.Fatalf("Apply after booting poison journal: %v", err)
		}
	}()
}

// TestApplyPoisonPageDoesNotWedge pins the Apply seam for the page-tree
// twin of the poison route above: protocol.AddPage never checks the path
// grammar (kiln/protocol/protocol.go:377-401), and applyUIHostPages
// registers the page as a core-ui screen, whose router panics on any
// dynamic path because worldScreen implements no SetParams
// (core-ui/app/router.go:85-91) and on malformed dynamic segments
// (router.go:223-255). The panic unwinds past live.Apply without
// running restoreFromJournal (live.go:105-107), while journal.Apply at
// live.go:99 has already put the page in the in-memory world: every
// subsequent Apply re-panics until ResetSession or restart.
func TestApplyPoisonPageDoesNotWedge(t *testing.T) {
	l, _ := newTestLive(t, journal.NewMemory())

	base := newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}})
	if err := l.Apply(base); err != nil {
		t.Fatalf("baseline Apply: %v", err)
	}

	poison := newEntry(t, "2", time.Now().Add(time.Second), journal.KindWorldEdit, journal.OpAddPage,
		journal.AddPagePayload{Page: &world.Page{Path: "/users/{id}", Tree: world.Node{Kind: "div"}}})
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SECURITY: Apply of an add_page entry (/users/{id}) panicked: %v.\n"+
					"live.Apply's documented contract is that ANY failure restores the "+
					"pre-entry state (kiln/live/live.go:91-95); the panic skips "+
					"restoreFromJournal, and because journal.Apply already mutated the "+
					"in-memory world (live.go:99), the poison page stays in memory.",
					r)
			}
		}()
		if err := l.Apply(poison); err != nil {
			// A clean rejection is an acceptable fix shape.
			t.Logf("Apply rejected the poison page: %v", err)
		}
	}()

	if got := len(l.Session().World.Pages); got != 0 {
		t.Errorf("poison page survived in the in-memory world after a failed Apply: %d page(s)", got)
	}
	if n, _ := l.Journal().Len(); n != 1 {
		t.Errorf("journal len = %d after a failed Apply, want 1 (baseline only)", n)
	}

	follow := newEntry(t, "3", time.Now().Add(2*time.Second), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "comments", Fields: []world.Field{{Name: "body", Type: "string"}}}})
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: world wedged: the Apply AFTER a failed add_page panicked: %v.\n"+
					"The poison page stayed in the rebuilt world, so every later Apply "+
					"re-panics at screen registration until ResetSession or restart.", r)
			}
		}()
		if err := l.Apply(follow); err != nil {
			t.Fatalf("follow-up Apply after failed add_page: %v", err)
		}
	}()
	if _, ok := l.Session().World.Entities["comments"]; !ok {
		t.Error("comments entity missing after successful follow-up Apply")
	}
}

// TestNewSurvivesPoisonPageJournal pins the boot seam for the same
// defect: replaying a journal that contains a dynamic-path add_page must
// surface as an error from live.New, never as a panic. Replay has no
// path-grammar guard either (kiln/journal/replay.go applyWorldEdit
// checks nil page, page actions, and duplicates only), so the panic
// fires inside live.New's rebuild.
func TestNewSurvivesPoisonPageJournal(t *testing.T) {
	j := journal.NewMemory()
	poison := newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddPage,
		journal.AddPagePayload{Page: &world.Page{Path: "/users/{id}", Tree: world.Node{Kind: "div"}}})
	if _, err := j.Append(poison); err != nil {
		t.Fatalf("append poison entry: %v", err)
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(db)) }

	var (
		l    *live.Live
		nErr error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: live.New panicked replaying a journal containing an "+
					"add_page entry (/users/{id}): %v.\nA hand-authored .kiln.session.jsonl "+
					"with this single entry makes every subsequent kiln boot die inside "+
					"rebuild until the journal is hand-edited: a repo-local boot DoS.", r)
			}
		}()
		l, nErr = live.New(j, factory)
	}()
	if nErr != nil {
		// A clean error naming the entry is the acceptable fix shape.
		t.Logf("live.New rejected the poison journal: %v", nErr)
		return
	}
	follow := newEntry(t, "2", time.Now().Add(time.Second), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}})
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: first Apply after booting a poison page journal panicked: %v", r)
			}
		}()
		if err := l.Apply(follow); err != nil {
			t.Fatalf("Apply after booting poison page journal: %v", err)
		}
	}()
}

// TestNewSurvivesCollisionJournal pins the boot seam of the entity/page
// mount collision: replay installs entity and page without any collision
// guard (kiln/journal/replay.go OpAddEntity/OpAddPage check duplicates
// only), and rebuild then panics inside framework App.Mount, which
// unconditionally compares each uihost RoutePatterns() entry against
// prefix+'/'+table (framework/app.go:1098-1114) — a key the protocol
// guards never compute (they compare '/'+Name, and only when the api
// prefix is empty; kiln/protocol/protocol.go:301-307, 393-401).
func TestNewSurvivesCollisionJournal(t *testing.T) {
	j := journal.NewMemory()
	ent := newEntry(t, "1", time.Now(), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "posts", Table: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}})
	if _, err := j.Append(ent); err != nil {
		t.Fatalf("append entity entry: %v", err)
	}
	page := newEntry(t, "2", time.Now().Add(time.Second), journal.KindWorldEdit, journal.OpAddPage,
		journal.AddPagePayload{Page: &world.Page{Path: "/api/posts", Tree: world.Node{Kind: "div"}}})
	if _, err := j.Append(page); err != nil {
		t.Fatalf("append page entry: %v", err)
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(db)) }

	var (
		l    *live.Live
		nErr error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: live.New panicked replaying a journal whose entity "+
					"CRUD mount (/api/posts) collides with a page at the same path: %v.\n"+
					"A hand-authored .kiln.session.jsonl with these two entries makes every "+
					"subsequent kiln boot die inside rebuild until the journal is "+
					"hand-edited: a repo-local boot DoS.", r)
			}
		}()
		l, nErr = live.New(j, factory)
	}()
	if nErr != nil {
		// A clean error naming the collision is the acceptable fix shape.
		t.Logf("live.New rejected the colliding journal: %v", nErr)
		return
	}
	follow := newEntry(t, "3", time.Now().Add(2*time.Second), journal.KindWorldEdit, journal.OpAddEntity,
		journal.AddEntityPayload{Entity: &world.Entity{Name: "comments", Fields: []world.Field{{Name: "body", Type: "string"}}}})
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: first Apply after booting a colliding journal panicked: %v", r)
			}
		}()
		if err := l.Apply(follow); err != nil {
			t.Fatalf("Apply after booting colliding journal: %v", err)
		}
	}()
}
